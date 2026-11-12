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
	"github.com/google/uuid"
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
	// bookingBody is what GET Booking/{id} answers (nil → 404)
	bookingBody *zinc.BookingRes
	// expireBody is what POST Booking/recovery-expire answers
	expireBody []zinc.BookingPrincipalRes
	// checkResults records every POST Booking/ktmb-check/{id}/result body
	checkResults map[string]zinc.PostApiVVersionBookingKtmbCheckCheckIdResultJSONBody
	expired      int
	duplicated   []string
	completed    []string
	recycled     []string
	parked       []string
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
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/"):
			if s.bookingBody == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if err := json.NewEncoder(w).Encode(s.bookingBody); err != nil {
				s.t.Fatal(err)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1.0/Booking/recovery-expire":
			s.expired++
			if err := json.NewEncoder(w).Encode(s.expireBody); err != nil {
				s.t.Fatal(err)
			}
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/ktmb-check/") && strings.HasSuffix(r.URL.Path, "/result"):
			var body zinc.PostApiVVersionBookingKtmbCheckCheckIdResultJSONBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				s.t.Fatal(err)
			}
			if s.checkResults == nil {
				s.checkResults = map[string]zinc.PostApiVVersionBookingKtmbCheckCheckIdResultJSONBody{}
			}
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1.0/Booking/ktmb-check/"), "/result")
			s.checkResults[id] = body
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
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

// ticketHolder + resolveFound are the money-critical §5.6 gate on the
// found-ticket path: a KTMB ticket already recorded on another Completed zinc
// booking must mark THIS booking Duplicate (refund), never force-complete
// (which would collect this booking's reserve for a ticket someone else owns);
// a ticket recorded on a since-released booking must be recycled (re-bought),
// never captured; one on a still-live booking must wait.

func holderRes(id uuid.UUID, status, bookingNo string) zinc.BookingPrincipalRes {
	return zinc.BookingPrincipalRes{Id: id, Status: &status, BookingNo: &bookingNo}
}

// A Completed booking recording the ticket is the holder.
func TestTicketHolderCompletedMatch(t *testing.T) {
	other := uuid.New()
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{holderRes(other, "Completed", "KTMB-K1")}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	h, err := c.ticketHolder(context.Background(), recoverDto(), "KTMB-K1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if h == nil || h.Id != other {
		t.Fatalf("expected holder %s, got %+v", other, h)
	}
}

// A Completed holder wins over a released one recording the same ticket.
func TestTicketHolderPrefersCompleted(t *testing.T) {
	released, completed := uuid.New(), uuid.New()
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{
		holderRes(released, "Terminated", "KTMB-K1"),
		holderRes(completed, "Completed", "KTMB-K1"),
	}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	h, err := c.ticketHolder(context.Background(), recoverDto(), "KTMB-K1")
	if err != nil || h == nil || h.Id != completed {
		t.Fatalf("expected the Completed holder %s, got %+v (err %v)", completed, h, err)
	}
}

// A holder still in flight wins over a released one, in either response
// order, so recovery waits instead of recycling (and burning the budget).
func TestTicketHolderPrefersLiveOverReleased(t *testing.T) {
	live, released := uuid.New(), uuid.New()
	orders := map[string][]zinc.BookingPrincipalRes{
		"released-first": {holderRes(released, "Cancelled", "KTMB-K1"), holderRes(live, "Buying", "KTMB-K1")},
		"live-first":     {holderRes(live, "Buying", "KTMB-K1"), holderRes(released, "Cancelled", "KTMB-K1")},
	}
	for name, body := range orders {
		t.Run(name, func(t *testing.T) {
			stub := &zincStub{t: t, searchBody: body}
			c, closeSrv := conflictClient(t, stub)
			defer closeSrv()

			h, err := c.ticketHolder(context.Background(), recoverDto(), "KTMB-K1")
			if err != nil || h == nil || h.Id != live {
				t.Fatalf("expected the live holder %s, got %+v (err %v)", live, h, err)
			}
			if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err == nil {
				t.Fatal("expected resolveFound to wait (error) on a live holder, got nil")
			}
			assertNoTransitions(t, stub)
		})
	}
}

// No booking records the ticket (or only this booking itself does): no holder.
func TestTicketHolderUnmatchedOrSelf(t *testing.T) {
	self := uuid.MustParse(testBookingId)
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{
		holderRes(uuid.New(), "Completed", "KTMB-OTHER"),
		holderRes(self, "Recovering", "KTMB-K1"),
	}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	h, err := c.ticketHolder(context.Background(), recoverDto(), "KTMB-K1")
	if err != nil || h != nil {
		t.Fatalf("expected no holder, got %+v (err %v)", h, err)
	}
}

