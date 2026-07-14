package ktmbcost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	"github.com/rs/zerolog"
)

const defaultCurrency = "MYR"

var errInvalidSession = errors.New("KTMB session is invalid")

type zincClient interface {
	GetApiVVersionBookingKtmbCostMissing(ctx context.Context, version string, params *zinc.GetApiVVersionBookingKtmbCostMissingParams, reqEditors ...zinc.RequestEditorFn) (*http.Response, error)
	PostApiVVersionBookingIdKtmbCost(ctx context.Context, version string, id openapi_types.UUID, body zinc.PostApiVVersionBookingIdKtmbCostJSONBody, reqEditors ...zinc.RequestEditorFn) (*http.Response, error)
}

type ticketClient interface {
	GetTicket(userData, bookingNo, ticketNo string) (ktmb.GenericRes[ktmb.GetTicketRes], error)
}

type sessionProvider interface {
	Login(ctx context.Context, email, password string) (string, error)
	Invalidate(ctx context.Context, userData string) error
}

type Options struct {
	DryRun      bool
	Max         int
	PageSize    int
	SleepBuffer time.Duration
}

type Summary struct {
	Fetched   int
	Attempted int
	Updated   int
	Failed    int
	DryRun    int
}

type Client struct {
	zinc     zincClient
	ktmb     ticketClient
	session  sessionProvider
	logger   *zerolog.Logger
	email    string
	password string
	options  Options
	sleep    func(context.Context, time.Duration) error
}

func New(zincClient zincClient, ktmbClient ticketClient, session sessionProvider, logger *zerolog.Logger, email, password string, options Options) *Client {
	return &Client{
		zinc:     zincClient,
		ktmb:     ktmbClient,
		session:  session,
		logger:   logger,
		email:    email,
		password: password,
		options:  options,
		sleep:    sleepContext,
	}
}

func (c *Client) Run(ctx context.Context) (Summary, error) {
	var summary Summary
	if c.options.Max <= 0 {
		return summary, fmt.Errorf("max must be greater than zero")
	}
	if c.options.PageSize <= 0 {
		return summary, fmt.Errorf("page size must be greater than zero")
	}

	work, err := c.listMissing(ctx)
	if err != nil {
		return summary, err
	}
	summary.Fetched = len(work)
	if len(work) == 0 {
		c.logger.Info().Msg("No completed bookings are missing KTMB actual cost")
		return summary, nil
	}

	userData, err := c.session.Login(ctx, c.email, c.password)
	if err != nil {
		return summary, fmt.Errorf("obtain KTMB session: %w", err)
	}

	for i, item := range work {
		if i > 0 && c.options.SleepBuffer > 0 {
			if err := c.sleep(ctx, c.options.SleepBuffer); err != nil {
				return summary, err
			}
		}
		summary.Attempted++
		if err := c.processWithSessionRetry(ctx, &userData, item); err != nil {
			summary.Failed++
			c.logger.Error().Err(err).
				Str("bookingId", item.Id.String()).
				Str("bookingNo", item.BookingNo).
				Str("ticketNo", item.TicketNo).
				Msg("Failed to backfill KTMB actual cost; continuing")
			continue
		}
		if c.options.DryRun {
			summary.DryRun++
		} else {
			summary.Updated++
		}
	}
	return summary, nil
}

func (c *Client) processWithSessionRetry(ctx context.Context, userData *string, item zinc.BookingKtmbCostMissingRes) error {
	err := c.process(ctx, *userData, item)
	if !errors.Is(err, errInvalidSession) {
		return err
	}

	c.logger.Warn().
		Str("bookingId", item.Id.String()).
		Str("bookingNo", item.BookingNo).
		Msg("KTMB rejected the cached session; invalidating it and retrying once")
	if invalidateErr := c.session.Invalidate(ctx, *userData); invalidateErr != nil {
		return fmt.Errorf("%w; invalidate cached session: %v", err, invalidateErr)
	}
	refreshed, loginErr := c.session.Login(ctx, c.email, c.password)
	if loginErr != nil {
		return fmt.Errorf("%w; refresh session: %v", err, loginErr)
	}
	*userData = refreshed
	return c.process(ctx, refreshed, item)
}

