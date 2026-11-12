package recoverer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/buyer"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const zincDateTimeLayout = "02-01-2006 15:04:05"

// terminalStatuses are booking states the recoverer must never act on: either
// already resolved or already parked for a human.
var terminalStatuses = map[string]bool{
	"Completed":                 true,
	"Cancelled":                 true,
	"Refunded":                  true,
	"Duplicate":                 true,
	"Terminated":                true,
	"RequireManualIntervention": true,
}

// ProcessItem classifies and resolves one parked booking. An error return
// means "retry later" (the caller re-queues); nil means resolved or dropped.
func (c *Client) ProcessItem(ctx context.Context, dto lib.RecoverDto) error {
	l := c.logger.With().Ctx(ctx).Str("bookingId", dto.BookingId).Str("date", dto.Date).Str("time", dto.Time).Str("dir", dto.Direction).Logger()

	booking, status, err := c.getBooking(ctx, dto.BookingId)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		l.Warn().Msg("Booking no longer exists in zinc, dropping recover item")
		return nil
	}

	bookStatus := ""
	if booking.Principal.Status != nil {
		bookStatus = *booking.Principal.Status
	}

	// already resolved elsewhere, or already parked for a human — nothing to do
	if terminalStatuses[bookStatus] {
		l.Info().Str("status", bookStatus).Msg("Booking already resolved or parked, dropping recover item")
		return nil
	}

	// legacy corruption (booking rolled back to a non-completed status after its
	// reserve was already collected): the ledger already collected this booking's
	// reserve (BookingNo is only written by a committed Complete) —
	// force-completing again would double-collect. Park it.
	if booking.Principal.BookingNo != nil && *booking.Principal.BookingNo != "" {
		l.Error().Str("bookingNo", *booking.Principal.BookingNo).
			Msg("Booking has a captured ticket but non-completed status (legacy corruption), parking for manual intervention")
		return c.markManualIntervention(ctx, dto.BookingId)
	}

	// the buyer queues the recover record first and transitions best-effort, so
	// a booking may still be in Buying — drive the transition ourselves
	if bookStatus == "Buying" {
		if err := c.markRecovering(ctx, dto.BookingId); err != nil {
			l.Error().Err(err).Msg("Failed to transition booking to recovering")
			return err
		}
		bookStatus = "Recovering"
	}

	if bookStatus != "Recovering" {
		// e.g. Pending — unexpected for a queued recover item; a human decides
		l.Warn().Str("status", bookStatus).Msg("Unexpected status for recover item, parking for manual intervention")
		return c.markManualIntervention(ctx, dto.BookingId)
	}

	// deterministic path: the purchase is known to have succeeded
	if dto.BookingNo != "" && dto.TicketNo != "" {
		l.Info().Str("bookingNo", dto.BookingNo).Str("ticketNo", dto.TicketNo).Msg("Recover item carries ticket identifiers, force completing")
		return c.forceComplete(ctx, dto.BookingId, dto.BookingNo, dto.TicketNo)
	}

	// the upcoming-ticket list cannot verify past departures — a human must
	target, err := time.ParseInLocation(zincDateTimeLayout, dto.Date+" "+dto.Time, c.loc)
	if err != nil {
		l.Error().Err(err).Msg("Failed to parse recover item date/time, parking for manual intervention")
		return c.markManualIntervention(ctx, dto.BookingId)
	}
	if target.Before(time.Now().In(c.loc)) {
		l.Warn().Msg("Departure already passed, cannot verify against KTMB, parking for manual intervention")
		return c.markManualIntervention(ctx, dto.BookingId)
	}

	matches, scanErr := c.scanOurAccountAll(ctx, target, dto.Direction, dto.PassportNumber)
	if len(matches) > 0 {
		// scanErr (if any) means the list may hold more matches than we saw;
		// resolveFound only lets that pass where acting on what we saw is safe
		return c.resolveFound(ctx, l, dto, matches, scanErr)
	}
	if scanErr != nil {
		// an inconclusive scan (empty/blank list, mutated pagination, unparseable
		// row) must never fall through to a refund — only a definitive not-found does
		l.Error().Err(scanErr).Msg("KTMB ticket scan inconclusive, will retry")
		return scanErr
	}

	// not on our account: the passenger holds this seat via another channel
	// (KTMB rejected our purchase as a duplicate passport, and a definitive scan
	// shows the ticket is not ours). Only a DEFINITIVE absence reaches here: an
	// inconclusive scan (empty/mutated list, unparseable row) returned an error
	// above and is retried, never refunded, so this can never act on a booking
	// whose ticket we actually hold. Before refunding as a duplicate, ask zinc
	// to recycle the booking back to Pending so the pipeline re-buys it — the
	// "duplicate passport" rejection is often transient (e.g. the conflicting
	// KTMB booking lapses); zinc holds the retry counter, and only once its cap
	// is exhausted (409) do we refund as a true duplicate.
	l.Warn().Msg("Ticket not on our KTMB account, requesting recycle before duplicate")
	return c.recycleOrDuplicate(ctx, l, dto.BookingId, true)
}

