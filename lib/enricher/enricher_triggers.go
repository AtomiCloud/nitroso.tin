package enricher

import (
	"context"
	"encoding/json"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/robfig/cron"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"math"
	"math/rand"
	"time"
)

var baseDelay = 1 * time.Second

type TriggerMessage struct {
	Type string
	Mc   propagation.MapCarrier
}

type Trigger struct {
	channel          chan TriggerMessage
	streamRedis      *otelredis.OtelRedis
	stream           config.StreamConfig
	enricher         config.EnricherConfig
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	psm              string
	timezone         *time.Location
}

func NewTrigger(channel chan TriggerMessage, logger *zerolog.Logger, streamRedis *otelredis.OtelRedis,
	streams config.StreamConfig, enricher config.EnricherConfig, otelConfigurator *telemetry.OtelConfigurator,
	psm string, timezone *time.Location) *Trigger {

	return &Trigger{
		channel:          channel,
		streamRedis:      streamRedis,
		stream:           streams,
		enricher:         enricher,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		psm:              psm,
		timezone:         timezone,
	}
}

func (p *Trigger) Cron(ctx context.Context) {

	c := cron.NewWithLocation(p.timezone)
	c.AddFunc("@midnight", func() {
		p.channel <- TriggerMessage{
			Type: "cron",
			Mc:   nil,
		}
	})
}

func (p *Trigger) RandomTrigger(ctx context.Context) {
	source := rand.NewSource(time.Now().UnixNano())
	random := rand.New(source)

	for {
		p.channel <- TriggerMessage{
			Type: "random",
			Mc:   nil,
		}
		randomInt := random.Intn(7200) + 7200
		duration := time.Duration(randomInt) * time.Second
		time.Sleep(duration)
	}
}

func (p *Trigger) RedisStream(ctx context.Context, consumerId string) error {

	maxCounter := p.enricher.BackoffLimit

	errorCounter := 0

	p.createGroup(ctx)

	for {
		for {
			shouldExit, err := p.redisLoop(ctx, consumerId)
			if err != nil {
				if errorCounter >= maxCounter {
					p.logger.Error().Err(err).Msg("Failed all backoff attempts, exiting...")
					return err
				}
				secRetry := math.Pow(2, float64(errorCounter))
				p.logger.Info().Msgf("Retrying operation in %f seconds", secRetry)
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
	}
}

func (p *Trigger) createGroup(ctx context.Context) {
	status := p.streamRedis.XGroupCreateMkStream(ctx, p.stream.Update, p.enricher.Group, "0")
	p.logger.Info().Msg("Group Create Status: " + status.String())
}

func (p *Trigger) redisLoop(ctx context.Context, consumerId string) (bool, error) {
	shutdown, err := p.otelConfigurator.Configure(ctx)
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to configure telemetry")
		return true, err
	}
	defer func() {
		deferErr := shutdown(ctx)
		if deferErr != nil {
			panic(deferErr)
		}
	}()
	tracer := otel.Tracer(p.psm)

	p.logger.Info().Ctx(ctx).Msg("Enricher waiting for CDC messages...")
	err = p.streamRedis.StreamGroupRead(ctx, tracer, p.stream.Update, p.enricher.Group, consumerId, func(ctx context.Context, _ json.RawMessage) error {
		p.logger.Info().Ctx(ctx).Msg("Enricher received CDC signal")

		mc := propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(ctx, mc)

		p.channel <- TriggerMessage{
			Type: "redis-stream",
			Mc:   mc,
		}
		return nil
	})
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to read")
		return false, err
	}
	return false, err
}
