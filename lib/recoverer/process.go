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

	userData, err := c.session.Login(ctx, c.enricher.Email, c.enricher.Password)
	if err != nil {
		l.Error().Err(err).Msg("Failed to login to KTMB")
		return err
	}

	found, err := c.findTicket(userData, target, dto.Direction, dto.PassportNumber)
	if err != nil {
		// an inconclusive scan (empty/blank list, mutated pagination, unparseable
		// row) must never fall through to a refund OR a re-buy probe
		l.Error().Err(err).Msg("KTMB ticket scan inconclusive, will retry")
		return err
	}

	if found != nil {
		claimed, err := c.isClaimed(ctx, dto, found.BookingNo)
		if err != nil {
			return err
		}
		if claimed {
			// the ticket on our account already belongs to another zinc booking
			// (user double-booked) — this booking is a true duplicate
			l.Warn().Str("ktmbBookingNo", found.BookingNo).Msg("Ticket already claimed by another completed booking, marking duplicate")
			return c.markDuplicate(ctx, dto.BookingId)
		}
		// our uncaptured ticket — capture it
		l.Info().Str("ktmbBookingNo", found.BookingNo).Str("ktmbTicketNo", found.TicketNo).Msg("Found uncaptured ticket on our KTMB account, force completing")
		return c.forceComplete(ctx, dto.BookingId, found.BookingNo, found.TicketNo)
	}

	// not on our account: either the user bought via another channel, or the
	// conflict was transient. A re-buy attempt distinguishes them — KTMB
	// rejects with the same duplicate error if the passenger is booked anywhere
	l.Info().Msg("Ticket not on our KTMB account, probing with a re-buy")
	return c.rebuy(ctx, dto, userData, target, l)
}

func (c *Client) rebuy(ctx context.Context, dto lib.RecoverDto, userData string, target time.Time, l zerolog.Logger) error {
	store, err := c.retriever.GetLoginData(ctx)
	if err != nil {
		return err
	}
	if store == nil {
		return fmt.Errorf("no login/find store available for re-buy")
	}

	find := store.Find[dto.Direction][dto.Date][dto.Time]
	if find.TripData == "" {
		return fmt.Errorf("no trip data available for %s %s %s", dto.Direction, dto.Date, dto.Time)
	}

	reserve, err := c.ktmb.Reserve(store.UserData, c.appInfo, find.SearchData, find.TripData)
	if err != nil {
		return err
	}
	if !reserve.Status {
		return fmt.Errorf("failed to reserve for re-buy: %+v", reserve.Messages)
	}
	bookingData := reserve.Data.BookingData

	p := buyer.Passenger{
		FullName:       dto.FullName,
		Gender:         dto.Gender,
		PassportExpiry: dto.PassportExpiry,
		PassportNumber: dto.PassportNumber,
	}

	pdf, bookingNo, ticketNo, err := c.buyer.Buy(store.UserData, bookingData, p, dto.Direction, dto.Date, dto.Time)
	if err != nil {
		var conflictErr *buyer.ConflictError
		var purchasedErr *buyer.PurchasedError
		switch {
		case errors.As(err, &conflictErr):
			// the probe confirms the passenger is booked somewhere. Release the
			// probe reservation, then re-scan to decide WHOSE ticket it is —
			// never refund on a contradictory/inconclusive scan.
			if _, e := c.buyer.Release(store.UserData, bookingData); e != nil {
				l.Error().Err(e).Msg("Failed to release probe reservation")
			}
			return c.resolveConflict(ctx, dto, func() (*foundTicket, error) {
				// KTMB just told us the passenger is booked, so an empty own-account
				// list is contradictory; findTicket treats it (and every other
				// inconclusive condition) as an error that must not drive a refund
				return c.findTicket(userData, target, dto.Direction, dto.PassportNumber)
			}, l)
		case errors.As(err, &purchasedErr):
			// bought but ticket retrieval failed — requeue deterministically
			l.Warn().Err(err).Msg("Re-buy purchased but ticket retrieval failed, requeueing with ticket identifiers")
			dto.BookingNo = purchasedErr.BookingNo
			dto.TicketNo = purchasedErr.TicketNo
			c.requeue(ctx, dto)
			return nil
		default:
			if _, e := c.buyer.Release(store.UserData, bookingData); e != nil {
				l.Error().Err(e).Msg("Failed to release probe reservation")
			}
			return err
		}
	}

	// Buy succeeded — real money spent. Stash the identifiers so recovery is
	// deterministic even if the complete call fails now (never re-buy).
	dto.BookingNo = bookingNo
	dto.TicketNo = ticketNo
	l.Info().Str("bookingNo", bookingNo).Str("ticketNo", ticketNo).Msg("Re-buy succeeded, completing booking")
	if err := c.completeBooking(ctx, dto.BookingId, bookingNo, ticketNo, pdf); err != nil {
		l.Error().Err(err).Msg("Re-buy succeeded but complete failed, requeueing with ticket identifiers")
		c.requeue(ctx, dto)
		return nil
	}
	return nil
}

