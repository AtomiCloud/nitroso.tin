package recoverer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/buyer"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/reserver"
	"github.com/AtomiCloud/nitroso-tin/lib/session"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/robfig/cron"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
)

// Client drains the recover queue on a cron and resolves each parked booking:
// force-complete when the ticket is provably ours, mark duplicate when the
// passenger already holds it elsewhere, re-buy when no conflict actually
// exists, and park for a human when nothing can be decided safely.
type Client struct {
	ktmb             ktmb.Ktmb
	buyer            *buyer.Buyer
	session          *session.Session
	retriever        *reserver.Retriever
	mainRedis        *otelredis.OtelRedis
	zinc             *zinc.Client
	encr             encryptor.Encryptor[lib.RecoverDto]
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	config           config.RecovererConfig
	enricher         config.EnricherConfig
	psm              string
	appInfo          string
	loc              *time.Location
}

func New(k ktmb.Ktmb, b *buyer.Buyer, s *session.Session, retriever *reserver.Retriever, mainRedis *otelredis.OtelRedis,
	zClient *zinc.Client, encr encryptor.Encryptor[lib.RecoverDto], otelConfigurator *telemetry.OtelConfigurator,
	logger *zerolog.Logger, cfg config.RecovererConfig, enricher config.EnricherConfig, psm, appInfo string,
	loc *time.Location) *Client {
	return &Client{
		ktmb:             k,
		buyer:            b,
		session:          s,
		retriever:        retriever,
		mainRedis:        mainRedis,
		zinc:             zClient,
		encr:             encr,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		config:           cfg,
		enricher:         enricher,
		psm:              psm,
		appInfo:          appInfo,
		loc:              loc,
	}
}

func (c *Client) Start(ctx context.Context) error {
	shutdown, err := c.otelConfigurator.Configure(ctx)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to configure telemetry")
		return err
	}
	defer func() {
		deferErr := shutdown(ctx)
		if deferErr != nil {
			panic(deferErr)
		}
	}()

	ch := make(chan string, 1)

	cr := cron.New()
	err = cr.AddFunc(c.config.Cron, func() {
		select {
		case ch <- "cron":
		default:
		}
	})
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("cron", c.config.Cron).Msg("Failed to schedule recoverer cron")
		return err
	}
	cr.Start()
	defer cr.Stop()

	// drain once on startup so a redeploy doesn't wait a full period
	ch <- "startup"

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case trigger := <-ch:
			c.logger.Info().Ctx(ctx).Str("trigger", trigger).Msg("Recoverer cycle starting")
			if drainErr := c.Drain(ctx); drainErr != nil {
				c.logger.Error().Ctx(ctx).Err(drainErr).Msg("Recoverer drain failed")
			}
			// safety net: the queue is a fast-path hint (destructive RPop), so a
			// lost item would strand a booking in Recovering forever. Reconcile
			// against zinc, the durable source of truth, every cycle.
			if sweepErr := c.Sweep(ctx); sweepErr != nil {
				c.logger.Error().Ctx(ctx).Err(sweepErr).Msg("Recoverer sweep failed")
			}
			c.logger.Info().Ctx(ctx).Msg("Recoverer cycle complete")
		}
	}
}

// Drain pops every item currently on the recover queue and processes each.
// Per-item failures are re-queued (with an attempt cap), never dropped.
func (c *Client) Drain(ctx context.Context) error {
	tracer := otel.Tracer(c.psm)

	// cap at the queue length observed at the start so re-queued items are not
	// re-processed within the same drain
	length, err := c.mainRedis.LLen(ctx, c.config.QueueName).Result()
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("queue", c.config.QueueName).Msg("Failed to read recover queue length")
		return err
	}
	c.logger.Info().Ctx(ctx).Int64("length", length).Str("queue", c.config.QueueName).Msg("Draining recover queue")

	for i := int64(0); i < length; i++ {
		found, popErr := c.mainRedis.QueuePopNoWait(ctx, tracer, c.config.QueueName, func(ctx context.Context, message json.RawMessage) error {
			var enc string
			if e := json.Unmarshal(message, &enc); e != nil {
				c.logger.Error().Ctx(ctx).Err(e).Msg("Failed to unmarshal recover queue message")
				return nil // malformed: drop — the sweep re-derives from zinc
			}
			dto, e := c.encr.DecryptAny(enc)
			if e != nil {
				c.logger.Error().Ctx(ctx).Err(e).Msg("Failed to decrypt recover queue message")
				return nil // undecryptable: drop — the sweep re-derives from zinc
			}
			c.logger.Info().Ctx(ctx).Str("bookingId", dto.BookingId).Int("attempts", dto.Attempts).Msg("Processing recover item")
			pErr := c.ProcessItem(ctx, dto)
			if pErr != nil {
				c.logger.Error().Ctx(ctx).Err(pErr).Str("bookingId", dto.BookingId).Msg("Failed to process recover item, requeueing")
				c.requeue(ctx, dto)
			}
			return nil
		})
		if popErr != nil {
			c.logger.Error().Ctx(ctx).Err(popErr).Msg("Failed to pop recover queue")
			return popErr
		}
		if !found {
			break
		}
	}
	return nil
}

// Sweep reconciles against zinc: every booking still in Recovering is
// re-derived and re-processed, so a booking whose queue item was lost (crash
// mid-drain, requeue push failure, malformed message) is never stranded.
// zinc — not the queue — is the durable source of truth for booking state.
func (c *Client) Sweep(ctx context.Context) error {
	bookings, err := c.listRecovering(ctx)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to list recovering bookings for sweep")
		return err
	}
	if len(bookings) == 0 {
		return nil
	}
	c.logger.Info().Ctx(ctx).Int("count", len(bookings)).Msg("Sweeping recovering bookings")
	for _, b := range bookings {
		dto := reconstructDto(b)
		if dto.BookingId == "" {
			continue
		}
		if err := c.ProcessItem(ctx, dto); err != nil {
			// left in Recovering; the next sweep retries it
			c.logger.Error().Ctx(ctx).Err(err).Str("bookingId", dto.BookingId).Msg("Sweep failed to process recovering booking")
		}
	}
	return nil
}

// requeue pushes the item back with an incremented attempt counter; past the
// cap the booking is parked as RequireManualIntervention instead
func (c *Client) requeue(ctx context.Context, dto lib.RecoverDto) {
	dto.Attempts++
	if dto.Attempts >= c.config.MaxAttempts {
		c.logger.Error().Ctx(ctx).Str("bookingId", dto.BookingId).Int("attempts", dto.Attempts).
			Msg("Recover item exceeded max attempts, parking for manual intervention")
		if err := c.markManualIntervention(ctx, dto.BookingId); err != nil {
			// keep it on the queue rather than lose it
			c.logger.Error().Ctx(ctx).Err(err).Str("bookingId", dto.BookingId).Msg("Failed to park exhausted item, re-queueing anyway")
		} else {
			return
		}
	}

	enc, err := c.encr.EncryptAny(dto)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("bookingId", dto.BookingId).Msg("Failed to encrypt recover dto for requeue")
		return
	}
	tracer := otel.Tracer(c.psm)
	_, err = c.mainRedis.QueuePush(ctx, tracer, c.config.QueueName, enc)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("bookingId", dto.BookingId).Msg("Failed to requeue recover dto")
	}
}
