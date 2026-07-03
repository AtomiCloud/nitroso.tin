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
//
// strict makes an empty/blank list inconclusive rather than definitively
// absent — use it in the re-scan after a KTMB conflict, where an empty own-
// account list is contradictory (KTMB just said the passenger is booked) and
// must never drive a refund.
func (c *Client) findTicket(userData string, target time.Time, direction, passport string, strict bool) (*foundTicket, error) {
	return findTicketIn(func(page int64) (ktmb.TicketListRes, error) {
		return c.listPage(userData, page)
	}, target, direction, passport, c.loc, strict)
}

// findTicketIn is the testable core: the list is ascending by departure
// datetime, so binary-search for a page whose datetime range spans the target,
// then scan every contiguous page that still contains the target datetime
// (same-datetime entries can straddle page boundaries).
func findTicketIn(fetch pageFetcher, target time.Time, direction, passport string, loc *time.Location, strict bool) (*foundTicket, error) {
	first, err := fetch(1)
	if err != nil {
		return nil, err
	}
	total := first.TotalPage
	if total <= 0 || len(first.Bookings) == 0 {
		if strict {
			return nil, fmt.Errorf("empty ticket list while a booking was expected (inconclusive)")
		}
		return nil, nil
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
		if f := matchPage(res, target, direction, passport, loc); f != nil {
			return f, nil
		}
		if !pageContainsDatetime(res, target, loc) {
			break
		}
	}
	// scan right from the page after the hit
	for page := hit + 1; page <= total; page++ {
		res, err := getPage(page)
		if err != nil {
			return nil, err
		}
		if !pageContainsDatetime(res, target, loc) {
			break
		}
		if f := matchPage(res, target, direction, passport, loc); f != nil {
			return f, nil
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
func pageContainsDatetime(page ktmb.TicketListRes, target time.Time, loc *time.Location) bool {
	for _, booking := range page.Bookings {
		if dt, err := departOf(booking, loc); err == nil && dt.Equal(target) {
			return true
		}
	}
	return false
}

func matchPage(page ktmb.TicketListRes, target time.Time, direction, passport string, loc *time.Location) *foundTicket {
	for _, booking := range page.Bookings {
		dt, err := departOf(booking, loc)
		if err != nil || !dt.Equal(target) {
			continue
		}
		if directionOf(booking.FromStationName) != direction {
			continue
		}
		for _, trip := range booking.Trips {
			for _, ticket := range trip.Tickets {
				if strings.EqualFold(strings.TrimSpace(ticket.PassengerPassportNo), strings.TrimSpace(passport)) {
					return &foundTicket{BookingNo: booking.BookingNo, TicketNo: ticket.TicketNo}
				}
			}
		}
	}
	return nil
}

func departOf(booking ktmb.TicketListBookingRes, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation(ktmbDateTimeLayout, booking.DepartFromLocalDateTime, loc)
}

// directionOf maps KTMB station names to the platform's direction strings:
// departing Woodlands => WToJ, departing JB Sentral => JToW
func directionOf(fromStationName string) string {
	if strings.Contains(strings.ToUpper(fromStationName), "WOODLANDS") {
		return "WToJ"
	}
	return "JToW"
}
