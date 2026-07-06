package recoverer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/rs/zerolog"
)

const testBookingId = "b7f9c1de-4a2b-4c3d-9e8f-0a1b2c3d4e5f"

// zincStub fakes the zinc endpoints the recoverer touches and records every
// status transition it drives, so tests can assert exactly which resolution
// (duplicate vs force-complete) was chosen.
type zincStub struct {
	t            *testing.T
	searchStatus int // 0 → 200
	searchBody   []zinc.BookingPrincipalRes
	duplicated   []string
	completed    []string
}

func (s *zincStub) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1.0/Booking":
			if s.searchStatus != 0 && s.searchStatus != http.StatusOK {
				w.WriteHeader(s.searchStatus)
				return
			}
			if err := json.NewEncoder(w).Encode(s.searchBody); err != nil {
				s.t.Fatal(err)
			}
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/duplicate/"):
			s.duplicated = append(s.duplicated, strings.TrimPrefix(r.URL.Path, "/api/v1.0/Booking/duplicate/"))
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/complete/"):
			s.completed = append(s.completed, strings.TrimPrefix(r.URL.Path, "/api/v1.0/Booking/complete/"))
			w.WriteHeader(http.StatusOK)
		default:
			s.t.Errorf("unexpected zinc call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func conflictClient(t *testing.T, stub *zincStub) (*Client, func()) {
	t.Helper()
	srv := stub.server()
	zc, err := zinc.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	l := zerolog.Nop()
	return &Client{zinc: zc, logger: &l, loc: sgLoc(t)}, srv.Close
}

func recoverDto() lib.RecoverDto {
	return lib.RecoverDto{
		BookingId:      testBookingId,
		Direction:      "WToJ",
		Date:           "03-07-2026",
		Time:           "13:45:00",
		PassportNumber: "E1234567X",
	}
}

// isClaimed is the money-critical §5.6 double-charge gate on the found-ticket
// path: a KTMB ticket already recorded on another Completed zinc booking must
// mark THIS booking Duplicate (refund), never force-complete (which would
// collect this booking's reserve for a ticket someone else owns). These tests
// pin the gate decision directly.

// The gate returns true when our uncaptured KTMB booking number already appears
// on a Completed zinc booking for the same passenger+slot.
func TestIsClaimedMatchingBookingNoIsClaimed(t *testing.T) {
	no := "KTMB-K1"
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{{BookingNo: &no}}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	claimed, err := c.isClaimed(context.Background(), recoverDto(), "KTMB-K1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !claimed {
		t.Error("a ticket whose booking number matches a Completed booking must be considered claimed")
	}
}

// A found ticket whose KTMB booking number matches no Completed zinc booking is
// unclaimed: the gate must let it through to capture (force-complete).
func TestIsClaimedUnmatchedBookingNoIsNotClaimed(t *testing.T) {
	other := "KTMB-OTHER"
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{{BookingNo: &other}}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	claimed, err := c.isClaimed(context.Background(), recoverDto(), "KTMB-K1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if claimed {
		t.Error("ticket with an unmatched booking number must not be considered claimed")
	}
}

// A failed claim search is inconclusive: it must surface an error (caller
// retries), never a silent "not claimed" that would force-complete a ticket
// another booking may own.
func TestIsClaimedSearchFailureErrors(t *testing.T) {
	stub := &zincStub{t: t, searchStatus: http.StatusBadGateway}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	if _, err := c.isClaimed(context.Background(), recoverDto(), "KTMB-K1"); err == nil {
		t.Fatal("expected error when the claim search fails, got nil")
	}
}
