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

func TestClassifyDirection(t *testing.T) {
	cases := []struct {
		from          string
		wantDir       string
		wantConfirmed bool
	}{
		{"WOODLANDS CIQ", "WToJ", true},
		{"woodlands ciq", "WToJ", true},
		{"JB SENTRAL", "JToW", true},
		{"jb sentral", "JToW", true},
		{"JOHOR BAHRU", "JToW", true},
		// unconfirmed: neither a Woodlands nor a JB-Sentral/Johor origin token.
		// Crucially these must NOT default to "JToW" — that false default is the
		// bug the tri-state classifier closes.
		{"WDL", "", false},
		{"", "", false},
		{"UNKNOWN STATION", "", false},
	}
	for _, tc := range cases {
		gotDir, gotConfirmed := classifyDirection(tc.from)
		if gotDir != tc.wantDir || gotConfirmed != tc.wantConfirmed {
			t.Errorf("classifyDirection(%q) = (%q, %t), want (%q, %t)",
				tc.from, gotDir, gotConfirmed, tc.wantDir, tc.wantConfirmed)
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

	found, err := matchPage(page, target, "WToJ", "E1234567X", loc)
	if err != nil || found == nil || found.BookingNo != "KST0001" || found.TicketNo != "TST0001" {
		t.Errorf("expected KST0001/TST0001, got %+v err %v", found, err)
	}

	// direction must disambiguate same-datetime bookings
	found, err = matchPage(page, target, "JToW", "E1234567X", loc)
	if err != nil || found == nil || found.BookingNo != "KST0002" {
		t.Errorf("expected KST0002 for JToW, got %+v err %v", found, err)
	}

	// passport is matched case-insensitively and trimmed
	found, err = matchPage(page, target, "WToJ", " e1234567x ", loc)
	if err != nil || found == nil || found.BookingNo != "KST0001" {
		t.Errorf("expected KST0001 for case-insensitive passport, got %+v err %v", found, err)
	}

	// wrong passport: no match
	if found, err = matchPage(page, target, "WToJ", "X0000000", loc); err != nil || found != nil {
		t.Errorf("expected no match for unknown passport, got %+v err %v", found, err)
	}

	// wrong datetime: no match
	other := time.Date(2026, 7, 4, 13, 45, 0, 0, loc)
	if found, err = matchPage(page, other, "WToJ", "E1234567X", loc); err != nil || found != nil {
		t.Errorf("expected no match for other datetime, got %+v err %v", found, err)
	}
}

// A WToJ booking whose KTMB FromStationName does NOT contain "WOODLANDS"
// (abbreviated/localized/blank) is misclassified by directionOf as "JToW". Such
// a row, carrying OUR passport at the target datetime, must surface as
// inconclusive (retry), never be silently skipped — a silent skip reads as "not
// ours" and, via the re-buy conflict path, drives a wrongful refund of a ticket
// we actually hold (§3.3).
func TestMatchPageDirectionUnconfirmedIsInconclusive(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)

	// our WToJ ticket, but the station string can't be classified as Woodlands
	page := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			ticket("2026-07-03T13:45:00", "WDL", "KST0001", "TST0001", "E1234567X"),
		},
	}
	if found, err := matchPage(page, target, "WToJ", "E1234567X", loc); err == nil || found != nil {
		t.Errorf("expected inconclusive error for direction-unconfirmed own ticket, got %+v err %v", found, err)
	}

	// a definitive same-direction match elsewhere on the page still wins over the
	// misclassified sibling (force-completing a found ticket is money-safe)
	page.Bookings = append(page.Bookings,
		ticket("2026-07-03T13:45:00", "WOODLANDS CIQ", "KST0002", "TST0002", "E1234567X"))
	found, err := matchPage(page, target, "WToJ", "E1234567X", loc)
	if err != nil || found == nil || found.BookingNo != "KST0002" {
		t.Errorf("expected definitive match to win over misclassified sibling, got %+v err %v", found, err)
	}
}

