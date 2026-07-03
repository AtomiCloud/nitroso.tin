package buyer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/reserver"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"io"
	"math"
	"mime/multipart"
	"time"
)

type Client struct {
	buyer            *Buyer
	mainRedis        *otelredis.OtelRedis
	streamRedis      *otelredis.OtelRedis
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	streamsCfg       config.StreamConfig
	buyerCfg         config.BuyerConfig
	recovererCfg     config.RecovererConfig
	psm              string
	zinc             *zinc.Client
	encr             encryptor.Encryptor[reserver.ReserveDto]
	recoverEncr      encryptor.Encryptor[lib.RecoverDto]
}

var baseDelay = 1 * time.Second

func CreateForm(values map[string]io.Reader) (s string, reader io.Reader, err error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for key, r := range values {
		var fw io.Writer
		if fw, err = w.CreateFormFile(key, key); err != nil {
			return
		}
		if _, err = io.Copy(fw, r); err != nil {
			return
		}

	}
	w.Close()

	return w.FormDataContentType(), &b, nil
}

func New(buyer *Buyer, mainRedis, streamRedis *otelredis.OtelRedis, otelConfigurator *telemetry.OtelConfigurator, logger *zerolog.Logger,
	streamsCfg config.StreamConfig, buyerCfg config.BuyerConfig, recovererCfg config.RecovererConfig, psm string, zinc *zinc.Client,
	enrc encryptor.Encryptor[reserver.ReserveDto], recoverEncr encryptor.Encryptor[lib.RecoverDto]) *Client {
	return &Client{
		buyer:            buyer,
		mainRedis:        mainRedis,
		streamRedis:      streamRedis,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		streamsCfg:       streamsCfg,
		buyerCfg:         buyerCfg,
		recovererCfg:     recovererCfg,
		psm:              psm,
		zinc:             zinc,
		encr:             enrc,
		recoverEncr:      recoverEncr,
	}
}

func (c *Client) Start(ctx context.Context) error {
	maxCounter := c.buyerCfg.BackoffLimit

	errorCounter := 0

	for {
		shouldExit, err := c.loop(ctx)
		if err != nil {
			if errorCounter >= maxCounter {
				c.logger.Error().Err(err).Msg("Failed all backoff attempts, exiting...")
				return err
			}
			secRetry := math.Pow(2, float64(errorCounter))
			c.logger.Info().Msgf("Retrying operation in %f seconds", secRetry)
			delay := time.Duration(secRetry) * baseDelay
			time.Sleep(delay)
			errorCounter++
		} else {
			errorCounter = 0
		}
		if shouldExit {
			break
		}
	}
	return nil
}

func (c *Client) loop(ctx context.Context) (bool, error) {
	shutdown, err := c.otelConfigurator.Configure(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to configure telemetry")
		return true, err
	}
	defer func() {
		deferErr := shutdown(ctx)
		if deferErr != nil {
			panic(deferErr)
		}
	}()

	tracer := otel.Tracer(c.psm)

	c.logger.Info().Ctx(ctx).Str("queue", c.streamsCfg.Reserver).Msg("Buyer waiting for reserver message...")
	err = c.streamRedis.QueuePop(ctx, tracer, c.streamsCfg.Reserver, func(ctx context.Context, message json.RawMessage) error {
		c.logger.Info().Ctx(ctx).Msg("Buyer received reserver emitted signal")
		var output string
		e := json.Unmarshal(message, &output)
		if e != nil {
			c.logger.Error().Err(e).Msg("Failed to unmarshal reserver emitted signal")
			return e
		}
		reserveDto, e := c.encr.DecryptAny(output)
		if e != nil {
			c.logger.Error().Err(e).Msg("Failed to decrypt reserver emitted signal")
			return e
		}
		c.logger.Info().Any("reserveDto", reserveDto).Ctx(ctx).Msg("Reserver emitted signal decrypted")
		er := c.buy(ctx, reserveDto.Direction, reserveDto.Date, reserveDto.Time, reserveDto.UserData, reserveDto.BookingData)
		if er != nil {
			c.logger.Error().Err(er).Str("date", reserveDto.Date).Str("time", reserveDto.Time).Str("dir", reserveDto.Direction).Msg("Failed to buy")
			return er
		}
		return nil
	})
	if err != nil {
		c.logger.Error().
			Err(err).
			Msg("Failed to read from redis list in buyer (from reserver)")
		return false, err
	} else {
		c.logger.Info().Msg("Buyer queue pop loop ended without failure")
	}
	return false, nil
}

