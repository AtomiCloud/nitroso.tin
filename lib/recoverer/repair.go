package recoverer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/buyer"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/google/uuid"
)

// ticketPrinter downloads a ticket PDF from KTMB by its identifiers. It is a
// function seam so the repair decision logic is testable without a live KTMB
// session (mirrors the pageFetcher seam in scan.go).
type ticketPrinter func(bookingNo, ticketNo string) ([]byte, error)

// Repair is the missing-ticket repair sweep: it lists Completed bookings whose
// ticket file reference is gone (lost from object storage, or completed via a
// path that never uploaded one) and restores each by re-downloading the PDF
// from KTMB and re-attaching it via zinc's Booking/ticket endpoint.
//
// The sweep is read-mostly and idempotent: re-attaching the same ticket is
// harmless, so per-item transient failures are just logged — the next sweep
// retries naturally, no queue involvement. Only two conditions park a booking
// for a human (RequireManualIntervention): missing KTMB identifiers (zinc has
// no BookingNo/TicketNo, so nothing can be re-downloaded) and a DEFINITIVE
// KTMB "booking/ticket unknown" rejection. Departed bookings are still
// attempted — KTMB's PrintTicketPdf may well serve a past ticket — and any
// inconclusive error on them just logs and moves on.
func (c *Client) Repair(ctx context.Context) error {
	if !c.config.RepairEnable {
		c.logger.Info().Ctx(ctx).Msg("Ticket repair sweep disabled, skipping")
		return nil
	}

	bookings, err := c.listMissingTickets(ctx)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to list missing-ticket bookings for repair sweep")
		return err
	}
	if len(bookings) == 0 {
		return nil
	}
	c.logger.Info().Ctx(ctx).Int("count", len(bookings)).Msg("Repairing completed bookings with missing tickets")

	// one login for the whole sweep (session.Login serves a cached token)
	userData, err := c.session.Login(ctx, c.enricher.Email, c.enricher.Password)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to login to KTMB for repair sweep")
		return err
	}
	print := func(bookingNo, ticketNo string) ([]byte, error) {
		return c.ktmb.PrintTicket(userData, bookingNo, ticketNo)
	}

	c.repairBookings(ctx, bookings, print)
	return nil
}

// repairBookings runs the per-booking repair decision over one sweep's page.
// Per-item failures never abort the sweep.
func (c *Client) repairBookings(ctx context.Context, bookings []zinc.BookingPrincipalRes, print ticketPrinter) {
	for _, b := range bookings {
		c.repairOne(ctx, b, print)
	}
}

// repairOne restores a single Completed booking's missing ticket file:
//
//   - zinc has both KTMB identifiers → PrintTicket → upload to zinc.
//   - identifiers missing → a human must source the ticket: park it.
//   - KTMB definitively does not know the booking/ticket → park it.
//   - anything inconclusive (network, 5xx, session hiccup, empty PDF, upload
//     failure) → log and move on; the next sweep retries.
func (c *Client) repairOne(ctx context.Context, b zinc.BookingPrincipalRes, print ticketPrinter) {
	bookingId := b.Id.String()
	bookingNo := lib.Deref(b.BookingNo)
	ticketNo := lib.Deref(b.TicketNo)
	l := c.logger.With().Ctx(ctx).Str("bookingId", bookingId).Str("bookingNo", bookingNo).Str("ticketNo", ticketNo).Logger()

	// defense-in-depth: the list query already filters Status=Completed, but
	// both actions below (ticket upload, and especially the terminal
	// manual-intervention park) are consequential — never trust a filter
	// regression on the zinc side (mirrors ProcessItem's terminalStatuses
	// re-check)
	if lib.Deref(b.Status) != "Completed" {
		l.Warn().Str("status", lib.Deref(b.Status)).Msg("Missing-ticket listing returned a non-Completed booking, skipping")
		return
	}

	if bookingNo == "" || ticketNo == "" {
		// nothing to re-download with — only a human can source this ticket
		l.Error().Msg("Completed booking has no ticket file and no KTMB identifiers, parking for manual intervention")
		if err := c.markManualIntervention(ctx, bookingId); err != nil {
			l.Error().Err(err).Msg("Failed to park identifier-less booking for manual intervention")
		}
		return
	}

	pdf, err := print(bookingNo, ticketNo)
	if err != nil {
		if matchesNotFound(c.config.RepairNotFoundPatterns, err) {
			// KTMB definitively does not know this booking/ticket — re-download
			// can never succeed; a human must source the ticket
			l.Error().Err(err).Msg("KTMB does not know this booking/ticket, parking for manual intervention")
			if miErr := c.markManualIntervention(ctx, bookingId); miErr != nil {
				l.Error().Err(miErr).Msg("Failed to park unknown-ticket booking for manual intervention")
			}
			return
		}
		// inconclusive (network, 5xx, session expiry, departed-ticket oddity):
		// never park on ambiguity — the next sweep retries for free
		l.Warn().Err(err).Msg("KTMB ticket re-download failed inconclusively, will retry next sweep")
		return
	}
	if len(pdf) == 0 {
		// an empty body with a 2xx is a scrape anomaly, not a verdict — retry
		l.Warn().Msg("KTMB returned an empty ticket PDF, will retry next sweep")
		return
	}

	if err := c.uploadTicket(ctx, bookingId, bookingNo, ticketNo, pdf); err != nil {
		l.Warn().Err(err).Msg("Failed to upload repaired ticket to zinc, will retry next sweep")
		return
	}
	l.Info().Int("bytes", len(pdf)).Msg("Repaired missing ticket file on completed booking")
}

