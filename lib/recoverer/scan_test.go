package recoverer

import (
	"testing"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
)

func testClient(t *testing.T) *Client {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		t.Fatal(err)
	}
	return &Client{loc: loc}
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
	c := testClient(t)
	target := time.Date(2026, 7, 3, 13, 45, 0, 0, c.loc)

	page := ktmb.TicketListRes{
		Bookings: []ktmb.TicketListBookingRes{
			{
				BookingNo:               "KST0001",
				DepartFromLocalDateTime: "2026-07-03T13:45:00",
				FromStationName:         "WOODLANDS CIQ",
				Trips: []ktmb.TicketListTripRes{
					{Tickets: []ktmb.TicketListTicketRes{{TicketNo: "TST0001", PassengerPassportNo: "E1234567X"}}},
				},
			},
			{
				BookingNo:               "KST0002",
				DepartFromLocalDateTime: "2026-07-03T13:45:00",
				FromStationName:         "JB SENTRAL",
				Trips: []ktmb.TicketListTripRes{
					{Tickets: []ktmb.TicketListTicketRes{{TicketNo: "TST0002", PassengerPassportNo: "E1234567X"}}},
				},
			},
			{
				BookingNo:               "KST0003",
				DepartFromLocalDateTime: "2026-07-03T15:00:00",
				FromStationName:         "WOODLANDS CIQ",
				Trips: []ktmb.TicketListTripRes{
					{Tickets: []ktmb.TicketListTicketRes{{TicketNo: "TST0003", PassengerPassportNo: "E1234567X"}}},
				},
			},
		},
	}

	found := c.matchPage(page, target, "WToJ", "E1234567X")
	if found == nil || found.BookingNo != "KST0001" || found.TicketNo != "TST0001" {
		t.Errorf("expected KST0001/TST0001, got %+v", found)
	}

	// direction must disambiguate same-datetime bookings
	found = c.matchPage(page, target, "JToW", "E1234567X")
	if found == nil || found.BookingNo != "KST0002" {
		t.Errorf("expected KST0002 for JToW, got %+v", found)
	}

	// passport is matched case-insensitively and trimmed
	found = c.matchPage(page, target, "WToJ", " e1234567x ")
	if found == nil || found.BookingNo != "KST0001" {
		t.Errorf("expected KST0001 for case-insensitive passport, got %+v", found)
	}

	// wrong passport: no match
	if found = c.matchPage(page, target, "WToJ", "X0000000"); found != nil {
		t.Errorf("expected no match for unknown passport, got %+v", found)
	}

	// wrong datetime: no match
	other := time.Date(2026, 7, 4, 13, 45, 0, 0, c.loc)
	if found = c.matchPage(page, other, "WToJ", "E1234567X"); found != nil {
		t.Errorf("expected no match for other datetime, got %+v", found)
	}
}
