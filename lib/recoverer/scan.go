package recoverer

import (
	"fmt"
	"strings"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
)

const ktmbDateTimeLayout = "2006-01-02T15:04:05"

// foundTicket is a ticket on our KTMB account matching a recover item
type foundTicket struct {
	BookingNo string
	TicketNo  string
}

// pageFetcher fetches one page of the KTMB upcoming-ticket list (1-indexed).
type pageFetcher func(page int64) (ktmb.TicketListRes, error)

// findTicket locates a ticket for the passport on the target departure via
// KTMB's upcoming-ticket list. Returns (nil, nil) only when the ticket is
// definitively absent; an error means the scan was inconclusive (the caller
// must retry rather than treat it as "not ours", which would refund a user
// who actually holds a valid ticket).
func (c *Client) findTicket(userData string, target time.Time, direction, passport string) (*foundTicket, error) {
	return findTicketIn(func(page int64) (ktmb.TicketListRes, error) {
		return c.listPage(userData, page)
	}, target, direction, passport, c.loc)
}

// findTicketIn is the testable core: the list is ascending by departure
// datetime, so binary-search for a page whose datetime range spans the target,
// then scan every contiguous page that still contains the target datetime
// (same-datetime entries can straddle page boundaries).
//
// Inconclusive conditions all surface as errors, never as "not found": an
// empty/blank list (our production account always holds tickets, so a blank
// page is a scrape failure until proven otherwise — spec §3.3), an empty page
// inside the known range (list mutated under us), and an unparseable departure
// datetime on any page that could hold the target (the malformed row could BE
// the target's row). A definitive passport+datetime+direction match found on a
// page always wins over a malformed sibling row: acting on a found ticket is a
// force-complete, which is money-safe, whereas failing to report it would only
// delay recovery.
func findTicketIn(fetch pageFetcher, target time.Time, direction, passport string, loc *time.Location) (*foundTicket, error) {
	first, err := fetch(1)
	if err != nil {
		return nil, err
	}
	total := first.TotalPage
	if total <= 0 || len(first.Bookings) == 0 {
		return nil, fmt.Errorf("empty ticket list (inconclusive, must retry — never treat as absent)")
	}

	getPage := func(page int64) (ktmb.TicketListRes, error) {
		if page == 1 {
			return first, nil
		}
		return fetch(page)
	}

	// binary search for a page whose [firstDepart, lastDepart] spans target
	lo, hi := int64(1), total
	hit := int64(0)
	for lo <= hi {
		mid := (lo + hi) / 2
		page, err := getPage(mid)
		if err != nil {
			return nil, err
		}
		if len(page.Bookings) == 0 {
			// a page inside the known range came back empty: the list mutated
			// under us — inconclusive, retry
			return nil, fmt.Errorf("empty page %d within range during scan", mid)
		}
		firstDt, err := departOf(page.Bookings[0], loc)
		if err != nil {
			return nil, err
		}
		lastDt, err := departOf(page.Bookings[len(page.Bookings)-1], loc)
		if err != nil {
			return nil, err
		}
		switch {
		case target.Before(firstDt):
			hi = mid - 1
		case target.After(lastDt):
			lo = mid + 1
		default:
			hit = mid
			lo = hi + 1 // found a spanning page, exit the search
		}
	}
	if hit == 0 {
		return nil, nil // target datetime falls outside every page's range
	}

	// scan left from the hit page while pages still contain the target datetime
	for page := hit; page >= 1; page-- {
		res, err := getPage(page)
		if err != nil {
			return nil, err
		}
		if len(res.Bookings) == 0 {
			// an empty page reached only via the contiguous scan (never the
			// bisection) is inconclusive, not "not found": the target's row may
			// live on this now-blank page. Treating it as absent could drive a
			// wrongful refund (spec §3.3).
			return nil, fmt.Errorf("empty page %d during left contiguous scan (inconclusive, must retry)", page)
		}
		f, matchErr := matchPage(res, target, direction, passport, loc)
		if f != nil {
			return f, nil
		}
		if matchErr != nil {
			return nil, matchErr
		}
		contains, containsErr := pageContainsDatetime(res, target, loc)
		if containsErr != nil {
			return nil, containsErr
		}
		if !contains {
			break
		}
	}
	// scan right from the page after the hit
	for page := hit + 1; page <= total; page++ {
		res, err := getPage(page)
		if err != nil {
			return nil, err
		}
		if len(res.Bookings) == 0 {
			// empty page in the contiguous scan → inconclusive (see scan-left)
			return nil, fmt.Errorf("empty page %d during right contiguous scan (inconclusive, must retry)", page)
		}
		f, matchErr := matchPage(res, target, direction, passport, loc)
		if f != nil {
			return f, nil
		}
		if matchErr != nil {
			return nil, matchErr
		}
		contains, containsErr := pageContainsDatetime(res, target, loc)
		if containsErr != nil {
			return nil, containsErr
		}
		if !contains {
			break
		}
	}
	return nil, nil
}

func (c *Client) listPage(userData string, page int64) (ktmb.TicketListRes, error) {
	res, err := c.ktmb.ListTicket(userData, page)
	if err != nil {
		return ktmb.TicketListRes{}, err
	}
	if !res.Status {
		return ktmb.TicketListRes{}, fmt.Errorf("failed to list tickets (page %d): %+v", page, res.Messages)
	}
	return res.Data, nil
}