// A failed search is inconclusive: it must surface an error (caller retries),
// never a silent "no holder" that would force-complete a ticket another
// booking may own.
func TestTicketHolderSearchFailureErrors(t *testing.T) {
	stub := &zincStub{t: t, searchStatus: http.StatusBadGateway}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	if _, err := c.ticketHolder(context.Background(), recoverDto(), "KTMB-K1"); err == nil {
		t.Fatal("expected error when the holder search fails, got nil")
	}
}

// A full result page could hide the holder: inconclusive, error.
func TestTicketHolderFullPageIsInconclusive(t *testing.T) {
	body := make([]zinc.BookingPrincipalRes, 100)
	for i := range body {
		body[i] = holderRes(uuid.New(), "Cancelled", "KTMB-X")
	}
	stub := &zincStub{t: t, searchBody: body}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	if _, err := c.ticketHolder(context.Background(), recoverDto(), "KTMB-K1"); err == nil {
		t.Fatal("expected an inconclusive error on a full page, got nil")
	}
}

// Completed holder → this booking is a true duplicate.
func TestResolveFoundCompletedHolderMarksDuplicate(t *testing.T) {
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{holderRes(uuid.New(), "Completed", "KTMB-K1")}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(stub.duplicated) != 1 || len(stub.recycled) != 0 || len(stub.completed) != 0 || len(stub.parked) != 0 {
		t.Fatalf("expected exactly one duplicate, got dup=%v recycle=%v complete=%v park=%v", stub.duplicated, stub.recycled, stub.completed, stub.parked)
	}
}

// The 2026-11-11 incident: the user double-booked the same passenger+slot,
// the first booking bought the ticket and was then terminated (KTMB ticket
// cancelled), and the second booking — recovering from the duplicate-passport
// rejection — found that ticket on our account. It must be recycled to Pending
// for a fresh purchase, never force-completed (KTMB will not print a cancelled
// ticket, so capture fails forever and the booking gets parked).
func TestResolveFoundReleasedHolderRecycles(t *testing.T) {
	for _, status := range []string{"Terminated", "Cancelled", "Refunded"} {
		t.Run(status, func(t *testing.T) {
			stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{holderRes(uuid.New(), status, "KTMB-K1")}}
			c, closeSrv := conflictClient(t, stub)
			defer closeSrv()
			c.printTicket = printer(map[string]string{"KTMB-K1": cancelledBody})

			if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if len(stub.recycled) != 1 || stub.recycled[0] != testBookingId {
				t.Fatalf("expected one recycle of %s, got %v", testBookingId, stub.recycled)
			}
			if len(stub.completed) != 0 || len(stub.duplicated) != 0 || len(stub.parked) != 0 {
				t.Fatalf("released holder must only recycle, got complete=%v dup=%v park=%v", stub.completed, stub.duplicated, stub.parked)
			}
		})
	}
}

// A holder still in flight (e.g. Buying) → wait: error, no transition.
func TestResolveFoundLiveHolderWaits(t *testing.T) {
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{holderRes(uuid.New(), "Buying", "KTMB-K1")}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err == nil {
		t.Fatal("expected an error (retry later) for a live holder, got nil")
	}
	assertNoTransitions(t, stub)
}

const (
	livePDF       = "%PDF-1.7\n%fake ticket"
	cancelledBody = "Error when retrieving booking."
)

// printer fakes KTMB PrintTicket per KTMB booking number; a missing entry is
// a print error
func printer(bodies map[string]string) func(context.Context, string, string) ([]byte, error) {
	return func(_ context.Context, bookingNo, _ string) ([]byte, error) {
		b, ok := bodies[bookingNo]
		if !ok {
			return nil, fmt.Errorf("print failed for %s", bookingNo)
		}
		return []byte(b), nil
	}
}

// zinc says the holder was Terminated, but its KTMB cancel never landed (the
// ticket still prints). Recycling would loop into a duplicate refund while the
// passenger's ticket is live: leave it for a human (error, no transition), and
// never capture it (a late terminator run could still cancel it).
func TestResolveFoundReleasedButStillLiveWaitsForHuman(t *testing.T) {
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{holderRes(uuid.New(), "Terminated", "KTMB-K1")}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.printTicket = printer(map[string]string{"KTMB-K1": livePDF})

	if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err == nil {
		t.Fatal("expected an error (leave for a human) for a still-live released ticket, got nil")
	}
	assertNoTransitions(t, stub)
}

