package buyer

import (
	"testing"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
)

func TestTicketIdsOf(t *testing.T) {
	wellFormed := ktmb.CompleteRes{Booking: ktmb.CompleteBookingRes{
		BookingNo: "KB001",
		Trips: []ktmb.CompleteTripRes{
			{Tickets: []ktmb.CompleteTicketRes{{TicketNo: "TK001"}}},
		},
	}}
	bookingNo, ticketNo, err := ticketIdsOf(wellFormed)
	if err != nil || bookingNo != "KB001" || ticketNo != "TK001" {
		t.Errorf("well-formed: got (%q, %q, %v), want (KB001, TK001, nil)", bookingNo, ticketNo, err)
	}

	// a successful Complete with no trips must error (the caller parks with a
	// PurchasedError), preserving the booking number for recovery
	noTrips := ktmb.CompleteRes{Booking: ktmb.CompleteBookingRes{BookingNo: "KB002"}}
	bookingNo, ticketNo, err = ticketIdsOf(noTrips)
	if err == nil || bookingNo != "KB002" || ticketNo != "" {
		t.Errorf("no trips: got (%q, %q, %v), want (KB002, \"\", error)", bookingNo, ticketNo, err)
	}

	// trips present but no tickets: same contract
	noTickets := ktmb.CompleteRes{Booking: ktmb.CompleteBookingRes{
		BookingNo: "KB003",
		Trips:     []ktmb.CompleteTripRes{{}},
	}}
	bookingNo, ticketNo, err = ticketIdsOf(noTickets)
	if err == nil || bookingNo != "KB003" || ticketNo != "" {
		t.Errorf("no tickets: got (%q, %q, %v), want (KB003, \"\", error)", bookingNo, ticketNo, err)
	}

	// fully blank response still errors without panicking
	if _, _, err = ticketIdsOf(ktmb.CompleteRes{}); err == nil {
		t.Error("blank response: expected error, got nil")
	}
}