// resolveConflict decides a booking whose re-buy probe was rejected as a
// duplicate: re-scan our KTMB account to find WHOSE ticket blocks it. It
// applies the same gates as the main scan's found-path (§5.6) — in particular
// the claim gate: a found ticket already recorded on another Completed zinc
// booking marks THIS booking Duplicate (refund), because force-completing it
// would collect this booking's reserve for a ticket someone else owns. Only a
// conclusive "not on our account" re-scan may refund; an inconclusive re-scan
// (or claim check) retries.
func (c *Client) resolveConflict(ctx context.Context, dto lib.RecoverDto, rescan func() (*foundTicket, error), l zerolog.Logger) error {
	found, err := rescan()
	if err != nil {
		l.Error().Err(err).Msg("Re-buy conflicted but re-scan inconclusive, will retry")
		return err
	}
	if found == nil {
		l.Warn().Msg("Re-buy confirmed conflict and ticket is not on our account, marking duplicate")
		return c.markDuplicate(ctx, dto.BookingId)
	}
	claimed, err := c.isClaimed(ctx, dto, found.BookingNo)
	if err != nil {
		// retry with the ORIGINAL dto (no stashed identifiers): stashing before
		// the claim gate passes would route the next cycle onto the deterministic
		// force-complete path, which skips this gate entirely
		l.Error().Err(err).Str("ktmbBookingNo", found.BookingNo).Msg("Re-scan found a ticket but the claim check failed, will retry")
		return err
	}
	if claimed {
		// the ticket on our account already belongs to another zinc booking
		// (user double-booked) — this booking is a true duplicate
		l.Warn().Str("ktmbBookingNo", found.BookingNo).Msg("Re-scan ticket already claimed by another completed booking, marking duplicate")
		return c.markDuplicate(ctx, dto.BookingId)
	}
	// ours and unclaimed: stash identifiers so a force-complete failure requeues
	// onto the deterministic path next cycle instead of re-probing
	dto.BookingNo = found.BookingNo
	dto.TicketNo = found.TicketNo
	l.Info().Str("ktmbBookingNo", found.BookingNo).Msg("Re-scan located our uncaptured ticket, force completing")
	if e := c.forceComplete(ctx, dto.BookingId, found.BookingNo, found.TicketNo); e != nil {
		l.Error().Err(e).Msg("Force complete failed after re-scan, requeueing with ticket identifiers")
		c.requeue(ctx, dto)
		return nil
	}
	return nil
}

// forceComplete downloads the ticket PDF from KTMB and reports it to zinc
func (c *Client) forceComplete(ctx context.Context, bookingId, bookingNo, ticketNo string) error {
	userData, err := c.session.Login(ctx, c.enricher.Email, c.enricher.Password)
	if err != nil {
		return err
	}
	pdf, err := c.ktmb.PrintTicket(userData, bookingNo, ticketNo)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("bookingNo", bookingNo).Msg("Failed to print ticket for force complete")
		return err
	}
	return c.completeBooking(ctx, bookingId, bookingNo, ticketNo, pdf)
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

// isClaimed reports whether the KTMB booking number is already recorded on a
// completed zinc booking for the same passenger and slot
func (c *Client) isClaimed(ctx context.Context, dto lib.RecoverDto, ktmbBookingNo string) (bool, error) {
	completed := "Completed"
	// explicit Limit so a zinc default page size can't hide the claiming booking:
	// a miss here returns false and force-completes a ticket owned by another
	// Completed booking, collecting this booking's reserve for someone else's
	// ticket (a §3.1 double-collect). The exact passport+slot+direction tuple
	// yields ≤1 in practice, so a large cap suffices.
	limit := int32(100)
	resp, err := c.zinc.GetApiVVersionBooking(ctx, "1.0", &zinc.GetApiVVersionBookingParams{
		PassportNumber: &dto.PassportNumber,
		Date:           &dto.Date,
		Time:           &dto.Time,
		Direction:      &dto.Direction,
		Status:         &completed,
		Limit:          &limit,
	})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("failed to search completed bookings: status %d: %s", resp.StatusCode, string(body))
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	var bookings []zinc.BookingPrincipalRes
	if err := json.Unmarshal(content, &bookings); err != nil {
		return false, err
	}
	for _, b := range bookings {
		if b.BookingNo != nil && *b.BookingNo == ktmbBookingNo {
			return true, nil
		}
	}
	return false, nil
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