func (c *Client) buy(ctx context.Context, direction, date, t, userData, bookingData string) error {

	resp, err := c.zinc.GetApiVVersionBookingReserveDirectionDateTime(ctx, "1.0", direction, date, t)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get reserved datetime")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		c.logger.Info().Msg("No booking found, releasing reservation...")
		_, e := c.buyer.Release(userData, bookingData)
		if e != nil {
			c.logger.Error().Err(e).Msg("Failed to release")
			return e
		}
		return nil
	}

	content, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		c.logger.Error().Err(readErr).Msg("Failed to read response from http response from booking reserve get endpoint")
		return readErr
	}
	var data zinc.BookingPrincipalRes
	er := json.Unmarshal(content, &data)
	if er != nil {
		c.logger.Error().Err(er).
			Str("content", string(content)).
			Msg("Failed to decode response from CDC endpoint")
		return er
	}

	reserved, err := c.zinc.PostApiVVersionBookingBuyingId(ctx, "1.0", data.Id)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get buying id")
		return err
	}
	defer reserved.Body.Close()

	if reserved.StatusCode != 200 {
		rErr := fmt.Errorf("failed to get mark booking as buying")
		c.logger.Error().Err(rErr).Msg("failed to get mark booking as buying")
		return rErr
	}

	_, readErr = io.ReadAll(reserved.Body)
	if readErr != nil {
		c.logger.Error().Err(readErr).Msg("Failed to read response from http response from booking reserve get endpoint")
		return readErr
	}

	p := Passenger{
		FullName:       *data.Passenger.FullName,
		Gender:         *data.Passenger.Gender,
		PassportExpiry: fmt.Sprintf("%sT00:00:00", lib.ZincToHeliumDate(*data.Passenger.PassportExpiry)),
		PassportNumber: *data.Passenger.PassportNumber,
	}

	c.logger.Info().Any("passenger", p).Msg("Passenger")
	buy, bookingNo, ticketNo, err := c.buyer.Buy(userData, bookingData, p, direction, date, t)
	if err != nil {
		var conflictErr *ConflictError
		var purchasedErr *PurchasedError
		switch {
		case errors.As(err, &conflictErr):
			// this passenger already holds a ticket for this slot — park the
			// booking for the recoverer and free the KTMB reservation
			c.logger.Warn().Ctx(ctx).Err(err).Str("date", date).Str("time", t).Str("dir", direction).Msg("Duplicate-passenger conflict, parking booking for recovery")
			return c.park(ctx, data.Id, direction, date, t, p, "", "", userData, bookingData, true)
		case errors.As(err, &purchasedErr):
			// the KTMB purchase went through — never release; park with the ticket
			// identifiers so the recoverer can force-complete deterministically
			c.logger.Warn().Ctx(ctx).Err(err).Str("bookingNo", purchasedErr.BookingNo).Str("ticketNo", purchasedErr.TicketNo).Msg("Purchase captured but ticket retrieval failed, parking booking for recovery")
			return c.park(ctx, data.Id, direction, date, t, p, purchasedErr.BookingNo, purchasedErr.TicketNo, userData, bookingData, false)
		default:
			c.logger.Error().Ctx(ctx).Err(err).Str("date", date).Str("time", t).Str("dir", direction).Msg("failed to buy")
			return err
		}
	}

	err = c.completeWithZinc(ctx, data.Id, bookingNo, ticketNo, buy)
	if err != nil {
		// the ticket is bought and paid for; losing this item would strand it —
		// park with the ticket identifiers instead of failing the queue handler
		c.logger.Error().Ctx(ctx).Err(err).Str("bookingNo", bookingNo).Str("ticketNo", ticketNo).Msg("Failed to report completed ticket to zinc, parking booking for recovery")
		return c.park(ctx, data.Id, direction, date, t, p, bookingNo, ticketNo, userData, bookingData, false)
	}

	return nil
}

// completeWithZinc reports a captured ticket to zinc, retrying with backoff:
// the KTMB money is already spent, so this must not give up on a blip
func (c *Client) completeWithZinc(ctx context.Context, id openapi_types.UUID, bookingNo, ticketNo string, pdf []byte) error {
	retries := c.buyerCfg.CompleteRetries
	if retries < 1 {
		retries = 1
	}

	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt-1))) * baseDelay
			c.logger.Info().Ctx(ctx).Int("attempt", attempt).Dur("delay", delay).Msg("Retrying zinc complete...")
			time.Sleep(delay)
		}

		lastErr = c.completeOnce(ctx, id, bookingNo, ticketNo, pdf)
		if lastErr == nil {
			return nil
		}
		c.logger.Error().Ctx(ctx).Err(lastErr).Int("attempt", attempt).Msg("Failed to mark booking as complete")
	}
	return lastErr
}

