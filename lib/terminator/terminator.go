package terminator

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
)

type Terminator struct {
	ktmb         ktmb.Ktmb
	logger       *zerolog.Logger
	enrichConfig config.EnricherConfig
}

func NewTerminator(ktmb ktmb.Ktmb, logger *zerolog.Logger, enrichConfig config.EnricherConfig) Terminator {
	return Terminator{
		ktmb:         ktmb,
		logger:       logger,
		enrichConfig: enrichConfig,
	}
}

func (t *Terminator) find(userData, bookingNo, ticketNo string, page int64) (string, string, error) {
	t.logger.Info().Int64("page", page).Msg("Listing tickets")
	first, err := t.ktmb.ListTicket(userData, page)
	if err != nil {
		t.logger.Error().Err(err).Msg("Failed to list tickets")
		return "", "", err
	}
	if !first.Status {
		e := fmt.Errorf("failed to list tickets: %+v", first.Messages)
		t.logger.Error().Err(e).Strs("errors", first.Messages).Msg("Failed to list tickets")
		return "", "", e
	}

	for _, booking := range first.Data.Bookings {
		if booking.BookingNo == bookingNo {
			for _, ticket := range booking.Trips[0].Tickets {
				if ticket.TicketNo == ticketNo {
					return booking.BookingData, ticket.TicketData, nil
				}
			}
		}
	}

	if first.Data.TotalPage > page {
		return t.find(userData, bookingNo, ticketNo, page+1)
	}

	t.logger.Error().Msg("Ticket not found")
	return "", "", fmt.Errorf("ticket not found")

}

func (t *Terminator) Terminate(termination BookingTermination) error {

	t.logger.Info().Msg("Logging in")
	cred, err := t.ktmb.Login(t.enrichConfig.Email, t.enrichConfig.Password)
	if err != nil {
		t.logger.Error().Err(err).Msg("Failed to login")
		return err
	}
	t.logger.Info().Msg("Logging succeeded")

	t.logger.Info().Msg("Getting ticket information")
	bookingData, ticketData, err := t.find(cred.Data.UserData, termination.BookingNo, termination.TicketNo, 1)
	if err != nil {
		t.logger.Error().Err(err).Msg("Failed to get ticket information")
		return err
	}

	t.logger.Info().Msg("Obtain Refund Policy")
	refundPolicy, err := t.ktmb.GetRefundPolicy(cred.Data.UserData, bookingData, ticketData)
	if err != nil {
		t.logger.Error().Err(err).Msg("Failed to get refund policy")
		return err
	}
	if !refundPolicy.Status {
		e := fmt.Errorf("failed to get refund policy: %+v", refundPolicy.Messages)
		t.logger.Error().Err(e).Strs("errors", refundPolicy.Messages).Msg("Failed to get refund policy")
		return e
	}
	t.logger.Info().Msg("Refunding Tickets")

	r, err := t.ktmb.RefundTicket(cred.Data.UserData, t.enrichConfig.Password, refundPolicy.Data.BookingData, refundPolicy.Data.Trips[0].Tickets[0].TicketData)
	if err != nil {
		t.logger.Error().Err(err).Msg("Failed to refund tickets")
		return err
	}

	if !r.Status {
		e := fmt.Errorf("failed to refund ticket: %+v", r.Messages)
		t.logger.Error().Err(e).Strs("errors", r.Messages).Msg("Failed to refund ticket")
		return e
	}
	return nil
}
