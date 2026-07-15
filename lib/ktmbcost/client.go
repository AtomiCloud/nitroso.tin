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
var errRefundAmountUnavailable = errors.New("KTMB exposes no exact refunded amount")

const (
	StatusCompleted  = 2
	StatusTerminated = 5
)

type zincClient interface {
	GetApiVVersionBookingKtmbCostMissing(ctx context.Context, version string, params *zinc.GetApiVVersionBookingKtmbCostMissingParams, reqEditors ...zinc.RequestEditorFn) (*http.Response, error)
	PostApiVVersionBookingIdKtmbCost(ctx context.Context, version string, id openapi_types.UUID, body zinc.PostApiVVersionBookingIdKtmbCostJSONBody, reqEditors ...zinc.RequestEditorFn) (*http.Response, error)
	GetApiVVersionBookingKtmbRefundMissing(ctx context.Context, version string, params *zinc.GetApiVVersionBookingKtmbRefundMissingParams, reqEditors ...zinc.RequestEditorFn) (*http.Response, error)
	PostApiVVersionBookingIdKtmbRefund(ctx context.Context, version string, id openapi_types.UUID, body zinc.PostApiVVersionBookingIdKtmbRefundJSONBody, reqEditors ...zinc.RequestEditorFn) (*http.Response, error)
}

type ticketClient interface {
	GetTicket(userData, bookingNo, ticketNo string) (ktmb.GenericRes[ktmb.GetTicketRes], error)
	GetTicketRaw(userData, bookingNo, ticketNo string) (ktmb.GenericRes[json.RawMessage], error)
	GetRefundPolicy(userData, bookingData, ticketData string) (ktmb.GenericRes[ktmb.RefundPolicyRes], error)
}

type sessionProvider interface {
	Login(ctx context.Context, email, password string) (string, error)
	Invalidate(ctx context.Context, userData string) error
}

