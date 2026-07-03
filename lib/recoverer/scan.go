package recoverer

import (
	"fmt"
	"strings"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
)

const ktmbDateTimeLayout = "2006-01-02T15:04:05"

// FoundTicket is a ticket on our KTMB account matching a recover item
type FoundTicket struct {
	BookingNo string
	TicketNo  string
}

// findTicket locates a ticket for the passport on the target departure via
// KTMB's upcoming-ticket list. The list is paginated and ascending by
// departure datetime, so binary-search the page containing the target, then
// scan outward across pages sharing the boundary datetime.
func (c *Client) findTicket(userData string, target time.Time, direction, passport string) (*FoundTicket, error) {
	first, err := c.listPage(userData, 1)
	if err != nil {
		return nil, err
	}
	totalPage := first.TotalPage
	if totalPage <= 0 || len(first.Bookings) == 0 {
		return nil, nil
	}

	if f := c.matchPage(first, target, direction, passport); f != nil {
		return f, nil
	}

	lo, hi := int64(2), totalPage
	candidate := int64(0)
	for lo <= hi {
		mid := (lo + hi) / 2
		page, err := c.listPage(userData, mid)
		if err != nil {
			return nil, err
		}
		if len(page.Bookings) == 0 {
			break
		}
		if f := c.matchPage(page, target, direction, passport); f != nil {
			return f, nil
		}

		firstDt, err := c.departOf(page.Bookings[0])
		if err != nil {
			return nil, err
		}
		lastDt, err := c.departOf(page.Bookings[len(page.Bookings)-1])
		if err != nil {
			return nil, err
		}

		switch {
		case target.Before(firstDt):
			hi = mid - 1
		case target.After(lastDt):
			lo = mid + 1
		default:
			// target datetime falls inside this page but did not match: entries
			// with the same departure may spill into neighbouring pages
			candidate = mid
			lo = hi + 1
		}
	}

	if candidate == 0 {
		return nil, nil
	}

	// scan neighbours that share the boundary departure datetime
	for _, page := range []int64{candidate - 1, candidate + 1} {
		if page < 1 || page > totalPage {
			continue
		}
		res, err := c.listPage(userData, page)
		if err != nil {
			return nil, err
		}
		if f := c.matchPage(res, target, direction, passport); f != nil {
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

func (c *Client) matchPage(page ktmb.TicketListRes, target time.Time, direction, passport string) *FoundTicket {
	for _, booking := range page.Bookings {
		dt, err := c.departOf(booking)
		if err != nil || !dt.Equal(target) {
			continue
		}
		if directionOf(booking.FromStationName) != direction {
			continue
		}
		for _, trip := range booking.Trips {
			for _, ticket := range trip.Tickets {
				if strings.EqualFold(strings.TrimSpace(ticket.PassengerPassportNo), strings.TrimSpace(passport)) {
					return &FoundTicket{BookingNo: booking.BookingNo, TicketNo: ticket.TicketNo}
				}
			}
		}
	}
	return nil
}

func (c *Client) departOf(booking ktmb.TicketListBookingRes) (time.Time, error) {
	return time.ParseInLocation(ktmbDateTimeLayout, booking.DepartFromLocalDateTime, c.loc)
}

// directionOf maps KTMB station names to the platform's direction strings:
// departing Woodlands ⇒ WToJ, departing JB Sentral ⇒ JToW
func directionOf(fromStationName string) string {
	if strings.Contains(strings.ToUpper(fromStationName), "WOODLANDS") {
		return "WToJ"
	}
	return "JToW"
}