// resolveFound decides what to do with the tickets for this passenger+slot
// found on our KTMB account (KTMB keeps cancelled bookings listed, so there can
// be more than one), based on which zinc booking — if any — records each, and
// on what KTMB says when asked to print it:
//
//   - a ticket no other booking records: print it. Live (a PDF) → it is this
//     booking's own uncaptured ticket → capture it. Confirmed cancelled →
//     treat like a released ticket. Anything else → retry.
//   - one recorded on a Completed booking → this booking is a true duplicate
//   - one recorded on a booking still in flight → wait (retry later)
//   - every ticket released (recorded on a Cancelled/Refunded/Terminated
//     booking, or unrecorded and confirmed cancelled) → once KTMB confirms
//     none is still live, recycle this booking to Pending so it is bought
//     afresh. zinc's Terminated does NOT prove the KTMB cancel landed (the
//     terminator can fail), so a still-live released ticket is left for a human
//     (retry → RequireManualIntervention), never recycled into a duplicate
//     refund and never captured (a late terminator run could still cancel it).
//
// partial (non-nil) means the scan was inconclusive somewhere, so matches may
// be incomplete: capturing a live match is still safe, but concluding "every
// ticket is released" is not.
func (c *Client) resolveFound(ctx context.Context, l zerolog.Logger, dto lib.RecoverDto, matches []foundTicket, partial error) error {
	slot, err := c.slotBookings(ctx, dto)
	if err != nil {
		return err
	}
	var unrecorded, released, live, completed []foundTicket
	var liveHolder, completedHolder *zinc.BookingPrincipalRes
	for _, m := range matches {
		holder := holderOf(slot, dto.BookingId, m.BookingNo)
		if holder == nil {
			unrecorded = append(unrecorded, m)
			continue
		}
		switch st := lib.Deref(holder.Status); {
		case st == "Completed":
			completed = append(completed, m)
			completedHolder = holder
		case releasedStatuses[st]:
			released = append(released, m)
		default:
			live = append(live, m)
			liveHolder = holder
		}
	}

	// unrecorded tickets: our own uncaptured ticket if KTMB still prints it
	deadUnrecorded := 0
	for _, m := range unrecorded {
		state, pdf, err := c.ticketState(ctx, m)
		if err != nil {
			return err
		}
		if state == ticketLive {
			l.Info().Str("ktmbBookingNo", m.BookingNo).Str("ktmbTicketNo", m.TicketNo).Msg("Found uncaptured ticket on our KTMB account, force completing")
			return c.completeBooking(ctx, dto.BookingId, m.BookingNo, m.TicketNo, pdf)
		}
		l.Warn().Str("ktmbBookingNo", m.BookingNo).Msg("Unrecorded ticket on our KTMB account is cancelled, ignoring it")
		deadUnrecorded++
	}
	if len(completed) > 0 {
		// the ticket on our account already belongs to another zinc booking
		// (user double-booked) — this booking is a true duplicate
		l.Warn().Str("ktmbBookingNo", completed[0].BookingNo).Str("holderId", completedHolder.Id.String()).
			Msg("Ticket already claimed by another completed booking, marking duplicate")
		return c.markDuplicate(ctx, dto.BookingId)
	}
	if len(live) > 0 {
		// recorded on a booking that is still in flight — never capture a
		// ticket another live booking may own; retry once it settles
		return fmt.Errorf("ticket %s is recorded on booking %s in status %q, waiting for it to settle",
			live[0].BookingNo, liveHolder.Id.String(), lib.Deref(liveHolder.Status))
	}
	if partial != nil {
		return fmt.Errorf("only cancelled/released tickets seen but the KTMB scan was inconclusive, will retry: %w", partial)
	}
	for _, m := range released {
		state, _, err := c.ticketState(ctx, m)
		if err != nil {
			return err
		}
		if state == ticketLive {
			l.Error().Str("ktmbBookingNo", m.BookingNo).Str("ktmbTicketNo", m.TicketNo).
				Msg("Ticket of a released booking is still live on KTMB (its cancel did not land), leaving for a human")
			return fmt.Errorf("ticket %s belongs to a released booking but is still live on KTMB; needs a human", m.BookingNo)
		}
	}
	// every ticket for this passenger+slot is cancelled on KTMB: the
	// duplicate-passport block came from those dead tickets — buy afresh. If
	// zinc refuses (budget spent / departure too close), park for a human:
	// the passenger holds no live ticket, so a Duplicate refund would be wrong.
	l.Warn().Int("releasedTickets", len(released)).Int("cancelledUnrecorded", deadUnrecorded).
		Msg("Only cancelled tickets on our account for this passenger+slot, recycling instead of capturing")
	return c.recycleOrDuplicate(ctx, l, dto.BookingId, false)
}