type Options struct {
	DryRun      bool
	Refund      bool
	Status      int
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
	zinc      zincClient
	ktmb      ticketClient
	session   sessionProvider
	logger    *zerolog.Logger
	email     string
	password  string
	options   Options
	sleep     func(context.Context, time.Duration) error
	rawDumped bool
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
	if c.options.Status == 0 {
		c.options.Status = StatusCompleted
	}
	if c.options.Status != StatusCompleted && c.options.Status != StatusTerminated {
		return summary, fmt.Errorf("status must be %d (completed) or %d (terminated)", StatusCompleted, StatusTerminated)
	}

	work, err := c.listMissing(ctx)
	if err != nil {
		return summary, err
	}
	summary.Fetched = len(work)
	if len(work) == 0 {
		if c.options.Refund {
			c.logger.Info().Msg("No terminated bookings are missing KTMB refund capture")
		} else {
			c.logger.Info().Int("status", c.options.Status).Msg("No bookings are missing KTMB actual cost")
		}
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
				Bool("refund", c.options.Refund).
				Msg("Failed to backfill KTMB financial data; continuing")
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
	if c.options.Refund {
		return c.listMissingRefunds(ctx)
	}
	work := make([]zinc.BookingKtmbCostMissingRes, 0, min(c.options.Max, c.options.PageSize))
	seen := make(map[openapi_types.UUID]struct{})
	scanned := 0
	for scanned < c.options.Max {
		limit := min(c.options.PageSize, c.options.Max-scanned)
		limit32 := int32(limit)
		skip32 := int32(scanned)
		resp, err := c.zinc.GetApiVVersionBookingKtmbCostMissing(ctx, "1.0", &zinc.GetApiVVersionBookingKtmbCostMissingParams{
			Limit:  &limit32,
			Skip:   &skip32,
			Status: &c.options.Status,
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

func (c *Client) listMissingRefunds(ctx context.Context) ([]zinc.BookingKtmbCostMissingRes, error) {
	limit := int32(c.options.Max)
	resp, err := c.zinc.GetApiVVersionBookingKtmbRefundMissing(ctx, "1.0", &zinc.GetApiVVersionBookingKtmbRefundMissingParams{Limit: &limit})
	if err != nil {
		return nil, fmt.Errorf("list bookings missing KTMB refund: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read missing KTMB refund response: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list bookings missing KTMB refund: status %d: %s", resp.StatusCode, string(body))
	}
	var work []zinc.BookingKtmbCostMissingRes
	if err := json.Unmarshal(body, &work); err != nil {
		return nil, fmt.Errorf("decode missing KTMB refund response: %w", err)
	}
	if len(work) > c.options.Max {
		work = work[:c.options.Max]
	}
	return work, nil
}

func (c *Client) process(ctx context.Context, userData string, item zinc.BookingKtmbCostMissingRes) error {
	if c.options.Refund {
		return c.processRefund(ctx, userData, item)
	}
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
	if booking.TotalAmount <= 0 {
		return fmt.Errorf("KTMB ticket response for booking %q has no usable purchase amount (totalAmount=%v)", item.BookingNo, booking.TotalAmount)
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

func (c *Client) processRefund(ctx context.Context, userData string, item zinc.BookingKtmbCostMissingRes) error {
	rawTicket, err := c.ktmb.GetTicketRaw(userData, item.BookingNo, item.TicketNo)
	if err != nil {
		return fmt.Errorf("get raw KTMB ticket: %w", err)
	}
	if !rawTicket.Status {
		if isInvalidSession(rawTicket.Messages) {
			return fmt.Errorf("%w: %s", errInvalidSession, strings.Join(rawTicket.Messages, ", "))
		}
		return fmt.Errorf("get raw KTMB ticket: %s", strings.Join(rawTicket.Messages, ", "))
	}
	if c.options.DryRun && !c.rawDumped {
		encoded, marshalErr := json.Marshal(rawTicket)
		if marshalErr == nil {
			c.logger.Debug().RawJSON("ktmbResponse", encoded).
				Str("bookingId", item.Id.String()).
				Str("bookingNo", item.BookingNo).
				Msg("Raw KTMB GetTicket response for refund probe")
		}
		c.rawDumped = true
	}

	amount, currency, rawFound := refundFromRaw(rawTicket.Data, item.TicketNo)
	var typed ktmb.GetTicketRes
	if err := json.Unmarshal(rawTicket.Data, &typed); err != nil {
		return fmt.Errorf("decode raw KTMB ticket data: %w", err)
	}
	booking, bookingErr := bookingFrom(typed, item.BookingNo)
	if bookingErr != nil {
		if !rawFound {
			return fmt.Errorf("%w; cannot probe refund policy: %v", errRefundAmountUnavailable, bookingErr)
		}
		c.logger.Warn().Err(bookingErr).Str("bookingNo", item.BookingNo).Msg("Could not probe KTMB refund policy; using exact refund field from GetTicket")
	} else {
		ticketData, ticketErr := ticketDataFrom(booking, item.TicketNo)
		if ticketErr != nil {
			if !rawFound {
				return fmt.Errorf("%w; cannot probe refund policy: %v", errRefundAmountUnavailable, ticketErr)
			}
			c.logger.Warn().Err(ticketErr).Str("bookingNo", item.BookingNo).Msg("Could not probe KTMB refund policy; using exact refund field from GetTicket")
		} else {
			policy, policyErr := c.ktmb.GetRefundPolicy(userData, booking.BookingData, ticketData)
			if policyErr != nil {
				c.logger.Warn().Err(policyErr).Str("bookingNo", item.BookingNo).Msg("KTMB refund-policy probe failed")
			} else if !policy.Status {
				if isInvalidSession(policy.Messages) {
					return fmt.Errorf("%w: %s", errInvalidSession, strings.Join(policy.Messages, ", "))
				}
				c.logger.Debug().Strs("messages", policy.Messages).Str("bookingNo", item.BookingNo).Msg("KTMB refund-policy probe rejected the already-refunded ticket")
			} else if policyAmount, policyCurrency, ok := refundFromPolicy(policy.Data, item.TicketNo); ok {
				amount, currency, rawFound = policyAmount, policyCurrency, true
			}
		}
	}
	if !rawFound {
		return fmt.Errorf("%w for booking %q ticket %q; leaving zinc record missing", errRefundAmountUnavailable, item.BookingNo, item.TicketNo)
	}
	currency = normalizeCurrency(currency, booking.CurrencyCode)
	c.logger.Info().
		Bool("dryRun", c.options.DryRun).
		Str("bookingId", item.Id.String()).
		Str("bookingNo", item.BookingNo).
		Str("ticketNo", item.TicketNo).
		Float32("refundAmount", amount).
		Str("refundCurrency", currency).
		Msg("Resolved exact KTMB refund")
	if c.options.DryRun {
		return nil
	}
	resp, err := c.zinc.PostApiVVersionBookingIdKtmbRefund(ctx, "1.0", item.Id, zinc.PostApiVVersionBookingIdKtmbRefundJSONBody{
		RefundAmount:   amount,
		RefundCurrency: currency,
	})
	if err != nil {
		return fmt.Errorf("post KTMB refund: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("post KTMB refund: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func ticketDataFrom(booking ktmb.GetTicketBookingRes, ticketNo string) (string, error) {
	for _, trip := range booking.Trips {
		for _, ticket := range trip.Tickets {
			if ticket.TicketNo == ticketNo && ticket.TicketData != "" {
				return ticket.TicketData, nil
			}
		}
	}
	return "", fmt.Errorf("KTMB ticket response does not contain ticket data for %q", ticketNo)
}

func refundFromPolicy(policy ktmb.RefundPolicyRes, ticketNo string) (float32, string, bool) {
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
	if policy.TotalRefundAmount > 0 && (ticketCount == 0 || (ticketCount == 1 && matched != nil)) {
		return policy.TotalRefundAmount, normalizeCurrency(policy.CurrencyCode), true
	}
	return 0, "", false
}

func refundFromRaw(raw json.RawMessage, ticketNo string) (float32, string, bool) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return 0, "", false
	}
	if amount, currency, ok := refundInTicketNode(root, ticketNo); ok {
		return amount, currency, true
	}
	ticketCount, targetPresent := ticketNodeStats(root, ticketNo)
	if ticketCount > 1 || (ticketCount > 0 && !targetPresent) {
		return 0, "", false
	}
	return exactRefundInNode(root)
}

func ticketNodeStats(node any, ticketNo string) (int, bool) {
	count := 0
	found := false
	switch value := node.(type) {
	case map[string]any:
		if _, ok := value["ticketNo"]; ok {
			count++
			found = stringField(value, "ticketNo") == ticketNo
		}
		for _, child := range value {
			childCount, childFound := ticketNodeStats(child, ticketNo)
			count += childCount
			found = found || childFound
		}
	case []any:
		for _, child := range value {
			childCount, childFound := ticketNodeStats(child, ticketNo)
			count += childCount
			found = found || childFound
		}
	}
	return count, found
}

func refundInTicketNode(node any, ticketNo string) (float32, string, bool) {
	switch value := node.(type) {
	case map[string]any:
		if stringField(value, "ticketNo") == ticketNo {
			if amount, currency, ok := exactRefundInMap(value); ok {
				return amount, currency, true
			}
		}
		for _, child := range value {
			if amount, currency, ok := refundInTicketNode(child, ticketNo); ok {
				return amount, currency, true
			}
		}
	case []any:
		for _, child := range value {
			if amount, currency, ok := refundInTicketNode(child, ticketNo); ok {
				return amount, currency, true
			}
		}
	}
	return 0, "", false
}

func exactRefundInNode(node any) (float32, string, bool) {
	switch value := node.(type) {
	case map[string]any:
		if amount, currency, ok := exactRefundInMap(value); ok {
			return amount, currency, true
		}
		for _, child := range value {
			if amount, currency, ok := exactRefundInNode(child); ok {
				return amount, currency, true
			}
		}
	case []any:
		for _, child := range value {
			if amount, currency, ok := exactRefundInNode(child); ok {
				return amount, currency, true
			}
		}
	}
	return 0, "", false
}

func exactRefundInMap(value map[string]any) (float32, string, bool) {
	for _, key := range []string{"refundAmount", "refundedAmount", "totalRefundAmount"} {
		if amount, ok := numberField(value, key); ok && amount > 0 {
			return amount, normalizeCurrency(stringField(value, "refundCurrency"), stringField(value, "currencyCode")), true
		}
	}
	return 0, "", false
}

func numberField(value map[string]any, key string) (float32, bool) {
	raw, ok := value[key]
	if !ok {
		return 0, false
	}
	switch number := raw.(type) {
	case json.Number:
		parsed, err := number.Float64()
		return float32(parsed), err == nil
	case float64:
		return float32(number), true
	}
	return 0, false
}

func stringField(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
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

func normalizeCurrency(currencies ...string) string {
	for _, currency := range currencies {
		currency = strings.ToUpper(strings.TrimSpace(currency))
		if currency != "" {
			return currency
		}
	}
	return defaultCurrency
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
