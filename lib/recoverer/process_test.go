package recoverer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/rs/zerolog"
)

const testBookingId = "b7f9c1de-4a2b-4c3d-9e8f-0a1b2c3d4e5f"

// zincStub fakes the zinc endpoints resolveConflict may touch and records
// every status transition it drives, so tests can assert exactly which
// resolution (duplicate vs force-complete) was chosen.
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

// The §5.6 double-charge guard: booking A is Completed holding KTMB ticket K1;
// booking B (same passenger/slot) is Recovering. B's re-buy probe conflicts and
// the re-scan finds K1 — because K1 is already claimed by A, B must be marked
// Duplicate (refund), NEVER force-completed (that would collect B's reserve for
// a ticket A owns). This is the same claim gate the main scan path applies.
func TestResolveConflictClaimedTicketMarksDuplicate(t *testing.T) {
	no := "KTMB-K1"
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{{BookingNo: &no}}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	rescan := func() (*foundTicket, error) {
		return &foundTicket{BookingNo: "KTMB-K1", TicketNo: "T1"}, nil
	}
	if err := c.resolveConflict(context.Background(), recoverDto(), rescan, zerolog.Nop()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(stub.duplicated) != 1 || stub.duplicated[0] != testBookingId {
		t.Errorf("expected duplicate transition for %s, got %v", testBookingId, stub.duplicated)
	}
	if len(stub.completed) != 0 {
		t.Errorf("claimed ticket must never be force-completed, got complete calls %v", stub.completed)
	}
}

// A failed claim check is inconclusive: retry (error return), never refund and
// never capture. The error path must not stash the found identifiers — a
// stashed id would route the next cycle onto the deterministic force-complete
// path, which skips the claim gate.
func TestResolveConflictClaimCheckFailureRetries(t *testing.T) {
	stub := &zincStub{t: t, searchStatus: http.StatusBadGateway}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	rescan := func() (*foundTicket, error) {
		return &foundTicket{BookingNo: "KTMB-K1", TicketNo: "T1"}, nil
	}
	if err := c.resolveConflict(context.Background(), recoverDto(), rescan, zerolog.Nop()); err == nil {
		t.Fatal("expected error when the claim check fails, got nil")
	}
	if len(stub.duplicated) != 0 || len(stub.completed) != 0 {
		t.Errorf("no transition may run on a failed claim check, got duplicate %v complete %v", stub.duplicated, stub.completed)
	}
}

// An inconclusive re-scan (empty list, mutated pagination, unparseable row)
// must retry — never read as "not ours" and refund (§3.3/§5.6).
func TestResolveConflictInconclusiveRescanRetries(t *testing.T) {
	stub := &zincStub{t: t}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	rescan := func() (*foundTicket, error) {
		return nil, fmt.Errorf("empty ticket list (inconclusive, must retry — never treat as absent)")
	}
	if err := c.resolveConflict(context.Background(), recoverDto(), rescan, zerolog.Nop()); err == nil {
		t.Fatal("expected error for inconclusive re-scan, got nil")
	}
	if len(stub.duplicated) != 0 || len(stub.completed) != 0 {
		t.Errorf("no transition may run on an inconclusive re-scan, got duplicate %v complete %v", stub.duplicated, stub.completed)
	}
}

// KTMB confirmed the conflict and a conclusive re-scan shows the ticket is not
// on our account: the passenger holds it via another channel — true duplicate.
func TestResolveConflictNotOnAccountMarksDuplicate(t *testing.T) {
	stub := &zincStub{t: t}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	rescan := func() (*foundTicket, error) { return nil, nil }
	if err := c.resolveConflict(context.Background(), recoverDto(), rescan, zerolog.Nop()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(stub.duplicated) != 1 || stub.duplicated[0] != testBookingId {
		t.Errorf("expected duplicate transition for %s, got %v", testBookingId, stub.duplicated)
	}
	if len(stub.completed) != 0 {
		t.Errorf("expected no complete calls, got %v", stub.completed)
	}
}

// A found ticket claimed by a DIFFERENT KTMB booking number than any Completed
// zinc booking is unclaimed: the claim gate must let it through to capture.
// (The capture itself needs a live KTMB session, so this only asserts the gate
// decision — isClaimed — not the force-complete plumbing.)
func TestResolveConflictUnclaimedIsNotDuplicate(t *testing.T) {
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
	if len(stub.duplicated) != 0 || len(stub.completed) != 0 {
		t.Errorf("expected no transitions, got duplicate %v complete %v", stub.duplicated, stub.completed)
	}
}
