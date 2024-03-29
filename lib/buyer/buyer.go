package buyer

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/rs/zerolog"
	"time"
)

type Buyer struct {
	ktmb          ktmb.Ktmb
	contactNumber string
	logger        *zerolog.Logger
	sleepBuffer   int
}

func NewBuyer(k ktmb.Ktmb, logger *zerolog.Logger, contactNumber string, sleepBuffer int) Buyer {
	return Buyer{
		ktmb:          k,
		logger:        logger,
		contactNumber: contactNumber,
		sleepBuffer:   sleepBuffer,
	}
}

func (c *Buyer) Buy(userData, bookingData string, p Passenger) ([]byte, string, string, error) {

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
		c.logger.Error().Err(err).Msg("Failed to set passenger")
		return nil, "", "", err
	}

	if !passenger.Status {
		e := fmt.Errorf("failed to set passenger: %+v", passenger.Messages)
		c.logger.Error().Err(e).Strs("errors", passenger.Messages).Msg("Failed to set passenger")
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

	ticket, err := c.ktmb.PrintTicket(complete.Data.UserData, complete.Data.Booking.BookingNo, complete.Data.Booking.Trips[0].Tickets[0].TicketNo)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to print ticket")
		return nil, "", "", err
	}

	c.logger.Info().Any("passenger", p).Msg("Successfully purchased Ticket")
	return ticket, complete.Data.Booking.BookingNo, complete.Data.Booking.Trips[0].Tickets[0].TicketNo, nil

}

func (c *Buyer) Release(userData, bookingData string) (ktmb.GenericRes[*interface{}], error) {
	return c.ktmb.Cancel(userData, bookingData)
}