type ticketLiveness int

const (
	ticketLive ticketLiveness = iota
	ticketCancelled
)

// ktmbCancelledBody is what KTMB's PrintTicketPdf answers (HTTP 200, plain
// text) for a ticket that has been cancelled — observed on production
// 2026-11-12 for the incident's terminated ticket
const ktmbCancelledBody = "error when retrieving booking"

// ticketState asks KTMB to print the ticket: a PDF means it is live; KTMB's
// known "cannot retrieve" answer means it is cancelled; anything else (print
// error, empty body, unknown page) is inconclusive and returned as an error so
// the caller retries — never guessed as cancelled.
func (c *Client) ticketState(ctx context.Context, m foundTicket) (ticketLiveness, []byte, error) {
	body, err := c.print(ctx, m.BookingNo, m.TicketNo)
	if err != nil {
		return 0, nil, fmt.Errorf("could not print ticket %s to check it: %w", m.BookingNo, err)
	}
	if isPDF(body) {
		return ticketLive, body, nil
	}
	if strings.Contains(strings.ToLower(string(body)), ktmbCancelledBody) {
		return ticketCancelled, nil, nil
	}
	return 0, nil, fmt.Errorf("KTMB answered an unrecognised non-PDF for ticket %s (%d bytes: %q), will retry", m.BookingNo, len(body), preview(body))
}

// recycleOrDuplicate asks zinc to recycle a Recovering booking back to Pending
// (POST Booking/recover-revert/{id}). zinc owns the retry counter:
//   - 200 → booking is Pending again; the pipeline re-reserves it (subject to
//     the normal release cooldown). Done.
//   - 409 (recovery_retries_exhausted) → the retry budget is spent and nothing
//     changed; fall back to markDuplicate (refund, terminal) exactly as before.
//   - 400 → the booking state changed under us; drop the item — the sweep
//     re-derives from zinc if anything is still stuck.
//   - anything else (404 on an old zinc without the endpoint, 5xx, network)
//     → error, normal requeue path.
//
// refundAsDuplicate chooses the 409 fallback: true (the ticket is not on our
// account — the passenger holds it elsewhere) refunds as Duplicate; false (we
// KNOW the passenger holds no live ticket — every ticket we saw was cancelled)
// parks for a human instead, since a Duplicate refund would tell the customer
// "you already have a ticket" when they do not (zinc also answers 409 when the
// departure is too close to re-buy).
func (c *Client) recycleOrDuplicate(ctx context.Context, l zerolog.Logger, bookingId string, refundAsDuplicate bool) error {
	id, err := uuid.Parse(bookingId)
	if err != nil {
		return err
	}
	resp, err := c.zinc.PostApiVVersionBookingRecoverRevertId(ctx, "1.0", id)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		ev := l.Info()
		var b zinc.BookingPrincipalRes
		if json.Unmarshal(body, &b) == nil && b.RecoveryRetries != nil {
			ev = ev.Int32("recoveryRetries", *b.RecoveryRetries)
		}
		ev.Msg("Booking recycled Recovering -> Pending for another purchase attempt")
		return nil
	case http.StatusConflict:
		// the recycle budget is spent — this is a true duplicate now; refund.
		// Sniff the problem type purely for logging: any 409 on this endpoint
		// means the cap (zinc changed nothing, the booking is still Recovering).
		if !refundAsDuplicate {
			l.Warn().Str("problem", problemTypeOf(body)).
				Msg("Recycle refused but the passenger holds no live ticket, parking for manual intervention")
			return c.markManualIntervention(ctx, bookingId)
		}
		l.Warn().Str("problem", problemTypeOf(body)).
			Msg("Recovery retries exhausted, marking duplicate")
		return c.markDuplicate(ctx, bookingId)
	case http.StatusBadRequest:
		// state changed under us (no longer Recovering) — drop the item; the
		// sweep re-derives from zinc if the booking still needs attention
		l.Warn().Str("body", string(body)).
			Msg("Recycle rejected: booking state changed under us, dropping recover item")
		return nil
	default:
		return fmt.Errorf("failed to recycle booking: status %d: %s", resp.StatusCode, string(body))
	}
}

