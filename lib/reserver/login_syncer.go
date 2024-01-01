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
	"math"
	"time"
)

type LoginSyncer struct {
	toReserver       chan LoginStore
	redis            *otelredis.OtelRedis
	retriever        *Retriever
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	psd              string
	streamConfig     config.StreamConfig
	reserver         config.ReserverConfig
}

func NewLoginSyncer(toReserver chan LoginStore, rds *otelredis.OtelRedis, retriever *Retriever,
	otelConfigurator *telemetry.OtelConfigurator, logger *zerolog.Logger, psd string,
	streamConfig config.StreamConfig, reserver config.ReserverConfig) LoginSyncer {
	return LoginSyncer{
		toReserver:       toReserver,
		redis:            rds,
		retriever:        retriever,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		psd:              psd,
		streamConfig:     streamConfig,
		reserver:         reserver,
	}
}

func (l *LoginSyncer) Start(ctx context.Context, consumerId string) error {
	maxCounter := l.reserver.BackoffLimit

	errorCounter := 0

	iErr := l.update(ctx)
	if iErr != nil {
		l.logger.Error().Err(iErr).Msg("Failed to update encrypted stores")
		return iErr
	}
	l.createGroup(ctx)
	for {
		shouldExit, err := l.loop(ctx, consumerId)
		if err != nil {
			if errorCounter >= maxCounter {
				l.logger.Error().Err(err).Msg("Failed all backoff attempts, exiting...")
				return err
			}
			secRetry := math.Pow(2, float64(errorCounter))
			l.logger.Info().Msgf("Retrying operation in %f seconds", secRetry)
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

func (l *LoginSyncer) createGroup(ctx context.Context) {
	status := l.redis.XGroupCreateMkStream(ctx, l.streamConfig.Enrich, l.reserver.Group, "0")
	l.logger.Info().Msg("Group Create Status: " + status.String())
}

func (l *LoginSyncer) update(ctx context.Context) error {
	key := fmt.Sprintf("%s:%s", l.psd, "count")
	l.logger.Info().Ctx(ctx).Msgf("Getting counts from redis '%s'", key)

	data, err := l.retriever.GetLoginData(ctx)
	if err != nil {
		l.logger.Error().Ctx(ctx).Err(err).Msg("Failed to get login data")
		return err
	}

	if data != nil {
		message := LoginStore{
			UserData: data.UserData,
			Find:     data.Find,
		}
		l.toReserver <- message
	}
	return nil
}

func (l *LoginSyncer) loop(ctx context.Context, consumerId string) (bool, error) {
	shutdown, err := l.otelConfigurator.Configure(ctx)
	if err != nil {
		l.logger.Error().Err(err).Msg("Failed to configure telemetry")
		return true, err
	}
	defer func() {
		deferErr := shutdown(ctx)
		if deferErr != nil {
			panic(deferErr)
		}
	}()
	tracer := otel.Tracer(l.psd)

	l.logger.Info().Ctx(ctx).Msg("Reserver 'login syncer', waiting for enricher ping...")

	err = l.redis.StreamGroupRead(ctx, tracer, l.streamConfig.Enrich, l.reserver.Group, consumerId, func(ctx context.Context, message json.RawMessage) error {
		l.logger.Info().Ctx(ctx).Msg("Reserver 'login syncer', received enricher emitted signal")
		return l.update(ctx)
	})
	if err != nil {
		l.logger.Error().Err(err).Msg("Failed to read from redis stream in reserver from enrich stream")
		return false, err
	}
	return false, nil
}
