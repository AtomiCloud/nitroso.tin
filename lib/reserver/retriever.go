package reserver

import (
	"context"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
)

type Retriever struct {
	redis    *otelredis.OtelRedis
	encr     encryptor.Encryptor[enricher.FindStore]
	logger   *zerolog.Logger
	enricher config.EnricherConfig
}

func NewRetriever(rds *otelredis.OtelRedis, e encryptor.Encryptor[enricher.FindStore], logger *zerolog.Logger, enricher config.EnricherConfig) *Retriever {
	return &Retriever{
		redis:    rds,
		encr:     e,
		logger:   logger,
		enricher: enricher,
	}
}

func (r *Retriever) GetLoginData(ctx context.Context) (*LoginStore, error) {

	r.logger.Info().Msgf("Getting Login Data: %s\n", r.enricher.UserDataKey)
	exists, err := r.redis.Exists(ctx, r.enricher.UserDataKey).Result()
	if err != nil {
		r.logger.Error().Ctx(ctx).Err(err).Msg("Failed to check if userdata key exists")
		return nil, err
	}

	if exists == 0 {
		r.logger.Info().Ctx(ctx).Msgf("Key '%s' does not exist\n", r.enricher.UserDataKey)
		return nil, nil
	}

	exists, err = r.redis.Exists(ctx, r.enricher.StoreKey).Result()
	if err != nil {
		r.logger.Error().Ctx(ctx).Err(err).Msg("Failed to check if store key exists")
		return nil, err
	}

	if exists == 0 {
		r.logger.Info().Ctx(ctx).Msgf("Key '%s' does not exist\n", r.enricher.StoreKey)
		return nil, nil
	}

	userDataEnc, err := r.redis.Get(ctx, r.enricher.UserDataKey).Result()
	if err != nil {
		r.logger.Error().Err(err).Msg("Failed to get user data")
		return nil, err
	}

	storeEnc, err := r.redis.Get(ctx, r.enricher.StoreKey).Result()
	if err != nil {
		r.logger.Error().Err(err).Msg("Failed to get store")
		return nil, err
	}

	userData, err := r.encr.Decrypt(userDataEnc)
	if err != nil {
		r.logger.Error().Err(err).Msg("Failed to decrypt user data")
		return nil, err
	}

	store, err := r.encr.DecryptAny(storeEnc)
	if err != nil {
		r.logger.Error().Err(err).Msg("Failed to decrypt store")
		return nil, err
	}
	return &LoginStore{
		UserData: userData,
		Find:     store,
	}, nil

}
