package reserver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"golang.org/x/exp/maps"
	"math"
	"time"
)

var baseDelay = 1 * time.Second

type CountSyncer struct {
	toDiffer         chan Count
	toReserver       chan Count
	redis            *otelredis.OtelRedis
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	psd              string
	streamConfig     config.StreamConfig
	reserver         config.ReserverConfig
}

func NewCountSyncer(toDiffer, toReserver chan Count, rds *otelredis.OtelRedis,
	otelConfigurator *telemetry.OtelConfigurator, logger *zerolog.Logger, psd string,
	streamConfig config.StreamConfig, reserver config.ReserverConfig) CountSyncer {
	return CountSyncer{
		toDiffer:         toDiffer,
		toReserver:       toReserver,
		redis:            rds,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		psd:              psd,
		streamConfig:     streamConfig,
		reserver:         reserver,
	}
}

func (s *CountSyncer) Start(ctx context.Context, consumerId string) error {
	maxCounter := s.reserver.BackoffLimit

	errorCounter := 0

	iErr := s.update(ctx)
	if iErr != nil {
		s.logger.Error().Err(iErr).Msg("Failed to update counts")
		return iErr
	}

	s.createGroup(ctx)
	for {
		shouldExit, err := s.loop(ctx, consumerId)
		if err != nil {
			if errorCounter >= maxCounter {
				s.logger.Error().Err(err).Msg("Failed all backoff attempts, exiting...")
				return err
			}
			secRetry := math.Pow(2, float64(errorCounter))
			s.logger.Info().Msgf("Retrying operation in %f seconds", secRetry)
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

func (s *CountSyncer) createGroup(ctx context.Context) {
	status := s.redis.XGroupCreateMkStream(ctx, s.streamConfig.Update, s.reserver.Group, "0")
	s.logger.Info().Msg("Group Create Status: " + status.String())
}

func (s *CountSyncer) update(ctx context.Context) error {

	key := fmt.Sprintf("%s:%s", s.psd, "count")

	s.logger.Info().Ctx(ctx).Msgf("Checking if key '%s' exists", key)
	exists, err := s.redis.Exists(ctx, key).Result()
	if err != nil {
		s.logger.Error().Ctx(ctx).Err(err).Msg("Failed to check if userdata key exists")
		return err
	}
	if exists == 0 {
		s.logger.Info().Ctx(ctx).Msgf("Key '%s' does not exist", key)
		return nil
	}

	s.logger.Info().Ctx(ctx).Msgf("Getting counts from redis '%s'", key)
	countsJson, rErr := s.redis.Get(ctx, key).Result()
	if rErr != nil {
		s.logger.Error().Ctx(ctx).Err(rErr).Msg("Failed to get counts from redis")
		return rErr
	}

	var counts map[string]map[string]map[string]int
	rErr = json.Unmarshal([]byte(countsJson), &counts)
	if rErr != nil {
		s.logger.Error().Ctx(ctx).Err(rErr).Msg("Failed to unmarshal counts")
		return rErr
	}

	dCount := make(Count)
	rCount := make(Count)
	maps.Copy(dCount, counts)
	maps.Copy(rCount, counts)

	s.toDiffer <- dCount
	s.toReserver <- rCount
	return nil
}

func (s *CountSyncer) loop(ctx context.Context, consumerId string) (bool, error) {
	shutdown, err := s.otelConfigurator.Configure(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to configure telemetry")
		return true, err
	}
	defer func() {
		deferErr := shutdown(ctx)
		if deferErr != nil {
			panic(deferErr)
		}
	}()
	tracer := otel.Tracer(s.psd)
	ctx, span := tracer.Start(ctx, "Reserver watcher")
	defer span.End()

	s.logger.Info().Ctx(ctx).Msg("Waiting for message...")
	err = s.redis.StreamGroupRead(ctx, tracer, s.streamConfig.Update, s.reserver.Group, consumerId, func(ctx context.Context, message json.RawMessage) error {
		s.logger.Info().Ctx(ctx).Msg("Received CDC emitted signal")
		return s.update(ctx)
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to read from redis stream in reserver")
		return false, err
	}
	return false, nil
}