func (c *Client) completeOnce(ctx context.Context, id openapi_types.UUID, bookingNo, ticketNo string, pdf []byte) error {
	contentType, rr, err := CreateForm(map[string]io.Reader{
		"file": bytes.NewReader(pdf),
	})
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to create form")
		return err
	}
	completed, err := c.zinc.PostApiVVersionBookingCompleteIdWithBody(ctx, "1.0", id, &zinc.PostApiVVersionBookingCompleteIdParams{
		BookingNo: &bookingNo,
		TicketNo:  &ticketNo,
	}, contentType, rr)
	if err != nil {
		return err
	}
	defer completed.Body.Close()

	if completed.StatusCode != 200 {
		body, _ := io.ReadAll(completed.Body)
		return fmt.Errorf("failed to mark booking as complete: status %d: %s", completed.StatusCode, string(body))
	}
	return nil
}

// park records a booking for recovery: it pushes an encrypted RecoverDto onto
// the recover queue FIRST (the durable record — when a purchase succeeded, the
// ticket identifiers live only here), then does a best-effort transition to
// Recovering (the recoverer drives the transition itself if this fails), then
// optionally releases the KTMB reservation (never when the purchase succeeded).
func (c *Client) park(ctx context.Context, id openapi_types.UUID, direction, date, t string, p Passenger,
	bookingNo, ticketNo, userData, bookingData string, release bool) error {

	dto := lib.RecoverDto{
		BookingId:      id.String(),
		Direction:      direction,
		Date:           date,
		Time:           t,
		FullName:       p.FullName,
		Gender:         p.Gender,
		PassportExpiry: p.PassportExpiry,
		PassportNumber: p.PassportNumber,
		BookingNo:      bookingNo,
		TicketNo:       ticketNo,
		Attempts:       0,
	}
	enc, err := c.recoverEncr.EncryptAny(dto)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to encrypt recover dto")
		return err
	}

	tracer := otel.Tracer(c.psm)
	if err = c.pushRecover(ctx, tracer, enc); err != nil {
		// nothing more we can do — the money record could not be durably stored
		c.logger.Error().Ctx(ctx).Err(err).Str("bookingId", id.String()).Str("bookingNo", bookingNo).Str("ticketNo", ticketNo).Str("queue", c.recovererCfg.QueueName).Msg("Failed to push recover dto after retries")
		return err
	}
	c.logger.Info().Ctx(ctx).Str("bookingId", id.String()).Str("queue", c.recovererCfg.QueueName).Msg("Booking recovery record queued")

	// transition to Recovering with retry so the booking leaves Buying promptly
	// (a booking left in Buying is only recovered via the queue item or the
	// manual `recover` command, since Buying cannot be safely auto-swept — it is
	// indistinguishable from an in-flight purchase). If this ultimately fails,
	// the recoverer drives Buying -> Recovering itself when it drains the queued
	// record.
	if err = c.transitionRecovering(ctx, id); err != nil {
		c.logger.Warn().Ctx(ctx).Err(err).Str("bookingId", id.String()).Msg("Failed to transition booking to recovering after retries (recoverer will drive it from the queue)")
	}

	if release {
		_, e := c.buyer.Release(userData, bookingData)
		if e != nil {
			// best effort: the reservation expires on its own; recovery is queued
			c.logger.Error().Ctx(ctx).Err(e).Msg("Failed to release KTMB reservation after parking")
		}
	}
	return nil
}

// transitionRecovering moves the booking Buying -> Recovering with a short
// retry so it does not linger in the un-sweepable Buying state.
func (c *Client) transitionRecovering(ctx context.Context, id openapi_types.UUID) error {
	retries := c.buyerCfg.CompleteRetries
	if retries < 1 {
		retries = 1
	}
	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(math.Pow(2, float64(attempt-1))) * baseDelay)
		}
		resp, err := c.zinc.PostApiVVersionBookingRecoveringId(ctx, "1.0", id)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return nil
		}
		lastErr = fmt.Errorf("transition to recovering: status %d: %s", resp.StatusCode, string(body))
	}
	return lastErr
}

// pushRecover pushes the encrypted recover record with a short retry, since it
// is the sole durable store of a captured ticket's identifiers
func (c *Client) pushRecover(ctx context.Context, tracer trace.Tracer, enc string) error {
	retries := c.buyerCfg.CompleteRetries
	if retries < 1 {
		retries = 1
	}
	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(math.Pow(2, float64(attempt-1))) * baseDelay)
		}
		_, lastErr = c.mainRedis.QueuePush(ctx, tracer, c.recovererCfg.QueueName, enc)
		if lastErr == nil {
			return nil
		}
		c.logger.Error().Ctx(ctx).Err(lastErr).Int("attempt", attempt).Str("queue", c.recovererCfg.QueueName).Msg("Failed to push recover dto")
	}
	return lastErr
}