// Printing the released ticket fails outright: cannot tell cancelled from a
// KTMB hiccup — retry, no transition.
func TestResolveFoundReleasedPrintErrorRetries(t *testing.T) {
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{holderRes(uuid.New(), "Terminated", "KTMB-K1")}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.printTicket = printer(map[string]string{})

	if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err == nil {
		t.Fatal("expected an error on a failed print, got nil")
	}
	assertNoTransitions(t, stub)
}

// A cancelled row of a released booking listed next to this booking's own
// live, uncaptured ticket (bought after a recycle, ids lost): capture our own
// ticket, in either list order — never recycle into a duplicate refund.
func TestResolveFoundReleasedPlusOwnTicketCaptures(t *testing.T) {
	dead := foundTicket{BookingNo: "KTMB-DEAD", TicketNo: "T-DEAD"}
	own := foundTicket{BookingNo: "KTMB-OWN", TicketNo: "T-OWN"}
	for name, order := range map[string][]foundTicket{"dead-first": {dead, own}, "own-first": {own, dead}} {
		t.Run(name, func(t *testing.T) {
			stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{holderRes(uuid.New(), "Terminated", "KTMB-DEAD")}}
			c, closeSrv := conflictClient(t, stub)
			defer closeSrv()
			c.printTicket = printer(map[string]string{"KTMB-DEAD": cancelledBody, "KTMB-OWN": livePDF})

			if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), order, nil); err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if len(stub.completed) != 1 || stub.completed[0] != testBookingId {
				t.Fatalf("expected our own ticket captured for %s, got %v", testBookingId, stub.completed)
			}
			if len(stub.recycled) != 0 || len(stub.duplicated) != 0 || len(stub.parked) != 0 {
				t.Fatalf("must only capture, got recycle=%v dup=%v park=%v", stub.recycled, stub.duplicated, stub.parked)
			}
		})
	}
}

// Only released tickets seen, but the scan was inconclusive somewhere (an
// unseen row could be our own ticket): retry, never recycle.
func TestResolveFoundReleasedPartialScanRetries(t *testing.T) {
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{holderRes(uuid.New(), "Terminated", "KTMB-K1")}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.printTicket = printer(map[string]string{"KTMB-K1": cancelledBody})

	if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, fmt.Errorf("unparseable row")); err == nil {
		t.Fatal("expected an error on a partial scan, got nil")
	}
	assertNoTransitions(t, stub)
}

// A non-PDF print on the capture path fails before any zinc upload.
func TestForceCompleteRejectsNonPDF(t *testing.T) {
	stub := &zincStub{t: t}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.printTicket = printer(map[string]string{"KTMB-K1": cancelledBody})

	if err := c.forceComplete(context.Background(), testBookingId, "KTMB-K1", "T-1"); err == nil {
		t.Fatal("expected an error for a non-PDF ticket, got nil")
	}
	assertNoTransitions(t, stub)
}

// A non-PDF answer that is not KTMB's known "cancelled" text (empty body,
// maintenance page, JSON error) is never read as cancelled: retry, no
// transition — for released and unrecorded tickets alike.
func TestResolveFoundUnknownPrintBodyRetries(t *testing.T) {
	for _, body := range []string{"", "<html>maintenance</html>", `{"status":false,"messages":["busy"]}`} {
		for _, status := range []string{"Terminated", ""} {
			t.Run(status+"/"+body, func(t *testing.T) {
				var search []zinc.BookingPrincipalRes
				if status != "" {
					search = []zinc.BookingPrincipalRes{holderRes(uuid.New(), status, "KTMB-K1")}
				}
				stub := &zincStub{t: t, searchBody: search}
				c, closeSrv := conflictClient(t, stub)
				defer closeSrv()
				c.printTicket = printer(map[string]string{"KTMB-K1": body})

				if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err == nil {
					t.Fatal("expected an error (retry) on an unrecognised print body, got nil")
				}
				assertNoTransitions(t, stub)
			})
		}
	}
}

// An unrecorded but cancelled ticket (e.g. zinc's passport search missed its
// holder) is not captured: with only cancelled tickets around, recycle.
func TestResolveFoundUnrecordedCancelledRecycles(t *testing.T) {
	stub := &zincStub{t: t}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.printTicket = printer(map[string]string{"KTMB-K1": cancelledBody})

	if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(stub.recycled) != 1 || len(stub.completed) != 0 || len(stub.duplicated) != 0 || len(stub.parked) != 0 {
		t.Fatalf("expected only a recycle, got recycle=%v complete=%v dup=%v park=%v", stub.recycled, stub.completed, stub.duplicated, stub.parked)
	}
}