// problemTypeOf extracts the RFC-7807 problem id from a zinc error body,
// tolerating any malformed payload (logging-only, never load-bearing). zinc
// sets "type" to an error-portal URL ending in the problem id (e.g.
// ".../v1/recovery_retries_exhausted") — or omits it entirely when the portal
// is disabled — so take the last path segment.
func problemTypeOf(body []byte) string {
	var problem struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		return ""
	}
	if i := strings.LastIndex(problem.Type, "/"); i >= 0 {
		return problem.Type[i+1:]
	}
	return problem.Type
}

// scanOurAccount logs in to OUR KTMB account and scans its upcoming-ticket
// list for the passport at the target departure+direction — the one scan both
// the recovery path and the admin KTMB check use. (nil, nil) only on a
// definitive absence; any error is inconclusive (see findTicketIn).
func (c *Client) scanOurAccount(ctx context.Context, target time.Time, direction, passport string) (*foundTicket, error) {
	userData, err := c.session.Login(ctx, c.enricher.Email, c.enricher.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to login to KTMB: %w", err)
	}
	return c.findTicket(ctx, userData, target, direction, passport)
}

// scanOurAccountAll is scanOurAccount returning every match (findTicketsIn)
func (c *Client) scanOurAccountAll(ctx context.Context, target time.Time, direction, passport string) ([]foundTicket, error) {
	userData, err := c.session.Login(ctx, c.enricher.Email, c.enricher.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to login to KTMB: %w", err)
	}
	return c.findTickets(ctx, userData, target, direction, passport)
}

// print fetches a ticket's PDF from our KTMB account (printTicket overrides it
// in tests)
func (c *Client) print(ctx context.Context, bookingNo, ticketNo string) ([]byte, error) {
	if c.printTicket != nil {
		return c.printTicket(ctx, bookingNo, ticketNo)
	}
	userData, err := c.session.Login(ctx, c.enricher.Email, c.enricher.Password)
	if err != nil {
		return nil, err
	}
	return c.ktmb.PrintTicket(userData, bookingNo, ticketNo)
}

// forceComplete downloads the ticket PDF from KTMB and reports it to zinc
func (c *Client) forceComplete(ctx context.Context, bookingId, bookingNo, ticketNo string) error {
	pdf, err := c.print(ctx, bookingNo, ticketNo)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("bookingNo", bookingNo).Msg("Failed to print ticket for force complete")
		return err
	}
	if !isPDF(pdf) {
		// KTMB answered 2xx with something that is not a ticket (an error body,
		// e.g. for a cancelled ticket). Uploading it only earns an opaque zinc
		// 500 ("Unknown file format"); fail here with what KTMB actually said.
		err := fmt.Errorf("KTMB did not return a ticket PDF for booking %s ticket %s (%d bytes: %q)", bookingNo, ticketNo, len(pdf), preview(pdf))
		c.logger.Error().Ctx(ctx).Err(err).Str("bookingNo", bookingNo).Msg("Printed ticket is not a PDF, not completing")
		return err
	}
	return c.completeBooking(ctx, bookingId, bookingNo, ticketNo, pdf)
}

// isPDF reports whether b looks like a PDF document (magic "%PDF-", allowing
// leading whitespace or a UTF-8 BOM).
func isPDF(b []byte) bool {
	b = bytes.TrimLeft(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), " \t\r\n")
	return bytes.HasPrefix(b, []byte("%PDF-"))
}

