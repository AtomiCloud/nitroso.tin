package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/auth"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"io"
	"math"
	"time"
)

var baseDelay = 1 * time.Second

// What your applicaiton needs

type Cdc struct {
	redis            *otelredis.OtelRedis
	streamConfig     config.StreamConfig
	cdcConfig        config.CdcConfig
	logger           *zerolog.Logger
	otelConfigurator *telemetry.OtelConfigurator
	psd              string
	cred             auth.CredentialsProvider
}

func NewCdc(rds *otelredis.OtelRedis, ccs config.CdcConfig, streams config.StreamConfig, logger *zerolog.Logger, otelConfigurator *telemetry.OtelConfigurator, psd string, cred auth.CredentialsProvider) *Cdc {

	return &Cdc{
		redis:            rds,
		streamConfig:     streams,
		cdcConfig:        ccs,
		logger:           logger,
		otelConfigurator: otelConfigurator,
		psd:              psd,
		cred:             cred,
	}
}

func (c *Cdc) createGroup(ctx context.Context) {
	status := c.redis.XGroupCreateMkStream(ctx, c.streamConfig.Cdc, c.cdcConfig.Group, "0")
	c.logger.Info().Msg("Group Create Status: " + status.String())
}

func (c *Cdc) Start(ctx context.Context, consumerId string) error {

	maxCounter := c.cdcConfig.Parallelism

	errorCounter := 0

	c.createGroup(ctx)
	for {
		shouldExit, err := c.loop(ctx, consumerId)
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

func (c *Cdc) loop(ctx context.Context, consumerId string) (bool, error) {
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
	tracer := otel.Tracer(c.psd)
	ctx, span := tracer.Start(ctx, "CDC subscriber")
	defer span.End()

	c.logger.Info().Ctx(ctx).Msg("Waiting for message...")
	err = c.redis.StreamGroupRead(ctx, tracer, c.streamConfig.Cdc, c.cdcConfig.Group, consumerId, func(ctx context.Context, message json.RawMessage) error {
		c.logger.Info().Ctx(ctx).Msg("Received CDC signal")
		return c.sync(ctx, tracer)
	})
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to read")
		return false, err
	}
	return false, nil
}

func (c *Cdc) sync(ctx context.Context, tracer trace.Tracer) error {
	c.logger.Info().Ctx(ctx).Msg("Starting CDC process")

	endpoint := fmt.Sprintf("%s://%s:%s", c.cdcConfig.Scheme, c.cdcConfig.Host, c.cdcConfig.Port)

	client, er := zinc.NewClient(endpoint,
		zinc.WithHTTPClient(otelhttp.DefaultClient),
		zinc.WithRequestEditorFn(c.cred.RequestEditor()))
	if er != nil {
		c.logger.Info().Ctx(ctx).Msg("Failed to create Zinc client")
		return er
	}

	c.logger.Info().Ctx(ctx).Msg("Calling Zinc API endpoint: " + endpoint)
	resp, er := client.GetApiVVersionBookingCounts(ctx, "1.0")
	if er != nil {
		c.logger.Error().Err(er).Msgf("Failed to call CDC endpoint")
		return er
	} else {
		c.logger.Info().Ctx(ctx).Msg("CDC endpoint response: " + resp.Status)
	}
	defer resp.Body.Close()

	content, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		c.logger.Error().Err(readErr).Msg("Failed to read response from http response from CDC endpoint")
		return readErr
	}
	// Decode the response into a struct
	var data []zinc.BookingCountRes
	er = json.Unmarshal(content, &data)
	if er != nil {
		c.logger.Error().Err(er).
			Str("content", string(content)).
			Msg("Failed to decode response from CDC endpoint")
		return er
	}

	c.logger.Info().Ctx(ctx).Any("counts", data).Msg("CDC endpoint response decoded")

	// Record<direction, date, time, count>
	counts := make(map[string]map[string]map[string]int)

	for _, d := range data {
		count := int(*d.TicketsNeeded)
		dir := *d.Direction
		date := *d.Date
		t := *d.Time

		if counts[dir] == nil {
			counts[dir] = make(map[string]map[string]int)
		}
		if counts[dir][date] == nil {
			counts[dir][date] = make(map[string]int)
		}
		counts[dir][date][t] = count

	}

	key := fmt.Sprintf("%s:%s", c.psd, "count")

	out, er := json.Marshal(counts)
	if er != nil {
		c.logger.Error().Err(er).
			Msg("Failed to marshal counts")
		return er
	}
	c.logger.Info().Ctx(ctx).Msg("Booking Counts: " + string(out))
	result, er := c.redis.Set(ctx, key, string(out), 0).Result()
	if er != nil {
		c.logger.Error().Err(er).
			Str("redisKey", key).
			Str("redisCmd", result).
			Msgf("Failed to set key: %s. Result: %s", key, result)
		return er
	}

	// notify the stream
	cmdErr, redErr := c.redis.StreamAdd(ctx, tracer, c.streamConfig.Update, "ping")
	if redErr != nil {
		c.logger.Error().Err(redErr).Msgf("Failed to notify enricher and pollers: %s", cmdErr)
		return redErr
	}
	return nil
}
