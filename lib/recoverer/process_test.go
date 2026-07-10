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
// (duplicate vs force-complete vs recycle) was chosen.
type zincStub struct {
	t            *testing.T
	searchStatus int // 0 → 200
	searchBody   []zinc.BookingPrincipalRes
	// recycleStatus is what POST Booking/recover-revert/{id} answers (0 → 200)
	recycleStatus int
	// recycleBody is the raw body the recycle endpoint answers with; empty →
	// a minimal BookingPrincipalRes
	recycleBody string
	duplicated  []string
	completed   []string
	recycled    []string
	parked      []string
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
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/manual-intervention/"):
			s.parked = append(s.parked, strings.TrimPrefix(r.URL.Path, "/api/v1.0/Booking/manual-intervention/"))
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/recover-revert/"):
			s.recycled = append(s.recycled, strings.TrimPrefix(r.URL.Path, "/api/v1.0/Booking/recover-revert/"))
			status := s.recycleStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if s.recycleBody != "" {
				_, _ = w.Write([]byte(s.recycleBody))
			} else if status == http.StatusOK {
				_, _ = w.Write([]byte(`{}`))
			}
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

// recycleOrDuplicate is the retry-before-duplicate gate on the definitive
// not-found path: a booking whose ticket is provably not on our KTMB account
// is first recycled back to Pending via zinc (which owns the retry counter)
// and only refunded as a Duplicate once zinc says the budget is spent (409).

// 200 → the booking is Pending again; no duplicate transition may fire.
func TestRecycleOrDuplicateRecycles(t *testing.T) {
	stub := &zincStub{t: t, recycleBody: `{"recoveryRetries": 3}`}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	l := zerolog.Nop()
	if err := c.recycleOrDuplicate(context.Background(), l, testBookingId); err != nil {
		t.Fatalf("expected nil error on a successful recycle, got %v", err)
	}
	if len(stub.recycled) != 1 || stub.recycled[0] != testBookingId {
		t.Errorf("expected exactly one recycle call for %s, got %v", testBookingId, stub.recycled)
	}
	if len(stub.duplicated) != 0 {
		t.Errorf("a successful recycle must not mark duplicate, got %v", stub.duplicated)
	}
}

// 409 (recovery_retries_exhausted) → the budget is spent: fall back to
// markDuplicate exactly as the pre-recycle behavior did.
func TestRecycleOrDuplicateExhaustedFallsBackToDuplicate(t *testing.T) {
	stub := &zincStub{t: t, recycleStatus: http.StatusConflict,
		recycleBody: `{"type":"recovery_retries_exhausted","title":"..."}`}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	l := zerolog.Nop()
	if err := c.recycleOrDuplicate(context.Background(), l, testBookingId); err != nil {
		t.Fatalf("expected nil error when falling back to duplicate, got %v", err)
	}
	if len(stub.duplicated) != 1 || stub.duplicated[0] != testBookingId {
		t.Errorf("expected exactly one duplicate transition for %s, got %v", testBookingId, stub.duplicated)
	}
}

// 400 → the booking state changed under us: drop the item (nil) without any
// transition; the sweep re-derives from zinc if it is still stuck.
func TestRecycleOrDuplicateBadRequestDropsItem(t *testing.T) {
	stub := &zincStub{t: t, recycleStatus: http.StatusBadRequest,
		recycleBody: `{"type":"invalid_booking_operation"}`}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	l := zerolog.Nop()
	if err := c.recycleOrDuplicate(context.Background(), l, testBookingId); err != nil {
		t.Fatalf("expected nil error (drop) on 400, got %v", err)
	}
	if len(stub.duplicated) != 0 {
		t.Errorf("a 400 must not mark duplicate, got %v", stub.duplicated)
	}
}

// Any other status (e.g. 404 from an old zinc without the endpoint, or a 5xx)
// is transient: surface an error so the caller requeues — never a silent
// duplicate, and never a silent drop.
func TestRecycleOrDuplicateOtherStatusErrors(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusBadGateway} {
		stub := &zincStub{t: t, recycleStatus: status}
		c, closeSrv := conflictClient(t, stub)

		l := zerolog.Nop()
		err := c.recycleOrDuplicate(context.Background(), l, testBookingId)
		closeSrv()
		if err == nil {
			t.Errorf("expected error on recycle status %d, got nil", status)
		}
		if len(stub.duplicated) != 0 {
			t.Errorf("status %d must not mark duplicate, got %v", status, stub.duplicated)
		}
	}
}

// problemTypeOf is logging-only and must tolerate garbage.
func TestProblemTypeOf(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"type":"recovery_retries_exhausted"}`, "recovery_retries_exhausted"},
		{`not json`, ""},
		{``, ""},
	}
	for _, tc := range cases {
		if got := problemTypeOf([]byte(tc.body)); got != tc.want {
			t.Errorf("problemTypeOf(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}
