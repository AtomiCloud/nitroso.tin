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
//
// SINGLE REPLICA ONLY. The drain pops with a destructive RPop and the sweep's
// queued-skip / handled-skip dedup assumes the drain and sweep run sequentially
// in one process. Running more than one recoverer concurrently would let two
// drains double-pop and a sweep weak-process an item another replica is
// handling — risking a wrongful refund. Keep replicaCount at 1 (see the Helm
// values); do not add an HPA.
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

	// buffered for both tick types so a coincident drain+sweep at the top of the
	// hour neither blocks nor drops the sweep
	ch := make(chan string, 2)

	cr := cron.New()
	if err = cr.AddFunc(c.config.DrainCron, func() {
		select {
		case ch <- "drain":
		default:
		}
	}); err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("cron", c.config.DrainCron).Msg("Failed to schedule recoverer drain cron")
		return err
	}
	if err = cr.AddFunc(c.config.SweepCron, func() {
		select {
		case ch <- "sweep":
		default:
		}
	}); err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("cron", c.config.SweepCron).Msg("Failed to schedule recoverer sweep cron")
		return err
	}
	cr.Start()
	defer cr.Stop()

	// run a full drain+sweep once on startup so a redeploy doesn't wait a period
	ch <- "sweep"

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case trigger := <-ch:
			c.logger.Info().Ctx(ctx).Str("trigger", trigger).Msg("Recoverer cycle starting")
			// both ticks drain (fast path); only the sweep tick reconciles zinc.
			handled, drainErr := c.Drain(ctx)
			if drainErr != nil {
				c.logger.Error().Ctx(ctx).Err(drainErr).Msg("Recoverer drain failed")
			}
			if trigger == "sweep" {
				// safety net: the queue is a fast-path hint (destructive RPop), so
				// a lost item would strand a booking in Recovering forever.
				// Reconcile against zinc, the durable source of truth — skipping
				// bookings this drain just handled (their queue item carries the
				// buyer's deterministic ticket identifiers, which the zinc
				// reconstruction lacks) and those still on the queue.
				if sweepErr := c.Sweep(ctx, handled); sweepErr != nil {
					c.logger.Error().Ctx(ctx).Err(sweepErr).Msg("Recoverer sweep failed")
				}
			}
			c.logger.Info().Ctx(ctx).Str("trigger", trigger).Msg("Recoverer cycle complete")
		}
	}
}

// Drain pops every item currently on the recover queue and processes each.
// Per-item failures are re-queued (with an attempt cap), never dropped. It
// returns the set of BookingIds it touched this cycle so the sweep can skip
// them (their queue item carries stronger info than the zinc reconstruction).
func (c *Client) Drain(ctx context.Context) (map[string]bool, error) {
	tracer := otel.Tracer(c.psm)
	handled := map[string]bool{}

	// cap at the queue length observed at the start so re-queued items are not
	// re-processed within the same drain
	length, err := c.mainRedis.LLen(ctx, c.config.QueueName).Result()
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("queue", c.config.QueueName).Msg("Failed to read recover queue length")
		return handled, err
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
			handled[dto.BookingId] = true
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
			return handled, popErr
		}
		if !found {
			break
		}
	}
	return handled, nil
}

// Sweep reconciles against zinc: every booking still in Recovering is
// re-derived and re-processed, so a booking whose queue item was lost (crash
// mid-drain, requeue push failure, malformed message) is never stranded.
// zinc — not the queue — is the durable source of truth for booking state.
// skip holds BookingIds already handled by the drain this cycle: their live
// queue item carries the buyer's deterministic ticket identifiers, so
// re-deriving them here (BookingNo unknown) would take a strictly weaker path.
func (c *Client) Sweep(ctx context.Context, skip map[string]bool) error {
	bookings, err := c.listRecovering(ctx)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to list recovering bookings for sweep")
		return err
	}
	if len(bookings) == 0 {
		return nil
	}
	// Also skip any booking that still has a live queue item — parked
	// concurrently by the buyer after the drain's snapshot, or left behind by a
	// drain error. park() pushes the queue item BEFORE the Recovering
	// transition, so every such booking we see as Recovering is captured here;
	// leaving it for the next drain preserves its deterministic ticket
	// identifiers instead of taking the weaker zinc-reconstructed scan path.
	queued, err := c.queuedBookingIds(ctx)
	if err != nil {
		// don't risk the weak path on a partial view of live items
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to read live queue items, skipping sweep this cycle")
		return err
	}

	c.logger.Info().Ctx(ctx).Int("count", len(bookings)).Msg("Sweeping recovering bookings")
	for _, b := range bookings {
		dto := ReconstructDto(b)
		if dto.BookingId == "" || skip[dto.BookingId] || queued[dto.BookingId] {
			continue
		}
		if err := c.ProcessItem(ctx, dto); err != nil {
			// requeue with an attempt count so the failure is durably counted:
			// repeated failures escalate to RequireManualIntervention (§5.7)
			// instead of silently retrying every sweep forever. The live queue
			// item also makes the next cycle's drain (not the sweep) retry it.
			c.logger.Error().Ctx(ctx).Err(err).Str("bookingId", dto.BookingId).Msg("Sweep failed to process recovering booking, requeueing with attempt count")
			c.requeue(ctx, dto)
		}
	}
	return nil
}

// queuedBookingIds returns the BookingIds of every recover record currently on
// the queue (decrypting each envelope). Used by the sweep to avoid re-deriving
// a booking that still has a live, stronger queue item.
func (c *Client) queuedBookingIds(ctx context.Context) (map[string]bool, error) {
	ids := map[string]bool{}
	vals, err := c.mainRedis.LRange(ctx, c.config.QueueName, 0, -1).Result()
	if err != nil {
		return ids, err
	}
	for _, v := range vals {
		var msg otelredis.OtelRedisMessage
		if e := json.Unmarshal([]byte(v), &msg); e != nil {
			continue
		}
		enc, ok := msg.Message.(string)
		if !ok {
			continue
		}
		dto, e := c.encr.DecryptAny(enc)
		if e != nil {
			continue
		}
		ids[dto.BookingId] = true
	}
	return ids, nil
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
	// The queue is the sole durable store of this item's attempt count, so a
	// transient Redis failure here would silently reset §5.7 escalation the next
	// sweep (which reconstructs Attempts:0 from zinc). Retry a few times before
	// giving up; a total failure still leaves the booking Recovering in zinc, so
	// the next sweep re-derives and re-processes it.
	for attempt := 0; attempt < recoverRequeueRetries; attempt++ {
		if _, err = c.mainRedis.QueuePush(ctx, tracer, c.config.QueueName, enc); err == nil {
			return
		}
		c.logger.Error().Ctx(ctx).Err(err).Int("attempt", attempt).Str("bookingId", dto.BookingId).Msg("Failed to requeue recover dto, retrying")
		select {
		case <-ctx.Done():
			c.logger.Error().Ctx(ctx).Err(ctx.Err()).Str("bookingId", dto.BookingId).Msg("Requeue aborted by context; item stays Recovering in zinc for the next sweep")
			return
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}
	c.logger.Error().Ctx(ctx).Err(err).Str("bookingId", dto.BookingId).Msg("Failed to requeue recover dto after retries; item stays Recovering in zinc for the next sweep")
}

// recoverRequeueRetries bounds how many times requeue retries a failed Redis
// push before falling back to zinc's durable Recovering status.
const recoverRequeueRetries = 3