// preview returns a short, printable prefix of a non-PDF body for logs
func preview(b []byte) string {
	const max = 160
	if len(b) > max {
		b = b[:max]
	}
	return strings.ToValidUTF8(string(b), "?")
}

func (c *Client) completeBooking(ctx context.Context, bookingId, bookingNo, ticketNo string, pdf []byte) error {
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
	resp, err := c.zinc.PostApiVVersionBookingCompleteIdWithBody(ctx, "1.0", id, &zinc.PostApiVVersionBookingCompleteIdParams{
		BookingNo: &bookingNo,
		TicketNo:  &ticketNo,
	}, contentType, rr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to complete booking: status %d: %s", resp.StatusCode, string(body))
	}
	c.logger.Info().Ctx(ctx).Str("bookingId", bookingId).Str("bookingNo", bookingNo).Msg("Booking completed in zinc")
	return nil
}

// releasedStatuses are zinc states whose KTMB ticket has been (or is being)
// given back: a ticket recorded on such a booking is not capturable by another
var releasedStatuses = map[string]bool{
	"Cancelled":  true,
	"Refunded":   true,
	"Terminated": true,
}

// ticketHolder returns the OTHER zinc booking (same passenger and slot, any
// status) that records the KTMB booking number, or nil when none does.
func (c *Client) ticketHolder(ctx context.Context, dto lib.RecoverDto, ktmbBookingNo string) (*zinc.BookingPrincipalRes, error) {
	slot, err := c.slotBookings(ctx, dto)
	if err != nil {
		return nil, err
	}
	return holderOf(slot, dto.BookingId, ktmbBookingNo), nil
}

// holderOf picks, among the slot's bookings other than self that record the
// KTMB booking number, the holder by precedence independent of order:
// Completed (owns the ticket) > still in flight (may yet own it — wait) >
// released (ticket given back). nil when none records it.
func holderOf(slot []zinc.BookingPrincipalRes, selfId, ktmbBookingNo string) *zinc.BookingPrincipalRes {
	var live, released *zinc.BookingPrincipalRes
	for i := range slot {
		b := &slot[i]
		if b.Id.String() == selfId || b.BookingNo == nil || *b.BookingNo != ktmbBookingNo {
			continue
		}
		switch st := lib.Deref(b.Status); {
		case st == "Completed":
			return b
		case releasedStatuses[st]:
			if released == nil {
				released = b
			}
		default:
			if live == nil {
				live = b
			}
		}
	}
	if live != nil {
		return live
	}
	return released
}

// claimant returns the Completed zinc booking (same passenger and slot, which
// may be the checked booking itself) that records the KTMB booking number, or
// nil when none does — the admin KTMB check's "claimed by" answer.
func (c *Client) claimant(ctx context.Context, dto lib.RecoverDto, ktmbBookingNo string) (*zinc.BookingPrincipalRes, error) {
	slot, err := c.slotBookings(ctx, dto)
	if err != nil {
		return nil, err
	}
	for i := range slot {
		if slot[i].BookingNo != nil && *slot[i].BookingNo == ktmbBookingNo && lib.Deref(slot[i].Status) == "Completed" {
			return &slot[i], nil
		}
	}
	return nil, nil
}

