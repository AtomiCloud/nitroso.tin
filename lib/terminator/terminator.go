package terminator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	"github.com/rs/zerolog"
)

type refundClient interface {
	ListTicket(userData string, page int64) (ktmb.GenericRes[ktmb.TicketListRes], error)
	GetRefundPolicy(userData, bookingData, ticketData string) (ktmb.GenericRes[ktmb.RefundPolicyRes], error)
	RefundTicket(userData, password, bookingData, ticketData string) (ktmb.GenericRes[*interface{}], error)
}

type sessionProvider interface {
	Login(ctx context.Context, email, password string) (string, error)
}

type refundReporter interface {
	PostApiVVersionBookingIdKtmbRefund(ctx context.Context, version string, id openapi_types.UUID, body zinc.PostApiVVersionBookingIdKtmbRefundJSONBody, reqEditors ...zinc.RequestEditorFn) (*http.Response, error)
}

type Terminator struct {
	ktmb         refundClient
	session      sessionProvider
	zinc         refundReporter
	logger       *zerolog.Logger
	enrichConfig config.EnricherConfig
}

func NewTerminator(ktmb refundClient, s sessionProvider, zinc refundReporter, logger *zerolog.Logger, enrichConfig config.EnricherConfig) Terminator {
	return Terminator{
		ktmb:         ktmb,
		logger:       logger,
		session:      s,
		zinc:         zinc,
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
			for _, trip := range booking.Trips {
				for _, ticket := range trip.Tickets {
					if ticket.TicketNo == ticketNo {
						return booking.BookingData, ticket.TicketData, nil
					}
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

func (t *Terminator) Terminate(ctx context.Context, termination BookingTermination) error {

	t.logger.Info().Msg("Logging in")
	cred, err := t.session.Login(ctx, t.enrichConfig.Email, t.enrichConfig.Password)
	if err != nil {
		t.logger.Error().Err(err).Msg("Failed to login")
		return err
	}
	t.logger.Info().Msg("Logging succeeded")

	t.logger.Info().Msg("Getting ticket information")
	bookingData, ticketData, err := t.find(cred, termination.BookingNo, termination.TicketNo, 1)
	if err != nil {
		t.logger.Error().Err(err).Msg("Failed to get ticket information")
		return err
	}

	t.logger.Info().Msg("Obtain Refund Policy")
	refundPolicy, err := t.ktmb.GetRefundPolicy(cred, bookingData, ticketData)
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

	policyTicket, err := refundTicket(refundPolicy.Data, termination.TicketNo)
	if err != nil {
		t.logger.Error().Err(err).Msg("Refund policy did not contain the requested ticket")
		return err
	}
	r, err := t.ktmb.RefundTicket(cred, t.enrichConfig.Password, refundPolicy.Data.BookingData, policyTicket.TicketData)
	if err != nil {
		t.logger.Error().Err(err).Msg("Failed to refund tickets")
		return err
	}

	if !r.Status {
		e := fmt.Errorf("failed to refund ticket: %+v", r.Messages)
		t.logger.Error().Err(e).Strs("errors", r.Messages).Msg("Failed to refund ticket")
		return e
	}

	amount, currency, ok := refundAmount(refundPolicy.Data, termination.TicketNo)
	if !ok {
		t.logger.Error().
			Str("bookingId", termination.Id.String()).
			Str("bookingNo", termination.BookingNo).
			Str("ticketNo", termination.TicketNo).
			Msg("KTMB refund succeeded but refund policy had no usable amount; leaving zinc refund missing for backfill")
		return nil
	}
	if err := t.reportRefund(ctx, termination.Id, amount, currency); err != nil {
		t.logger.Error().Err(err).
			Str("bookingId", termination.Id.String()).
			Str("bookingNo", termination.BookingNo).
			Str("ticketNo", termination.TicketNo).
			Msg("KTMB refund succeeded but reporting the refund to zinc failed; continuing")
	}
	return nil
}

func refundAmount(policy ktmb.RefundPolicyRes, ticketNo string) (float32, string, bool) {
	var matched *ktmb.RefundPolicyTripTicketRes
	ticketCount := 0
	for _, trip := range policy.Trips {
		for i := range trip.Tickets {
			ticketCount++
			ticket := &trip.Tickets[i]
			if ticket.TicketNo == ticketNo {
				matched = ticket
			}
		}
	}
	if matched != nil && matched.RefundAmount > 0 {
		return matched.RefundAmount, normalizeCurrency(matched.CurrencyCode, policy.CurrencyCode), true
	}
	// TotalRefundAmount is an aggregate across the policy response. It is exact
	// for this booking only when KTMB omitted ticket details, or when the target
	// is the policy's sole ticket; using it for one ticket in a multi-ticket
	// policy would overstate that booking's refund.
	if policy.TotalRefundAmount > 0 && (ticketCount == 0 || (ticketCount == 1 && matched != nil)) {
		return policy.TotalRefundAmount, normalizeCurrency(policy.CurrencyCode), true
	}
	return 0, "", false
}

func refundTicket(policy ktmb.RefundPolicyRes, ticketNo string) (ktmb.RefundPolicyTripTicketRes, error) {
	var sole *ktmb.RefundPolicyTripTicketRes
	ticketCount := 0
	for _, trip := range policy.Trips {
		for i := range trip.Tickets {
			ticketCount++
			ticket := &trip.Tickets[i]
			if ticket.TicketNo == ticketNo {
				if ticket.TicketData == "" {
					return ktmb.RefundPolicyTripTicketRes{}, fmt.Errorf("refund policy ticket %q has no ticket data", ticketNo)
				}
				return *ticket, nil
			}
			sole = ticket
		}
	}
	// Some KTMB responses omit TicketNo after the caller already selected one.
	// A sole anonymous ticket is still unambiguous; an explicitly different or
	// multi-ticket response is not.
	if ticketCount == 1 && sole != nil && sole.TicketNo == "" && sole.TicketData != "" {
		return *sole, nil
	}
	return ktmb.RefundPolicyTripTicketRes{}, fmt.Errorf("refund policy does not contain ticket %q", ticketNo)
}

func normalizeCurrency(currencies ...string) string {
	for _, currency := range currencies {
		if value := strings.ToUpper(strings.TrimSpace(currency)); value != "" {
			return value
		}
	}
	return "MYR"
}

func (t *Terminator) reportRefund(ctx context.Context, id openapi_types.UUID, amount float32, currency string) error {
	if t.zinc == nil {
		return fmt.Errorf("zinc refund reporter is not configured")
	}
	resp, err := t.zinc.PostApiVVersionBookingIdKtmbRefund(ctx, "1.0", id, zinc.PostApiVVersionBookingIdKtmbRefundJSONBody{
		RefundAmount:   amount,
		RefundCurrency: currency,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