// pageContainsDatetime reports whether any booking on the page departs exactly
// at target — i.e. whether the target datetime's entries may extend further.
// A definitive hit wins; otherwise an unparseable departure is an error (the
// malformed row could be at the target datetime), never a silent "no".
func pageContainsDatetime(page ktmb.TicketListRes, target time.Time, loc *time.Location) (bool, error) {
	var parseErr error
	for _, booking := range page.Bookings {
		dt, err := departOf(booking, loc)
		if err != nil {
			parseErr = fmt.Errorf("unparseable departure %q on booking %s (inconclusive): %w",
				booking.DepartFromLocalDateTime, booking.BookingNo, err)
			continue
		}
		if dt.Equal(target) {
			return true, nil
		}
	}
	return false, parseErr
}

// matchPage looks for the passport's ticket at the target datetime+direction
// on one page. A definitive match wins; otherwise any ambiguity on a row that
// could BE the target's row is an error (inconclusive), never a silent skip —
// a skipped true match would end in a wrongful refund (§3.3). Two dimensions
// can be ambiguous:
//
//   - an unparseable departure datetime (the malformed row could be at the
//     target datetime), and
//   - a datetime+passport match whose direction cannot be *confirmed* equal to
//     the target. The passport is checked BEFORE direction so an unrelated
//     passenger's opposite-direction booking at the same instant is still
//     ignored, but our OWN passenger's booking at the target datetime whose
//     FromStationName cannot be positively classified as the target direction
//     (abbreviated/localized/blank) must NOT be silently skipped nor treated as
//     a definitive match: a real passenger cannot hold two opposite-direction
//     tickets at the same instant, so such a row is almost certainly our
//     misclassified ticket. Classification is tri-state (confirmed-WToJ,
//     confirmed-JToW, unconfirmed): a station is a definitive match ONLY when it
//     is *positively* confirmed as the target direction. An unconfirmed station
//     is inconclusive for BOTH target directions — a station that merely fails
//     to be Woodlands is NOT thereby "JToW". Skipping (or force-completing) such
//     a row reads as "not ours" / "the wrong-direction ticket is ours" and
//     drives a wrongful refund or a wrongful force-complete; instead surface it
//     as inconclusive so the caller retries (and ultimately parks for a human).
func matchPage(page ktmb.TicketListRes, target time.Time, direction, passport string, loc *time.Location) (*foundTicket, error) {
	var inconclusiveErr error
	for _, booking := range page.Bookings {
		dt, err := departOf(booking, loc)
		if err != nil {
			inconclusiveErr = fmt.Errorf("unparseable departure %q on booking %s (inconclusive): %w",
				booking.DepartFromLocalDateTime, booking.BookingNo, err)
			continue
		}
		if !dt.Equal(target) {
			continue
		}
		ticketNo, hasPassport := ticketForPassport(booking, passport)
		if !hasPassport {
			continue
		}
		dir, confirmed := classifyDirection(booking.FromStationName)
		if !confirmed || dir != direction {
			inconclusiveErr = fmt.Errorf(
				"passport %s present on booking %s at target datetime but station %q could not be positively confirmed as target direction %q (classified=%q confirmed=%t) (inconclusive)",
				passport, booking.BookingNo, booking.FromStationName, direction, dir, confirmed)
			continue
		}
		return &foundTicket{BookingNo: booking.BookingNo, TicketNo: ticketNo}, nil
	}
	return nil, inconclusiveErr
}

// ticketForPassport returns the ticket number for the passport on this booking
// (matched case-insensitively and trimmed) and whether it was present.
func ticketForPassport(booking ktmb.TicketListBookingRes, passport string) (string, bool) {
	for _, trip := range booking.Trips {
		for _, ticket := range trip.Tickets {
			if strings.EqualFold(strings.TrimSpace(ticket.PassengerPassportNo), strings.TrimSpace(passport)) {
				return ticket.TicketNo, true
			}
		}
	}
	return "", false
}

func departOf(booking ktmb.TicketListBookingRes, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation(ktmbDateTimeLayout, booking.DepartFromLocalDateTime, loc)
}

// classifyDirection maps a KTMB station name to the platform's direction
// strings, tri-state: it returns (direction, confirmed). A direction is only
// returned with confirmed==true when the station name POSITIVELY identifies its
// origin — departing Woodlands => WToJ, departing JB Sentral => JToW. Any other
// string (abbreviated, localized, blank, unknown) is unconfirmed ("", false):
// crucially, a station that merely does NOT contain "WOODLANDS" is NOT thereby
// "JToW". Callers must treat an unconfirmed station as inconclusive for BOTH
// target directions — a positive JToW confirmation requires a JToW origin token,
// never the absence of a Woodlands token. This symmetry is load-bearing: without
// it, a WToJ ticket with an ambiguous station would false-match a JToW target and
// drive a wrongful force-complete (the mirror of the WToJ wrongful-refund path).
func classifyDirection(fromStationName string) (string, bool) {
	upper := strings.ToUpper(fromStationName)
	if strings.Contains(upper, "WOODLANDS") {
		return "WToJ", true
	}
	// JB Sentral is the JToW origin; match its distinctive tokens only.
	if strings.Contains(upper, "SENTRAL") || strings.Contains(upper, "JOHOR") {
		return "JToW", true
	}
	return "", false
}
