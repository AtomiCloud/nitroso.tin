package recoverer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
)

// Admin "check our KTMB account": zinc queues {checkId, bookingId} on the
// stream-cache list recoverer.checkQueueName when an admin presses the button;
// the recoverer answers each request with the SAME scan + claim lookup it uses
// to resolve a Recovering booking (scanOurAccount / claimant) and writes the
// answer back to zinc. Strictly READ-ONLY: a check never completes, refunds,
// recycles or marks anything — the only zinc write is the check result.

const (
	CheckFound    = "Found"
	CheckNotFound = "NotFound"
	CheckDeparted = "Departed"
	CheckError    = "Error"
)

// checkRequest is zinc's queue payload (ids only; the booking is read from zinc)
type checkRequest struct {
	CheckId   uuid.UUID `json:"checkId"`
	BookingId uuid.UUID `json:"bookingId"`
}

// CheckResult is the answer for one booking
type CheckResult struct {
	Status        string
	KtmbBookingNo string
	KtmbTicketNo  string
	// the Completed zinc booking already recording this KTMB booking number
	// (may be the checked booking itself); nil when unclaimed
	ClaimedBy *uuid.UUID
	Error     string
}

// ticketScanner is scanOurAccount's shape, injectable for tests
type ticketScanner func(ctx context.Context, target time.Time, direction, passport string) (*foundTicket, error)

// CheckBooking answers "does OUR KTMB account hold a ticket for this
// booking's passenger/date/direction?" as of now. It never returns an error:
// every failure is reported to the admin as an Error result.
func (c *Client) CheckBooking(ctx context.Context, bookingId string, now time.Time, scan ticketScanner) CheckResult {
	booking, status, err := c.getBooking(ctx, bookingId)
	if err != nil {
		return CheckResult{Status: CheckError, Error: err.Error()}
	}
	if status == http.StatusNotFound {
		return CheckResult{Status: CheckError, Error: "booking not found in zinc"}
	}

	dto := ReconstructDto(booking.Principal)
	target, err := time.ParseInLocation(zincDateTimeLayout, dto.Date+" "+dto.Time, c.loc)
	if err != nil {
		return CheckResult{Status: CheckError, Error: fmt.Sprintf("unparseable departure %q %q: %v", dto.Date, dto.Time, err)}
	}
	// the upcoming-ticket list no longer shows past departures (same rule as
	// ProcessItem, which parks those for a human)
	if !target.After(now.In(c.loc)) {
		return CheckResult{Status: CheckDeparted}
	}

	found, err := scan(ctx, target, dto.Direction, dto.PassportNumber)
	if err != nil {
		return CheckResult{Status: CheckError, Error: "KTMB scan inconclusive: " + err.Error()}
	}
	if found == nil {
		return CheckResult{Status: CheckNotFound}
	}

	res := CheckResult{Status: CheckFound, KtmbBookingNo: found.BookingNo, KtmbTicketNo: found.TicketNo}
	claim, err := c.claimant(ctx, dto, found.BookingNo)
	if err != nil {
		// the ticket IS on our account; only the claim lookup failed
		res.Error = "could not look up which booking claims this ticket: " + err.Error()
		return res
	}
	if claim != nil {
		id := claim.Id
		res.ClaimedBy = &id
	}
	return res
}

// answerCheck runs one queued check and reports it to zinc
func (c *Client) answerCheck(ctx context.Context, req checkRequest) {
	l := c.logger.With().Ctx(ctx).Str("checkId", req.CheckId.String()).Str("bookingId", req.BookingId.String()).Logger()
	res := c.CheckBooking(ctx, req.BookingId.String(), time.Now(), c.scanOurAccount)
	l.Info().Str("result", res.Status).Str("ktmbBookingNo", res.KtmbBookingNo).Msg("KTMB account check answered")

	var err error
	for attempt := 0; attempt < recoverRequeueRetries; attempt++ {
		if err = c.reportCheck(ctx, req.CheckId, res); err == nil {
			return
		}
		l.Error().Err(err).Int("attempt", attempt).Msg("Failed to report KTMB check result, retrying")
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
		}
	}
	// the admin sees the check stay Pending and can press the button again
	l.Error().Err(err).Msg("Failed to report KTMB check result after retries")
}

func (c *Client) reportCheck(ctx context.Context, checkId uuid.UUID, res CheckResult) error {
	body := zinc.PostApiVVersionBookingKtmbCheckCheckIdResultJSONBody{
		Status:             res.Status,
		KtmbBookingNo:      optional(res.KtmbBookingNo),
		KtmbTicketNo:       optional(res.KtmbTicketNo),
		ClaimedByBookingId: res.ClaimedBy,
		Error:              optional(res.Error),
	}
	resp, err := c.zinc.PostApiVVersionBookingKtmbCheckCheckIdResult(ctx, "1.0", checkId, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// the check (or its booking) is gone — nothing to report to
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to report KTMB check: status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// popChecks blocks on the check queue and hands each request to the main
// loop, so checks run one at a time between drain/sweep cycles (the recoverer
// stays sequential). Exits when ctx is cancelled.
func (c *Client) popChecks(ctx context.Context, out chan<- checkRequest) {
	tracer := otel.Tracer(c.psm)
	backoff := time.Second
	for ctx.Err() == nil {
		err := c.streamRedis.QueuePop(ctx, tracer, c.config.CheckQueueName, func(ctx context.Context, message json.RawMessage) error {
			var req checkRequest
			if e := json.Unmarshal(message, &req); e != nil || req.CheckId == uuid.Nil || req.BookingId == uuid.Nil {
				c.logger.Error().Ctx(ctx).Err(e).Str("message", string(message)).Msg("Malformed KTMB check request, dropping")
				return nil
			}
			select {
			case out <- req:
			case <-ctx.Done():
			}
			return nil
		})
		if err == nil {
			backoff = time.Second
			continue
		}
		if ctx.Err() != nil {
			return
		}
		c.logger.Error().Ctx(ctx).Err(err).Str("queue", c.config.CheckQueueName).Msg("Failed to pop KTMB check queue, backing off")
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}