// Symmetric to TestMatchPageDirectionUnconfirmedIsInconclusive, for a JToW
// target. Before the tri-state classifier, directionOf defaulted any
// non-Woodlands station to "JToW", so a WToJ ticket carrying our passport at the
// target datetime with an ambiguous FromStationName (e.g. "WDL") false-matched a
// JToW target as a DEFINITIVE hit → wrongful force-complete against the
// wrong-direction ticket. The classifier now confirms JToW only via a positive
// JB-Sentral/Johor token, so an unconfirmed station is inconclusive (retry) for
// the JToW target too, closing the mirror of the WToJ hole.
func TestMatchPageJToWDirectionUnconfirmedIsInconclusive(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)

	// our ticket at the target datetime, station string not positively JToW
	page := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			ticket("2026-07-03T13:45:00", "WDL", "KST0001", "TST0001", "E1234567X"),
		},
	}
	if found, err := matchPage(page, target, "JToW", "E1234567X", loc); err == nil || found != nil {
		t.Errorf("expected inconclusive error for direction-unconfirmed own ticket on JToW target, got %+v err %v", found, err)
	}

	// a blank station is likewise unconfirmed → inconclusive, never a default JToW match
	blank := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			ticket("2026-07-03T13:45:00", "", "KST0001", "TST0001", "E1234567X"),
		},
	}
	if found, err := matchPage(blank, target, "JToW", "E1234567X", loc); err == nil || found != nil {
		t.Errorf("expected inconclusive error for blank station on JToW target, got %+v err %v", found, err)
	}

	// a definitive JB Sentral match elsewhere on the page still wins
	page.Bookings = append(page.Bookings,
		ticket("2026-07-03T13:45:00", "JB SENTRAL", "KST0002", "TST0002", "E1234567X"))
	found, err := matchPage(page, target, "JToW", "E1234567X", loc)
	if err != nil || found == nil || found.BookingNo != "KST0002" {
		t.Errorf("expected definitive JToW match to win over misclassified sibling, got %+v err %v", found, err)
	}
}

// An UNRELATED passenger's opposite-direction booking at the same datetime must
// NOT make the page inconclusive: passport is checked before direction, so a
// different passenger's ticket is simply ignored. Otherwise every same-instant
// opposite-direction booking on our account would wedge the scanner.
func TestMatchPageOtherPassengerOtherDirectionIgnored(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)

	page := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			ticket("2026-07-03T13:45:00", "JB SENTRAL", "KST0001", "TST0001", "OTHER"),
			ticket("2026-07-03T13:45:00", "WOODLANDS CIQ", "KST0002", "TST0002", "E1234567X"),
		},
	}
	found, err := matchPage(page, target, "WToJ", "E1234567X", loc)
	if err != nil || found == nil || found.BookingNo != "KST0002" {
		t.Errorf("expected KST0002, unrelated opposite-direction row ignored, got %+v err %v", found, err)
	}

	// and with only the unrelated opposite-direction passenger present, our
	// passport is genuinely absent → definitively not found (nil, nil), NOT
	// inconclusive
	only := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			ticket("2026-07-03T13:45:00", "JB SENTRAL", "KST0001", "TST0001", "OTHER"),
		},
	}
	if found, err := matchPage(only, target, "WToJ", "E1234567X", loc); err != nil || found != nil {
		t.Errorf("expected definitive absence (nil,nil) for absent passport, got %+v err %v", found, err)
	}
}

func TestMatchPageMalformedDatetime(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)

	// the MATCHING row's datetime is malformed: the page must be inconclusive
	// (error), never a silent "no match" that would end in a wrongful refund
	malformed := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			ticket("garbage-datetime", "WOODLANDS CIQ", "KST0001", "TST0001", "E1234567X"),
			ticket("2026-07-03T15:00:00", "WOODLANDS CIQ", "KST0003", "TST0003", "E1234567X"),
		},
	}
	if found, err := matchPage(malformed, target, "WToJ", "E1234567X", loc); err == nil {
		t.Errorf("expected inconclusive error for malformed matching row, got %+v err nil", found)
	}

	// a definitive match elsewhere on the page wins over a malformed sibling
	// row: force-completing a found ticket is money-safe
	withMatch := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			ticket("garbage-datetime", "WOODLANDS CIQ", "KST0001", "TST0001", "OTHER"),
			ticket("2026-07-03T13:45:00", "WOODLANDS CIQ", "KST0002", "TST0002", "E1234567X"),
		},
	}
	found, err := matchPage(withMatch, target, "WToJ", "E1234567X", loc)
	if err != nil || found == nil || found.BookingNo != "KST0002" {
		t.Errorf("expected definitive match to win over malformed sibling, got %+v err %v", found, err)
	}
}

