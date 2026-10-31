package recoverer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/google/uuid"
)

// The admin KTMB account check must report exactly what the recovery scan
// sees and NEVER resolve anything: the zinc stub fails the test on any call
// outside GET booking, the claim search and the check-result write, and each
// test also asserts no transition endpoint was hit.

func checkBooking(status string, bookingNo *string) *zinc.BookingRes {
	s := func(v string) *string { return &v }
	return &zinc.BookingRes{Principal: zinc.BookingPrincipalRes{
		Id:        uuid.MustParse(testBookingId),
		Date:      s("03-07-2026"),
		Time:      s("13:45:00"),
		Direction: s("WToJ"),
		Status:    s(status),
		BookingNo: bookingNo,
		Passenger: zinc.BookingPassengerRes{PassportNumber: s("E1234567X")},
	}}
}

// a clock well before the 03-07-2026 13:45 SGT departure
func beforeDeparture(t *testing.T) time.Time {
	return time.Date(2026, 7, 3, 10, 0, 0, 0, sgLoc(t))
}

func scanReturning(f *foundTicket, err error) (ticketScanner, *int) {
	calls := 0
	return func(_ context.Context, target time.Time, direction, passport string) (*foundTicket, error) {
		calls++
		return f, err
	}, &calls
}

func assertNoTransitions(t *testing.T, s *zincStub) {
	t.Helper()
	if len(s.duplicated)+len(s.completed)+len(s.recycled)+len(s.parked) != 0 || s.expired != 0 {
		t.Fatalf("a KTMB check must never transition a booking: duplicated=%v completed=%v recycled=%v parked=%v expired=%d",
			s.duplicated, s.completed, s.recycled, s.parked, s.expired)
	}
}

func TestCheckFoundUnclaimed(t *testing.T) {
	stub := &zincStub{t: t, bookingBody: checkBooking("Recovering", nil)}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	scan, calls := scanReturning(&foundTicket{BookingNo: "KTMB-K1", TicketNo: "T-1"}, nil)

	res := c.CheckBooking(context.Background(), testBookingId, beforeDeparture(t), scan)

	if res.Status != CheckFound || res.KtmbBookingNo != "KTMB-K1" || res.KtmbTicketNo != "T-1" || res.ClaimedBy != nil || res.Error != "" {
		t.Fatalf("unexpected result %+v", res)
	}
	if *calls != 1 {
		t.Errorf("expected one scan, got %d", *calls)
	}
	assertNoTransitions(t, stub)
}

func TestCheckFoundClaimedByAnotherBooking(t *testing.T) {
	other := uuid.New()
	no := "KTMB-K1"
	stub := &zincStub{t: t, bookingBody: checkBooking("Duplicate", nil),
		searchBody: []zinc.BookingPrincipalRes{{Id: other, BookingNo: &no}}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	scan, _ := scanReturning(&foundTicket{BookingNo: "KTMB-K1", TicketNo: "T-1"}, nil)

	res := c.CheckBooking(context.Background(), testBookingId, beforeDeparture(t), scan)

	if res.Status != CheckFound || res.ClaimedBy == nil || *res.ClaimedBy != other {
		t.Fatalf("expected Found claimed by %s, got %+v", other, res)
	}
	assertNoTransitions(t, stub)
}

func TestCheckFoundClaimLookupFailureStillReportsTicket(t *testing.T) {
	stub := &zincStub{t: t, bookingBody: checkBooking("Recovering", nil), searchStatus: 502}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	scan, _ := scanReturning(&foundTicket{BookingNo: "KTMB-K1", TicketNo: "T-1"}, nil)

	res := c.CheckBooking(context.Background(), testBookingId, beforeDeparture(t), scan)

	if res.Status != CheckFound || res.KtmbBookingNo != "KTMB-K1" || res.Error == "" {
		t.Fatalf("expected Found with a claim-lookup error, got %+v", res)
	}
	assertNoTransitions(t, stub)
}

func TestCheckNotFound(t *testing.T) {
	stub := &zincStub{t: t, bookingBody: checkBooking("Duplicate", nil)}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	scan, _ := scanReturning(nil, nil)

	res := c.CheckBooking(context.Background(), testBookingId, beforeDeparture(t), scan)

	if res.Status != CheckNotFound {
		t.Fatalf("expected NotFound, got %+v", res)
	}
	assertNoTransitions(t, stub)
}

// an inconclusive scan is an Error for the admin — never "not found"
func TestCheckInconclusiveScanIsError(t *testing.T) {
	stub := &zincStub{t: t, bookingBody: checkBooking("Recovering", nil)}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	scan, _ := scanReturning(nil, errors.New("empty ticket list"))

	res := c.CheckBooking(context.Background(), testBookingId, beforeDeparture(t), scan)

	if res.Status != CheckError || res.Error == "" {
		t.Fatalf("expected Error, got %+v", res)
	}
	assertNoTransitions(t, stub)
}

func TestCheckDepartedSkipsTheScan(t *testing.T) {
	stub := &zincStub{t: t, bookingBody: checkBooking("Duplicate", nil)}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	scan, calls := scanReturning(nil, nil)

	for _, now := range []time.Time{
		time.Date(2026, 7, 3, 13, 45, 0, 0, sgLoc(t)), // at departure
		time.Date(2026, 7, 4, 9, 0, 0, 0, sgLoc(t)),   // after
	} {
		if res := c.CheckBooking(context.Background(), testBookingId, now, scan); res.Status != CheckDeparted {
			t.Fatalf("expected Departed at %s, got %+v", now, res)
		}
	}
	if *calls != 0 {
		t.Errorf("a departed booking must not be scanned, got %d scans", *calls)
	}
	assertNoTransitions(t, stub)
}

func TestCheckMissingBookingIsError(t *testing.T) {
	stub := &zincStub{t: t}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	scan, calls := scanReturning(nil, nil)

	res := c.CheckBooking(context.Background(), testBookingId, beforeDeparture(t), scan)

	if res.Status != CheckError || *calls != 0 {
		t.Fatalf("expected Error without a scan, got %+v (scans=%d)", res, *calls)
	}
}

// the result write carries the ids and only the fields that are set
func TestReportCheckPostsResult(t *testing.T) {
	stub := &zincStub{t: t}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	checkId := uuid.New()
	claim := uuid.New()

	err := c.reportCheck(context.Background(), checkId, CheckResult{Status: CheckFound, KtmbBookingNo: "KTMB-K1", KtmbTicketNo: "T-1", ClaimedBy: &claim})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := stub.checkResults[checkId.String()]
	if !ok {
		t.Fatalf("no result posted for %s", checkId)
	}
	if got.Status != CheckFound || *got.KtmbBookingNo != "KTMB-K1" || *got.KtmbTicketNo != "T-1" || *got.ClaimedByBookingId != claim || got.Error != nil {
		t.Errorf("unexpected body %+v", got)
	}
	assertNoTransitions(t, stub)
}

func TestExpireRecoveryCallsZinc(t *testing.T) {
	stub := &zincStub{t: t, expireBody: []zinc.BookingPrincipalRes{{Id: uuid.New()}}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()

	if err := c.ExpireRecovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stub.expired != 1 {
		t.Errorf("expected one recovery-expire call, got %d", stub.expired)
	}
}
