package recoverer

import (
	"fmt"
	"testing"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
)

func sgLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func testClient(t *testing.T) *Client {
	t.Helper()
	return &Client{loc: sgLoc(t)}
}

// ticket builds a one-ticket booking at the given departure datetime.
func ticket(depart, from, bookingNo, ticketNo, passport string) ktmb.TicketListBookingRes {
	return ktmb.TicketListBookingRes{
		BookingNo:               bookingNo,
		DepartFromLocalDateTime: depart,
		FromStationName:         from,
		Trips: []ktmb.TicketListTripRes{
			{Tickets: []ktmb.TicketListTicketRes{{TicketNo: ticketNo, PassengerPassportNo: passport}}},
		},
	}
}

// paginate splits bookings into fixed-size pages and returns a pageFetcher plus
// a call-counter, emulating KTMB's ascending, paginated UpcomingShuttleList.
func paginate(bookings []ktmb.TicketListBookingRes, pageSize int) (pageFetcher, *int) {
	total := int64((len(bookings) + pageSize - 1) / pageSize)
	if total == 0 {
		total = 1
	}
	calls := 0
	return func(page int64) (ktmb.TicketListRes, error) {
		calls++
		start := int(page-1) * pageSize
		end := start + pageSize
		if start > len(bookings) {
			start = len(bookings)
		}
		if end > len(bookings) {
			end = len(bookings)
		}
		return ktmb.TicketListRes{
			Page:      page,
			TotalPage: total,
			Bookings:  bookings[start:end],
		}, nil
	}, &calls
}

func TestDirectionOf(t *testing.T) {
	cases := []struct {
		from string
		want string
	}{
		{"WOODLANDS CIQ", "WToJ"},
		{"woodlands ciq", "WToJ"},
		{"JB SENTRAL", "JToW"},
	}
	for _, tc := range cases {
		if got := directionOf(tc.from); got != tc.want {
			t.Errorf("directionOf(%q) = %q, want %q", tc.from, got, tc.want)
		}
	}
}

func TestMatchPage(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)

	page := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			ticket("2026-07-03T13:45:00", "WOODLANDS CIQ", "KST0001", "TST0001", "E1234567X"),
			ticket("2026-07-03T13:45:00", "JB SENTRAL", "KST0002", "TST0002", "E1234567X"),
			ticket("2026-07-03T15:00:00", "WOODLANDS CIQ", "KST0003", "TST0003", "E1234567X"),
		},
	}

	found := matchPage(page, target, "WToJ", "E1234567X", loc)
	if found == nil || found.BookingNo != "KST0001" || found.TicketNo != "TST0001" {
		t.Errorf("expected KST0001/TST0001, got %+v", found)
	}

	// direction must disambiguate same-datetime bookings
	found = matchPage(page, target, "JToW", "E1234567X", loc)
	if found == nil || found.BookingNo != "KST0002" {
		t.Errorf("expected KST0002 for JToW, got %+v", found)
	}

	// passport is matched case-insensitively and trimmed
	found = matchPage(page, target, "WToJ", " e1234567x ", loc)
	if found == nil || found.BookingNo != "KST0001" {
		t.Errorf("expected KST0001 for case-insensitive passport, got %+v", found)
	}

	// wrong passport: no match
	if found = matchPage(page, target, "WToJ", "X0000000", loc); found != nil {
		t.Errorf("expected no match for unknown passport, got %+v", found)
	}

	// wrong datetime: no match
	other := time.Date(2026, 7, 4, 13, 45, 0, 0, loc)
	if found = matchPage(page, other, "WToJ", "E1234567X", loc); found != nil {
		t.Errorf("expected no match for other datetime, got %+v", found)
	}
}