// An unrecorded cancelled ticket must not mask a Completed holder's ticket:
// still a duplicate.
func TestResolveFoundUnrecordedCancelledPlusCompletedIsDuplicate(t *testing.T) {
	stub := &zincStub{t: t, searchBody: []zinc.BookingPrincipalRes{holderRes(uuid.New(), "Completed", "KTMB-HELD")}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.printTicket = printer(map[string]string{"KTMB-DEAD": cancelledBody, "KTMB-HELD": livePDF})

	matches := []foundTicket{{BookingNo: "KTMB-DEAD", TicketNo: "T-D"}, {BookingNo: "KTMB-HELD", TicketNo: "T-H"}}
	if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), matches, nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(stub.duplicated) != 1 || len(stub.completed) != 0 || len(stub.recycled) != 0 || len(stub.parked) != 0 {
		t.Fatalf("expected only a duplicate, got dup=%v complete=%v recycle=%v park=%v", stub.duplicated, stub.completed, stub.recycled, stub.parked)
	}
}

// Only cancelled tickets and zinc refuses the recycle (budget spent or
// departure too close): park for a human — never a Duplicate refund telling
// the customer they already hold a ticket.
func TestResolveFoundCancelledOnlyRecycleRefusedParks(t *testing.T) {
	stub := &zincStub{t: t, recycleStatus: http.StatusConflict, searchBody: []zinc.BookingPrincipalRes{holderRes(uuid.New(), "Terminated", "KTMB-K1")}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.printTicket = printer(map[string]string{"KTMB-K1": cancelledBody})

	if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(stub.parked) != 1 || len(stub.duplicated) != 0 || len(stub.completed) != 0 {
		t.Fatalf("expected a park and no duplicate, got park=%v dup=%v complete=%v", stub.parked, stub.duplicated, stub.completed)
	}
}

// An unrecorded live ticket is captured with the PDF already printed.
func TestResolveFoundUnrecordedLiveCaptures(t *testing.T) {
	stub := &zincStub{t: t}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.printTicket = printer(map[string]string{"KTMB-K1": livePDF})

	if err := c.resolveFound(context.Background(), zerolog.Nop(), recoverDto(), []foundTicket{{BookingNo: "KTMB-K1", TicketNo: "T-1"}}, nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(stub.completed) != 1 || len(stub.recycled) != 0 || len(stub.duplicated) != 0 || len(stub.parked) != 0 {
		t.Fatalf("expected only a capture, got complete=%v recycle=%v dup=%v park=%v", stub.completed, stub.recycled, stub.duplicated, stub.parked)
	}
}

func TestIsPDF(t *testing.T) {
	cases := map[string]bool{
		"%PDF-1.7\n...":                     true,
		"\xef\xbb\xbf%PDF-1.4":              true,
		"\n  %PDF-1.4":                      true,
		`{"status":false,"messages":["x"]}`: false,
		"":                                  false,
		"<html>":                            false,
	}
	for in, want := range cases {
		if got := isPDF([]byte(in)); got != want {
			t.Errorf("isPDF(%q) = %v, want %v", in, got, want)
		}
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
	if err := c.recycleOrDuplicate(context.Background(), l, testBookingId, true); err != nil {
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
	if err := c.recycleOrDuplicate(context.Background(), l, testBookingId, true); err != nil {
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
	if err := c.recycleOrDuplicate(context.Background(), l, testBookingId, true); err != nil {
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
		err := c.recycleOrDuplicate(context.Background(), l, testBookingId, true)
		closeSrv()
		if err == nil {
			t.Errorf("expected error on recycle status %d, got nil", status)
		}
		if len(stub.duplicated) != 0 {
			t.Errorf("status %d must not mark duplicate, got %v", status, stub.duplicated)
		}
	}
}

// problemTypeOf is logging-only and must tolerate garbage. zinc's real
// problem-details payload carries "type" as an error-portal URL ending in the
// problem id (or omits it when the portal is disabled) — pin the
// last-segment extraction against that shape.
func TestProblemTypeOf(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"type":"https://errors.example.com/v1/recovery_retries_exhausted","title":"conflict","status":409}`, "recovery_retries_exhausted"},
		{`{"type":"recovery_retries_exhausted"}`, "recovery_retries_exhausted"},
		{`{"title":"conflict","status":409}`, ""},
		{`not json`, ""},
		{``, ""},
	}
	for _, tc := range cases {
		if got := problemTypeOf([]byte(tc.body)); got != tc.want {
			t.Errorf("problemTypeOf(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}