// slotBookings returns every zinc booking (any status, including the booking
// itself) for this passenger+slot+direction.
func (c *Client) slotBookings(ctx context.Context, dto lib.RecoverDto) ([]zinc.BookingPrincipalRes, error) {
	// explicit Limit so a zinc default page size can't hide the claiming booking:
	// a miss here force-completes a ticket owned by another Completed booking,
	// collecting this booking's reserve for someone else's ticket (a §3.1
	// double-collect). The exact passport+slot+direction tuple yields a handful
	// at most, so a large cap suffices; a full page is treated as inconclusive.
	limit := int32(100)
	resp, err := c.zinc.GetApiVVersionBooking(ctx, "1.0", &zinc.GetApiVVersionBookingParams{
		PassportNumber: &dto.PassportNumber,
		Date:           &dto.Date,
		Time:           &dto.Time,
		Direction:      &dto.Direction,
		Limit:          &limit,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to search bookings for ticket holder: status %d: %s", resp.StatusCode, string(body))
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var bookings []zinc.BookingPrincipalRes
	if err := json.Unmarshal(content, &bookings); err != nil {
		return nil, err
	}
	if len(bookings) >= int(limit) {
		// a full page could hide the holder on the next one — inconclusive
		return nil, fmt.Errorf("ticket holder search hit the %d-result cap, inconclusive", limit)
	}
	return bookings, nil
}

// listRecovering fetches every booking currently in Recovering status,
// paginating defensively (there should only ever be a handful).
func (c *Client) listRecovering(ctx context.Context) ([]zinc.BookingPrincipalRes, error) {
	recovering := "Recovering"
	limit := int32(100)
	var all []zinc.BookingPrincipalRes
	for skip := int32(0); ; skip += limit {
		s := skip
		resp, err := c.zinc.GetApiVVersionBooking(ctx, "1.0", &zinc.GetApiVVersionBookingParams{
			Status: &recovering,
			Limit:  &limit,
			Skip:   &s,
		})
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to list recovering bookings: status %d: %s", resp.StatusCode, string(content))
		}
		if readErr != nil {
			return nil, readErr
		}
		var page []zinc.BookingPrincipalRes
		if err := json.Unmarshal(content, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if int32(len(page)) < limit {
			break
		}
	}
	return all, nil
}

// ReconstructDto rebuilds a recover item from a zinc booking (the sweep path
// and the manual `recover` command). It cannot know a captured-but-unreported
// ticket's KTMB numbers — zinc has none for such bookings — so
// BookingNo/TicketNo are left empty and the scan re-derives them.
func ReconstructDto(b zinc.BookingPrincipalRes) lib.RecoverDto {
	return lib.RecoverDto{
		BookingId:      b.Id.String(),
		Direction:      lib.Deref(b.Direction),
		Date:           lib.Deref(b.Date),
		Time:           lib.Deref(b.Time),
		FullName:       lib.Deref(b.Passenger.FullName),
		Gender:         lib.Deref(b.Passenger.Gender),
		PassportExpiry: safeHeliumExpiry(lib.Deref(b.Passenger.PassportExpiry)),
		PassportNumber: lib.Deref(b.Passenger.PassportNumber),
	}
}

// safeHeliumExpiry converts a zinc yyyy-mm-dd expiry to the KTMB
// dd-mm-yyyyT00:00:00 form, tolerating an empty/malformed value.
func safeHeliumExpiry(zincExpiry string) string {
	if len(strings.Split(zincExpiry, "-")) != 3 {
		return ""
	}
	return fmt.Sprintf("%sT00:00:00", lib.ZincToHeliumDate(zincExpiry))
}

func (c *Client) getBooking(ctx context.Context, bookingId string) (*zinc.BookingRes, int, error) {
	id, err := uuid.Parse(bookingId)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.zinc.GetApiVVersionBookingId(ctx, "1.0", id, &zinc.GetApiVVersionBookingIdParams{})
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, http.StatusNotFound, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("failed to get booking: status %d: %s", resp.StatusCode, string(body))
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	var booking zinc.BookingRes
	if err := json.Unmarshal(content, &booking); err != nil {
		return nil, resp.StatusCode, err
	}
	return &booking, resp.StatusCode, nil
}

func (c *Client) markRecovering(ctx context.Context, bookingId string) error {
	return c.postStatus(ctx, bookingId, "recovering", c.zinc.PostApiVVersionBookingRecoveringId)
}

func (c *Client) markDuplicate(ctx context.Context, bookingId string) error {
	return c.postStatus(ctx, bookingId, "duplicate", c.zinc.PostApiVVersionBookingDuplicateId)
}

func (c *Client) markManualIntervention(ctx context.Context, bookingId string) error {
	return c.postStatus(ctx, bookingId, "manual-intervention", c.zinc.PostApiVVersionBookingManualInterventionId)
}

func (c *Client) postStatus(ctx context.Context, bookingId, name string,
	post func(ctx context.Context, version string, id uuid.UUID, reqEditors ...zinc.RequestEditorFn) (*http.Response, error)) error {
	id, err := uuid.Parse(bookingId)
	if err != nil {
		return err
	}
	resp, err := post(ctx, "1.0", id)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to mark booking as %s: status %d: %s", name, resp.StatusCode, string(body))
	}
	c.logger.Info().Ctx(ctx).Str("bookingId", bookingId).Str("transition", name).Msg("Booking transitioned in zinc")
	return nil
}