func TestFindTicketIn(t *testing.T) {
	loc := sgLoc(t)
	// 25 bookings, ascending, two per hour starting 2026-07-01 10:00, page size 4.
	// Every departure has two bookings (WToJ then JToW) so same-datetime entries
	// straddle page boundaries.
	var all []ktmb.TicketListBookingRes
	base := time.Date(2026, 7, 1, 10, 0, 0, 0, loc)
	for i := 0; i < 20; i++ {
		dt := base.Add(time.Duration(i) * time.Hour).Format(ktmbDateTimeLayout)
		all = append(all,
			ticket(dt, "WOODLANDS CIQ", fmt.Sprintf("KSTW%02d", i), fmt.Sprintf("TSTW%02d", i), fmt.Sprintf("PPW%02d", i)),
			ticket(dt, "JB SENTRAL", fmt.Sprintf("KSTJ%02d", i), fmt.Sprintf("TSTJ%02d", i), fmt.Sprintf("PPJ%02d", i)),
		)
	}

	find := func(hour int, dir, passport string) (*foundTicket, error) {
		fetch, _ := paginate(all, 4)
		target := base.Add(time.Duration(hour) * time.Hour)
		return findTicketIn(fetch, target, dir, passport, loc)
	}

	// target on page 1
	if f, err := find(0, "WToJ", "PPW00"); err != nil || f == nil || f.BookingNo != "KSTW00" {
		t.Errorf("page-1 target: got %+v err %v", f, err)
	}
	// target requiring bisection, JToW half of a same-datetime pair
	if f, err := find(10, "JToW", "PPJ10"); err != nil || f == nil || f.BookingNo != "KSTJ10" {
		t.Errorf("bisected target: got %+v err %v", f, err)
	}
	// last departure
	if f, err := find(19, "JToW", "PPJ19"); err != nil || f == nil || f.BookingNo != "KSTJ19" {
		t.Errorf("last target: got %+v err %v", f, err)
	}
	// passport present but wrong direction at that datetime → not found
	if f, err := find(10, "WToJ", "PPJ10"); err != nil || f != nil {
		t.Errorf("wrong-direction: expected nil,nil got %+v err %v", f, err)
	}
	// datetime exists but passport absent → definitively not found
	if f, err := find(10, "WToJ", "NOPE"); err != nil || f != nil {
		t.Errorf("absent passport: expected nil,nil got %+v err %v", f, err)
	}
	// datetime beyond the list entirely → not found
	fetch, _ := paginate(all, 4)
	beyond := base.Add(100 * time.Hour)
	if f, err := findTicketIn(fetch, beyond, "WToJ", "PPW00", loc); err != nil || f != nil {
		t.Errorf("beyond-range: expected nil,nil got %+v err %v", f, err)
	}
}

func TestFindTicketInSpanningManyPages(t *testing.T) {
	loc := sgLoc(t)
	// 10 bookings ALL at the same departure datetime, page size 3 → the target
	// datetime spans 4 pages; the passenger is on the last one.
	dt := "2026-07-03T13:45:00"
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)
	var all []ktmb.TicketListBookingRes
	for i := 0; i < 9; i++ {
		all = append(all, ticket(dt, "WOODLANDS CIQ", fmt.Sprintf("KST%02d", i), fmt.Sprintf("TST%02d", i), fmt.Sprintf("PP%02d", i)))
	}
	all = append(all, ticket(dt, "WOODLANDS CIQ", "KSTLAST", "TSTLAST", "TARGET"))

	fetch, _ := paginate(all, 3)
	f, err := findTicketIn(fetch, target, "WToJ", "TARGET", loc)
	if err != nil || f == nil || f.BookingNo != "KSTLAST" {
		t.Errorf("spanning pages: expected KSTLAST, got %+v err %v", f, err)
	}
}

func TestFindTicketInInconclusiveOnEmptyMidPage(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)
	// page 1 reports a wide range and total of 3, but page 2 (inside the range)
	// comes back empty — the scan must report inconclusive, not "not found".
	fetch := func(page int64) (ktmb.TicketListRes, error) {
		switch page {
		case 1:
			return ktmb.TicketListRes{Page: 1, TotalPage: 3, Bookings: []ktmb.TicketListBookingRes{
				ticket("2026-07-03T10:00:00", "WOODLANDS CIQ", "A", "A", "AA"),
				ticket("2026-07-03T20:00:00", "WOODLANDS CIQ", "B", "B", "BB"),
			}}, nil
		default:
			return ktmb.TicketListRes{Page: page, TotalPage: 3, Bookings: nil}, nil
		}
	}
	if _, err := findTicketIn(fetch, target, "WToJ", "AA", loc); err == nil {
		t.Error("expected inconclusive error on empty mid-page, got nil")
	}
}
