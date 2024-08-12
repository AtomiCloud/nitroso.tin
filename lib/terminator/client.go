package terminator

import (
	"context"
	"encoding/json"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"math"
	"time"
)

type Client struct {
	terminator       *Terminator
	redis            *otelredis.OtelRedis
	otelConfigurator *telemetry.OtelConfigurator
	config           config.TerminatorConfig
	logger           *zerolog.Logger
	psm              string
}

var baseDelay = 1 * time.Second

func New(terminator *Terminator, redis *otelredis.OtelRedis, otelConfigurator *telemetry.OtelConfigurator,
	config config.TerminatorConfig,
	logger *zerolog.Logger,
	psm string) *Client {
	return &Client{
		terminator:       terminator,
		redis:            redis,
		otelConfigurator: otelConfigurator,
		config:           config,
		logger:           logger,
		psm:              psm,
	}
}

func (c *Client) Start(ctx context.Context) error {
	maxCounter := c.config.BackoffLimit

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

	c.logger.Info().Ctx(ctx).Str("queue", c.config.QueueName).Msg("Terminator waiting for zinc message...")
	err = c.redis.QueuePop(ctx, tracer, c.config.QueueName, func(ctx context.Context, message json.RawMessage) error {
		c.logger.Info().Ctx(ctx).Msg("Terminator received zinc emitted termination")
		var termMessage BookingTermination
		e := json.Unmarshal(message, &termMessage)
		if e != nil {
			c.logger.Error().Err(e).Msg("Failed to unmarshal zinc emitted termination")
			return e
		}

		c.logger.Info().Any("termination", termMessage).Ctx(ctx).Msg("Termination message")
		er := c.terminator.Terminate(termMessage)
		if er != nil {
			c.logger.Error().Err(er).Msg("Failed to terminate")
			return er
		}
		return nil
	})
	if err != nil {
		c.logger.Error().
			Err(err).
			Msg("Failed to read from redis list in terminator (from zinc)")
		return false, err
	}
	return false, nil
}
