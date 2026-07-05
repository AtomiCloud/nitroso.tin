package buyer

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/rs/zerolog"
	"time"
)

type Buyer struct {
	ktmb             ktmb.Ktmb
	contactNumber    string
	logger           *zerolog.Logger
	sleepBuffer      int
	conflictPatterns []string
	revertPatterns   []string
}

func NewBuyer(k ktmb.Ktmb, logger *zerolog.Logger, contactNumber string, sleepBuffer int, conflictPatterns, revertPatterns []string) Buyer {
	return Buyer{
		ktmb:             k,
		logger:           logger,
		contactNumber:    contactNumber,
		sleepBuffer:      sleepBuffer,
		conflictPatterns: conflictPatterns,
		revertPatterns:   revertPatterns,
	}
}

func (c *Buyer) Buy(userData, bookingData string, p Passenger, direction, date, t string) ([]byte, string, string, error) {

	sd := time.Duration(c.sleepBuffer) * time.Second
	c.logger.Info().Msg("Initialize booking...")
	start, err := c.ktmb.BookStart(userData, bookingData)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to start booking process")
		return nil, "", "", err
	}

	if !start.Status {
		e := fmt.Errorf("failed to start booking process: %+v", start.Messages)
		c.logger.Error().Err(e).Strs("errors", start.Messages).Msg("Failed to start booking process")
		return nil, "", "", e
	}

	c.logger.Info().Int("sleepDuration", c.sleepBuffer).Msg("Booking Complete. Sleeping to prevent overloading...")
	time.Sleep(sd)
	c.logger.Info().Int("sleepDuration", c.sleepBuffer).Msg("Sleep Complete. Setting Passenger...")

	if len(start.Data.Passengers) == 0 {
		// pre-payment: no money moved yet, so a plain error (retry) is safe
		e := fmt.Errorf("book start response carries no passengers: %+v", start.Messages)
		c.logger.Error().Err(e).Msg("Malformed book start response")
		return nil, "", "", e
	}
	bd1 := start.Data.BookingData
	ud1 := start.Data.UserData
	pd := start.Data.Passengers[0].PassengerData

	passenger, err := c.ktmb.SetPassenger(ud1, bd1, []ktmb.PassengerReq{
		{
			ContactNo:          c.contactNumber,
			FullName:           p.FullName,
			Gender:             p.Gender,
			IsAddFavorite:      false,
			IsBuyInsurance:     false,
			IsSelf:             false,
			PassengerData:      pd,
			PassportExpiryDate: p.PassportExpiry,
			PassportNo:         p.PassportNumber,
			PnrData:            "",
			PnrNo:              "",
			TicketTypeID:       "Adult",
			Tickets: []ktmb.TicketReq{
				{
					PromoCode: "",
				},
			},
		},
	})
	if err != nil {
		c.logger.Error().Err(err).Str("date", date).Str("time", t).Str("dir", direction).Msg("Failed to set passenger")
		return nil, "", "", err
	}

	if !passenger.Status {
		if matchesConflict(c.conflictPatterns, passenger.Messages) {
			e := &ConflictError{Messages: passenger.Messages}
			c.logger.Error().Err(e).Strs("errors", passenger.Messages).Str("date", date).Str("time", t).Str("dir", direction).Msg("Passenger conflict detected (duplicate passport)")
			return nil, "", "", e
		}
		e := fmt.Errorf("failed to set passenger: %+v", passenger.Messages)
		c.logger.Error().Err(e).Strs("errors", passenger.Messages).Str("date", date).Str("time", t).Str("dir", direction).Msg("Failed to set passenger")
		return nil, "", "", e
	}

	c.logger.Info().Int("sleepDuration", c.sleepBuffer).Msg("Passenger Set. Sleeping to prevent overloading...")
	time.Sleep(sd)
	c.logger.Info().Int("sleepDuration", c.sleepBuffer).Msg("Sleep Complete. Performing Payment...")

	bd2 := passenger.Data.BookingData
	ud2 := passenger.Data.UserData

	pay, err := c.ktmb.Pay(ud2, bd2, "KtmbEWallet", passenger.Data.PaymentAmount)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to pay")
		return nil, "", "", err
	}
	if !pay.Status {
		if matchesConflict(c.revertPatterns, pay.Messages) {
			e := &RevertError{Messages: pay.Messages}
			c.logger.Warn().Err(e).Strs("errors", pay.Messages).Str("date", date).Str("time", t).Str("dir", direction).Msg("Transient pay failure with no ticket bought (revertable)")
			return nil, "", "", e
		}
		e := fmt.Errorf("failed to pay: %+v", pay.Messages)
		c.logger.Error().Err(e).Strs("errors", pay.Messages).Msg("Failed to pay")
		return nil, "", "", e
	}

	c.logger.Info().Int("sleepDuration", c.sleepBuffer).Msg("Payment Complete. Sleeping to prevent overloading...")
	time.Sleep(sd)
	c.logger.Info().Int("sleepDuration", c.sleepBuffer).Msg("Sleep Complete. Completing Purchase Flow...")

	bd3 := pay.Data.BookingData

	complete, err := c.ktmb.Complete(ud2, bd3)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to complete")
		return nil, "", "", err
	}
	if !complete.Status {
		e := fmt.Errorf("failed to complete: %+v", complete.Messages)
		c.logger.Error().Err(e).Strs("errors", complete.Messages).Msg("Failed to complete")
		return nil, "", "", e
	}

	c.logger.Info().Int("sleepDuration", c.sleepBuffer).Msg("Purchase flow completed. Sleeping to prevent overloading...")
	time.Sleep(sd)
	c.logger.Info().Int("sleepDuration", c.sleepBuffer).Msg("Sleep Complete. Printing Ticket...")

	bookingNo, ticketNo, idErr := ticketIdsOf(complete.Data)
	if idErr != nil {
		// Complete reported success, so the payment IS captured — a malformed
		// response shape must park the booking (with whatever identifiers we
		// have), never panic or read as a failed buy: the popped queue item is
		// the only other record and it is already gone
		e := &PurchasedError{BookingNo: bookingNo, TicketNo: ticketNo, Cause: idErr}
		c.logger.Error().Err(idErr).Str("bookingNo", bookingNo).Msg("Purchase succeeded but complete response is malformed, parking for recovery")
		return nil, bookingNo, ticketNo, e
	}

	ticket, err := c.ktmb.PrintTicket(complete.Data.UserData, bookingNo, ticketNo)
	if err != nil {
		// the purchase already succeeded — surface the ticket identifiers so the
		// caller can recover instead of treating this as a failed buy
		e := &PurchasedError{BookingNo: bookingNo, TicketNo: ticketNo, Cause: err}
		c.logger.Error().Err(err).Str("bookingNo", bookingNo).Str("ticketNo", ticketNo).Msg("Purchase succeeded but failed to print ticket")
		return nil, bookingNo, ticketNo, e
	}

	// log ticket identifiers, not the passenger object — the latter carries the
	// full name + passport number (PII) into the log stream
	c.logger.Info().Str("bookingNo", bookingNo).Str("ticketNo", ticketNo).Str("date", date).Str("time", t).Str("dir", direction).Msg("Successfully purchased Ticket")
	return ticket, bookingNo, ticketNo, nil

}

// ticketIdsOf extracts the booking/ticket identifiers from a successful
// Complete response without trusting its shape: by the time Complete reports
// Status=true the payment is captured, so a missing trips/tickets array must
// surface as an error the caller can park on — never an index panic, which
// would drop the sole in-process record of a paid ticket. The BookingNo is
// returned even when the ticket number is missing so recovery keeps whatever
// identifier exists.
func ticketIdsOf(complete ktmb.CompleteRes) (string, string, error) {
	bookingNo := complete.Booking.BookingNo
	if len(complete.Booking.Trips) == 0 || len(complete.Booking.Trips[0].Tickets) == 0 {
		return bookingNo, "", fmt.Errorf("complete response carries no trips/tickets (bookingNo %q)", bookingNo)
	}
	return bookingNo, complete.Booking.Trips[0].Tickets[0].TicketNo, nil
}

func (c *Buyer) Release(userData, bookingData string) (ktmb.GenericRes[*interface{}], error) {
	return c.ktmb.Cancel(userData, bookingData)
}
