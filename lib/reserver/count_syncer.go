package reserver

import (
	"context"
	"encoding/json"
	"github.com/AtomiCloud/nitroso-tin/lib/count"
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
	streamRedis      *otelredis.OtelRedis
	countReader      *count.Client
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	psm              string
	streamConfig     config.StreamConfig
	reserver         config.ReserverConfig
}

func NewCountSyncer(toDiffer, toReserver chan Count, streamRedis *otelredis.OtelRedis,
	otelConfigurator *telemetry.OtelConfigurator, logger *zerolog.Logger, psm string,
	streamConfig config.StreamConfig, reserver config.ReserverConfig, countReader *count.Client) CountSyncer {
	return CountSyncer{
		toDiffer:         toDiffer,
		toReserver:       toReserver,
		streamRedis:      streamRedis,
		countReader:      countReader,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		psm:              psm,
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
	status := s.streamRedis.XGroupCreateMkStream(ctx, s.streamConfig.Update, s.reserver.Group, "0")
	s.logger.Info().Msg("Group Create Status: " + status.String())
}

func (s *CountSyncer) update(ctx context.Context) error {

	exists, counts, err := s.countReader.GetReserverCount(ctx, time.Now())
	if !exists {
		s.logger.Info().Ctx(ctx).Msg("Key does not exist")
		return nil
	}
	if err != nil {
		s.logger.Error().Ctx(ctx).Err(err).Msg("Failed to get counts from redis")
		return err
	}

	dCount := make(Count)
	rCount := make(Count)
	maps.Copy(dCount, counts)
	maps.Copy(rCount, counts)

	s.logger.Info().Ctx(ctx).Msgf("Sending counts to reserver differ")
	s.toDiffer <- dCount
	s.logger.Info().Ctx(ctx).Msgf("Sending counts to reserver reserver")
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

	s.logger.Info().Ctx(ctx).Msg("Reserver waiting for CDC signal...")

	tracer := otel.Tracer(s.psm)
	err = s.streamRedis.StreamGroupRead(ctx, tracer, s.streamConfig.Update, s.reserver.Group, consumerId, func(ctx context.Context, message json.RawMessage) error {
		s.logger.Info().Ctx(ctx).Msg("Reserver received CDC emitted signal")
		return s.update(ctx)
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to read from redis stream in reserver")
		return false, err
	}
	return false, nil
}