func TestPageContainsDatetimeMalformed(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)

	// malformed row, no definitive hit → inconclusive error, not "no"
	page := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			ticket("garbage-datetime", "WOODLANDS CIQ", "KST0001", "TST0001", "AA"),
			ticket("2026-07-03T15:00:00", "WOODLANDS CIQ", "KST0002", "TST0002", "BB"),
		},
	}
	if contains, err := pageContainsDatetime(page, target, loc); err == nil {
		t.Errorf("expected inconclusive error for malformed row, got contains=%v err nil", contains)
	}

	// a definitive hit wins over a malformed sibling row
	page.Bookings = append(page.Bookings, ticket("2026-07-03T13:45:00", "WOODLANDS CIQ", "KST0003", "TST0003", "CC"))
	if contains, err := pageContainsDatetime(page, target, loc); err != nil || !contains {
		t.Errorf("expected definitive containment to win, got contains=%v err %v", contains, err)
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
	// passport present at that datetime but on a booking classified as the other
	// direction → inconclusive (retry), never a silent "not found": a real
	// passenger cannot hold two opposite-direction tickets at the same instant,
	// so the safe read is that our ticket's station string was misclassified.
	if f, err := find(10, "WToJ", "PPJ10"); err == nil || f != nil {
		t.Errorf("wrong-direction: expected inconclusive error, got %+v err %v", f, err)
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

func TestFindTicketInMalformedMatchingRow(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)
	// single page; the row that WOULD match the target passport has a malformed
	// departure. The scan must be inconclusive (retry), never "not found" —
	// "not found" leads to a re-buy probe and, after a conflict + a re-scan
	// that also skips the row, a wrongful refund.
	fetch := func(page int64) (ktmb.TicketListRes, error) {
		return ktmb.TicketListRes{Page: 1, TotalPage: 1, Bookings: []ktmb.TicketListBookingRes{
			ticket("2026-07-03T10:00:00", "WOODLANDS CIQ", "A", "A", "AA"),
			ticket("garbage-datetime", "WOODLANDS CIQ", "KST0001", "TST0001", "TARGET"),
			ticket("2026-07-03T20:00:00", "WOODLANDS CIQ", "B", "B", "BB"),
		}}, nil
	}
	if f, err := findTicketIn(fetch, target, "WToJ", "TARGET", loc); err == nil {
		t.Errorf("expected inconclusive error for malformed matching row, got %+v err nil", f)
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

func TestFindTicketInEmptyListInconclusive(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)
	empty := func(page int64) (ktmb.TicketListRes, error) {
		return ktmb.TicketListRes{Page: 1, TotalPage: 0, Bookings: nil}, nil
	}
	// an empty/blank list is ALWAYS inconclusive (spec §3.3/§5.6): it must
	// retry, never read as "not on our account" — that would trigger a re-buy
	// probe (and, post-conflict, could drive a refund of a held ticket)
	if _, err := findTicketIn(empty, target, "WToJ", "AA", loc); err == nil {
		t.Error("empty list: expected inconclusive error, got nil")
	}
}

func TestFindTicketInEmptyContiguousScanPage(t *testing.T) {
	loc := sgLoc(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, loc)
	// page 1 is the bisection hit: it holds a DIFFERENT passenger at the target
	// datetime (so the page contains the target datetime but yields no match for
	// our passport). page 2 — reachable only by the contiguous scan-right, never
	// the bisection — comes back empty, though it could hold OUR ticket. The scan
	// must report inconclusive, never a "not found" that would drive a re-buy /
	// wrongful refund of a held ticket (spec §3.3).
	fetch := func(page int64) (ktmb.TicketListRes, error) {
		switch page {
		case 1:
			return ktmb.TicketListRes{Page: 1, TotalPage: 2, Bookings: []ktmb.TicketListBookingRes{
				ticket("2026-07-03T13:45:00", "WOODLANDS CIQ", "KSTOTHER", "TSTOTHER", "OTHERPASS"),
			}}, nil
		default:
			return ktmb.TicketListRes{Page: page, TotalPage: 2, Bookings: nil}, nil
		}
	}
	if f, err := findTicketIn(fetch, target, "WToJ", "E1234567X", loc); err == nil {
		t.Errorf("empty contiguous-scan page: expected inconclusive error, got %+v err nil", f)
	}
}
