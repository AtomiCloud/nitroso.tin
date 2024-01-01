package enricher

import (
	"context"
	"encoding/json"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"math"
	"math/rand"
	"time"
)

var baseDelay = 1 * time.Second

type Trigger struct {
	channel          chan string
	redis            *otelredis.OtelRedis
	stream           config.StreamConfig
	enricher         config.EnricherConfig
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	psd              string
}

func NewTrigger(channel chan string, logger *zerolog.Logger, rds *otelredis.OtelRedis,
	streams config.StreamConfig, enricher config.EnricherConfig, otelConfigurator *telemetry.OtelConfigurator,
	psd string) *Trigger {

	return &Trigger{
		channel:          channel,
		redis:            rds,
		stream:           streams,
		enricher:         enricher,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		psd:              psd,
	}
}

func (p *Trigger) RandomTrigger(ctx context.Context) {
	source := rand.NewSource(time.Now().UnixNano())
	random := rand.New(source)

	for {
		p.channel <- "random"
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
	status := p.redis.XGroupCreateMkStream(ctx, p.stream.Update, p.enricher.Group, "0")
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
	tracer := otel.Tracer(p.psd)
	ctx, span := tracer.Start(ctx, "CDC Poller")
	defer span.End()
	p.logger.Info().Ctx(ctx).Msg("Waiting for CDC messages...")
	err = p.redis.StreamGroupRead(ctx, tracer, p.stream.Update, p.enricher.Group, consumerId, func(ctx context.Context, _ json.RawMessage) error {
		p.logger.Info().Ctx(ctx).Msg("Received CDC signal")
		p.channel <- "redis-stream"
		return nil
	})
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to read")
		return false, err
	}
	return false, err
}
