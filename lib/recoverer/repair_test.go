package recoverer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// repairZincStub fakes the two zinc endpoints the repair sweep writes to:
// the ticket upload and the manual-intervention transition.
type repairZincStub struct {
	t            *testing.T
	uploadStatus int // 0 → 200
	uploaded     []string
	uploadQuery  []string
	parked       []string
}

func (s *repairZincStub) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/ticket/"):
			s.uploaded = append(s.uploaded, strings.TrimPrefix(r.URL.Path, "/api/v1.0/Booking/ticket/"))
			s.uploadQuery = append(s.uploadQuery, r.URL.RawQuery)
			if s.uploadStatus != 0 && s.uploadStatus != http.StatusOK {
				w.WriteHeader(s.uploadStatus)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/manual-intervention/"):
			s.parked = append(s.parked, strings.TrimPrefix(r.URL.Path, "/api/v1.0/Booking/manual-intervention/"))
			w.WriteHeader(http.StatusOK)
		default:
			s.t.Errorf("unexpected zinc call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func repairClient(t *testing.T, stub *repairZincStub) (*Client, func()) {
	t.Helper()
	srv := stub.server()
	zc, err := zinc.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	l := zerolog.Nop()
	return &Client{
		zinc:   zc,
		logger: &l,
		loc:    sgLoc(t),
		config: config.RecovererConfig{
			RepairEnable:           true,
			RepairLimit:            100,
			RepairNotFoundPatterns: []string{"not found", "no record"},
		},
	}, srv.Close
}

func completedBooking(t *testing.T, bookingNo, ticketNo string) zinc.BookingPrincipalRes {
	t.Helper()
	status := "Completed"
	id, err := uuid.Parse(testBookingId)
	if err != nil {
		t.Fatal(err)
	}
	b := zinc.BookingPrincipalRes{Id: id, Status: &status}
	if bookingNo != "" {
		b.BookingNo = &bookingNo
	}
	if ticketNo != "" {
		b.TicketNo = &ticketNo
	}
	return b
}

// The repair sweep's decision logic (spec): identifiers present + KTMB serves
// the PDF → upload to zinc; identifiers missing or KTMB definitively unknown →
// park for a human; anything inconclusive → do nothing (next sweep retries).

// Happy path: both identifiers present, PrintTicket succeeds → PDF is uploaded
// with the identifiers as query params; nothing is parked.
func TestRepairOneUploadsTicket(t *testing.T) {
	stub := &repairZincStub{t: t}
	c, closeSrv := repairClient(t, stub)
	defer closeSrv()

	print := func(bookingNo, ticketNo string) ([]byte, error) {
		if bookingNo != "KTMB-K1" || ticketNo != "T-1" {
			t.Errorf("print called with (%q, %q), want (KTMB-K1, T-1)", bookingNo, ticketNo)
		}
		return []byte("%PDF-fake"), nil
	}
	c.repairOne(context.Background(), completedBooking(t, "KTMB-K1", "T-1"), print)

	if len(stub.uploaded) != 1 || stub.uploaded[0] != testBookingId {
		t.Fatalf("expected one upload for %s, got %v", testBookingId, stub.uploaded)
	}
	if !strings.Contains(stub.uploadQuery[0], "bookingNo=KTMB-K1") || !strings.Contains(stub.uploadQuery[0], "ticketNo=T-1") {
		t.Errorf("upload query must carry identifiers, got %q", stub.uploadQuery[0])
	}
	if len(stub.parked) != 0 {
		t.Errorf("happy path must not park, got %v", stub.parked)
	}
}

// Missing identifiers: nothing can be re-downloaded — a human must source the
// ticket. Park, never call KTMB.
func TestRepairOneMissingIdentifiersParks(t *testing.T) {
	for _, b := range []zinc.BookingPrincipalRes{
		completedBooking(t, "", ""),
		completedBooking(t, "KTMB-K1", ""),
		completedBooking(t, "", "T-1"),
	} {
		stub := &repairZincStub{t: t}
		c, closeSrv := repairClient(t, stub)

		printed := false
		c.repairOne(context.Background(), b, func(string, string) ([]byte, error) {
			printed = true
			return nil, fmt.Errorf("must not be called")
		})
		closeSrv()

		if printed {
			t.Error("KTMB must not be called when identifiers are missing")
		}
		if len(stub.parked) != 1 || stub.parked[0] != testBookingId {
			t.Errorf("expected manual-intervention park, got %v", stub.parked)
		}
		if len(stub.uploaded) != 0 {
			t.Errorf("must not upload without a PDF, got %v", stub.uploaded)
		}
	}
}

// A KTMB error matching a not-found pattern is definitive: the ticket can
// never be re-downloaded — park for a human.
func TestRepairOneKtmbNotFoundParks(t *testing.T) {
	stub := &repairZincStub{t: t}
	c, closeSrv := repairClient(t, stub)
	defer closeSrv()

	print := func(string, string) ([]byte, error) {
		return nil, &ktmb.HttpStatusError{StatusCode: 400, Body: "Booking Not Found"}
	}
	c.repairOne(context.Background(), completedBooking(t, "KTMB-K1", "T-1"), print)

	if len(stub.parked) != 1 || stub.parked[0] != testBookingId {
		t.Errorf("expected manual-intervention park on definitive not-found, got %v", stub.parked)
	}
	if len(stub.uploaded) != 0 {
		t.Errorf("must not upload on not-found, got %v", stub.uploaded)
	}
}

// Any other KTMB error is inconclusive (network, 5xx, session expiry, a
// departed ticket behaving oddly): do nothing — the next sweep retries. This
// also covers the misconfiguration case (empty pattern list): degrade to
// retry-forever, never to a wrongful park.
func TestRepairOneTransientKtmbErrorRetriesNextSweep(t *testing.T) {
	stub := &repairZincStub{t: t}
	c, closeSrv := repairClient(t, stub)
	defer closeSrv()

	print := func(string, string) ([]byte, error) {
		return nil, fmt.Errorf("connection reset by peer")
	}
	c.repairOne(context.Background(), completedBooking(t, "KTMB-K1", "T-1"), print)

	if len(stub.parked) != 0 {
		t.Errorf("transient errors must not park, got %v", stub.parked)
	}
	if len(stub.uploaded) != 0 {
		t.Errorf("transient errors must not upload, got %v", stub.uploaded)
	}
}

// An empty 2xx PDF is a scrape anomaly, not a verdict: retry next sweep.
func TestRepairOneEmptyPdfRetriesNextSweep(t *testing.T) {
	stub := &repairZincStub{t: t}
	c, closeSrv := repairClient(t, stub)
	defer closeSrv()

	c.repairOne(context.Background(), completedBooking(t, "KTMB-K1", "T-1"),
		func(string, string) ([]byte, error) { return []byte{}, nil })

	if len(stub.parked) != 0 || len(stub.uploaded) != 0 {
		t.Errorf("empty PDF must neither park nor upload, got parked=%v uploaded=%v", stub.parked, stub.uploaded)
	}
}

// A failed upload is transient too: the PDF is re-downloadable, so just log
// and let the next sweep retry — never park.
func TestRepairOneUploadFailureRetriesNextSweep(t *testing.T) {
	stub := &repairZincStub{t: t, uploadStatus: http.StatusBadGateway}
	c, closeSrv := repairClient(t, stub)
	defer closeSrv()

	c.repairOne(context.Background(), completedBooking(t, "KTMB-K1", "T-1"),
		func(string, string) ([]byte, error) { return []byte("%PDF-fake"), nil })

	if len(stub.parked) != 0 {
		t.Errorf("upload failure must not park, got %v", stub.parked)
	}
}

// Per-item failures never abort the sweep: a parked booking and a transient
// failure must not stop later bookings from being repaired.
func TestRepairBookingsContinuesPastFailures(t *testing.T) {
	stub := &repairZincStub{t: t}
	c, closeSrv := repairClient(t, stub)
	defer closeSrv()

	good := completedBooking(t, "KTMB-K2", "T-2")
	bookings := []zinc.BookingPrincipalRes{
		completedBooking(t, "", ""), // parks
		good,                        // repairs
	}
	c.repairBookings(context.Background(), bookings, func(bookingNo, ticketNo string) ([]byte, error) {
		return []byte("%PDF-fake"), nil
	})

	if len(stub.parked) != 1 {
		t.Errorf("expected the identifier-less booking parked, got %v", stub.parked)
	}
	if len(stub.uploaded) != 1 {
		t.Errorf("expected the healthy booking repaired despite the earlier park, got %v", stub.uploaded)
	}
}

// matchesNotFound is the definitive-vs-transient classifier; pin its double
// gate: only a semantic KTMB client rejection (HttpStatusError 400/404/410)
// whose BODY matches a pattern is definitive. Transport errors, 5xx/gateway
// pages and auth failures are never definitive no matter what their body
// says, and empty patterns never match.
func TestMatchesNotFound(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		err      error
		want     bool
	}{
		{"semantic 400 with matching body", []string{"not found"},
			&ktmb.HttpStatusError{StatusCode: 400, Body: "Booking NOT FOUND"}, true},
		{"semantic 404 with matching body", []string{"no record"},
			&ktmb.HttpStatusError{StatusCode: 404, Body: "No Record Exists"}, true},
		{"5xx outage page containing pattern is NOT definitive", []string{"not found"},
			&ktmb.HttpStatusError{StatusCode: 502, Body: "<html>404 not found - nginx</html>"}, false},
		{"auth failure containing pattern is NOT definitive", []string{"not found"},
			&ktmb.HttpStatusError{StatusCode: 401, Body: "session not found"}, false},
		{"plain transport error is NOT definitive", []string{"not found"},
			fmt.Errorf("booking not found: connection refused"), false},
		{"semantic 400 without matching body", []string{"not found"},
			&ktmb.HttpStatusError{StatusCode: 400, Body: "internal error"}, false},
		{"nil patterns never match", nil,
			&ktmb.HttpStatusError{StatusCode: 400, Body: "booking not found"}, false},
		{"empty pattern never matches", []string{""},
			&ktmb.HttpStatusError{StatusCode: 400, Body: "anything"}, false},
	}
	for _, tc := range cases {
		if got := matchesNotFound(tc.patterns, tc.err); got != tc.want {
			t.Errorf("%s: matchesNotFound(%v, %v) = %t, want %t", tc.name, tc.patterns, tc.err, got, tc.want)
		}
	}
}