func (c *Client) listMissing(ctx context.Context) ([]zinc.BookingKtmbCostMissingRes, error) {
	work := make([]zinc.BookingKtmbCostMissingRes, 0, min(c.options.Max, c.options.PageSize))
	seen := make(map[openapi_types.UUID]struct{})
	scanned := 0
	for scanned < c.options.Max {
		limit := min(c.options.PageSize, c.options.Max-scanned)
		limit32 := int32(limit)
		skip32 := int32(scanned)
		resp, err := c.zinc.GetApiVVersionBookingKtmbCostMissing(ctx, "1.0", &zinc.GetApiVVersionBookingKtmbCostMissingParams{
			Limit: &limit32,
			Skip:  &skip32,
		})
		if err != nil {
			return nil, fmt.Errorf("list bookings missing KTMB cost: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read missing KTMB cost response: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list bookings missing KTMB cost: status %d: %s", resp.StatusCode, string(body))
		}
		var page []zinc.BookingKtmbCostMissingRes
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode missing KTMB cost response: %w", err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > limit {
			page = page[:limit]
		}
		scanned += len(page)
		for _, item := range page {
			if _, duplicate := seen[item.Id]; duplicate {
				c.logger.Debug().Str("bookingId", item.Id.String()).Msg("Skipping duplicate KTMB cost work item")
				continue
			}
			seen[item.Id] = struct{}{}
			work = append(work, item)
		}
		if len(page) < limit {
			break
		}
	}
	return work, nil
}

func (c *Client) process(ctx context.Context, userData string, item zinc.BookingKtmbCostMissingRes) error {
	ticket, err := c.ktmb.GetTicket(userData, item.BookingNo, item.TicketNo)
	if err != nil {
		return fmt.Errorf("get KTMB ticket: %w", err)
	}
	if !ticket.Status {
		if isInvalidSession(ticket.Messages) {
			return fmt.Errorf("%w: %s", errInvalidSession, strings.Join(ticket.Messages, ", "))
		}
		return fmt.Errorf("get KTMB ticket: %s", strings.Join(ticket.Messages, ", "))
	}
	booking, err := bookingFrom(ticket.Data, item.BookingNo)
	if err != nil {
		return err
	}
	currency := normalizeCurrency(booking.CurrencyCode)

	c.logger.Info().
		Bool("dryRun", c.options.DryRun).
		Str("bookingId", item.Id.String()).
		Str("bookingNo", item.BookingNo).
		Str("ticketNo", item.TicketNo).
		Float32("ktmbAmount", booking.TotalAmount).
		Str("ktmbCurrency", currency).
		Str("completedAt", item.CompletedAt).
		Msg("Resolved KTMB actual cost")
	if c.options.DryRun {
		return nil
	}

	resp, err := c.zinc.PostApiVVersionBookingIdKtmbCost(ctx, "1.0", item.Id, zinc.PostApiVVersionBookingIdKtmbCostJSONBody{
		Amount:   booking.TotalAmount,
		Currency: currency,
	})
	if err != nil {
		return fmt.Errorf("post KTMB actual cost: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("post KTMB actual cost: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func isInvalidSession(messages []string) bool {
	for _, message := range messages {
		if strings.Contains(strings.ToLower(message), "please login to continue") {
			return true
		}
	}
	return false
}

func bookingFrom(ticket ktmb.GetTicketRes, bookingNo string) (ktmb.GetTicketBookingRes, error) {
	for _, booking := range ticket.Bookings {
		if booking.BookingNo == bookingNo {
			return booking, nil
		}
	}
	return ktmb.GetTicketBookingRes{}, fmt.Errorf("KTMB ticket response does not contain booking %q", bookingNo)
}

func normalizeCurrency(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return defaultCurrency
	}
	return currency
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