// listMissingTickets fetches one page (RepairLimit) of Completed bookings that
// carry no ticket file reference. One page per sweep is deliberate: the
// backlog should be near-zero in steady state, and a bounded page keeps the
// sweep's KTMB traffic modest; leftovers are picked up next sweep.
func (c *Client) listMissingTickets(ctx context.Context) ([]zinc.BookingPrincipalRes, error) {
	completed := "Completed"
	missing := true
	limit := int32(c.config.RepairLimit)
	resp, err := c.zinc.GetApiVVersionBooking(ctx, "1.0", &zinc.GetApiVVersionBookingParams{
		Status:        &completed,
		MissingTicket: &missing,
		Limit:         &limit,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list missing-ticket bookings: status %d: %s", resp.StatusCode, string(content))
	}
	var bookings []zinc.BookingPrincipalRes
	if err := json.Unmarshal(content, &bookings); err != nil {
		return nil, err
	}
	return bookings, nil
}

// uploadTicket attaches the re-downloaded PDF to the Completed booking via
// zinc's ticket-repair endpoint (multipart, like completeBooking).
func (c *Client) uploadTicket(ctx context.Context, bookingId, bookingNo, ticketNo string, pdf []byte) error {
	id, err := uuid.Parse(bookingId)
	if err != nil {
		return err
	}
	contentType, rr, err := buyer.CreateForm(map[string]io.Reader{
		"file": bytes.NewReader(pdf),
	})
	if err != nil {
		return err
	}
	resp, err := c.zinc.PostApiVVersionBookingTicketIdWithBody(ctx, "1.0", id, &zinc.PostApiVVersionBookingTicketIdParams{
		BookingNo: &bookingNo,
		TicketNo:  &ticketNo,
	}, contentType, rr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to attach ticket to booking: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// matchesNotFound reports whether a KTMB PrintTicket error DEFINITIVELY means
// the booking/ticket is unknown to KTMB. Two gates must BOTH pass:
//
//  1. the error is a ktmb.HttpStatusError with a semantic client-rejection
//     status (400/404/410) — a 5xx, gateway page, WAF block, auth failure
//     (401/403) or plain transport error is never definitive, no matter what
//     its body says (an upstream outage page routinely contains "not found");
//  2. the KTMB response body matches a configured pattern, case-insensitive
//     substring (same style as the buyer's conflictPatterns).
//
// Anything else — including an empty pattern list — reads as inconclusive, so
// misconfiguration degrades to "retry forever", never to a wrongful
// manual-intervention park.
func matchesNotFound(patterns []string, err error) bool {
	var httpErr *ktmb.HttpStatusError
	if !errors.As(err, &httpErr) {
		return false
	}
	switch httpErr.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusGone:
	default:
		return false
	}
	body := strings.ToLower(httpErr.Body)
	for _, p := range patterns {
		if p != "" && strings.Contains(body, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
