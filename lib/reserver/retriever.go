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
	mainRedis *otelredis.OtelRedis
	encr      encryptor.Encryptor[enricher.FindStore]
	logger    *zerolog.Logger
	enricher  config.EnricherConfig
}

func NewRetriever(mainRedis *otelredis.OtelRedis, e encryptor.Encryptor[enricher.FindStore], logger *zerolog.Logger, enricher config.EnricherConfig) *Retriever {
	return &Retriever{
		mainRedis: mainRedis,
		encr:      e,
		logger:    logger,
		enricher:  enricher,
	}
}

func (r *Retriever) GetLoginData(ctx context.Context) (*LoginStore, error) {

	r.logger.Info().Msgf("Getting Login Data: %s", r.enricher.UserDataKey)
	exists, err := r.mainRedis.Exists(ctx, r.enricher.UserDataKey).Result()
	if err != nil {
		r.logger.Error().Ctx(ctx).Err(err).Msg("Failed to check if userdata key exists")
		return nil, err
	}

	if exists == 0 {
		r.logger.Info().Ctx(ctx).Msgf("Key '%s' does not exist", r.enricher.UserDataKey)
		return nil, nil
	}

	exists, err = r.mainRedis.Exists(ctx, r.enricher.StoreKey).Result()
	if err != nil {
		r.logger.Error().Ctx(ctx).Err(err).Msg("Failed to check if store key exists")
		return nil, err
	}

	if exists == 0 {
		r.logger.Info().Ctx(ctx).Msgf("Key '%s' does not exist", r.enricher.StoreKey)
		return nil, nil
	}

	userDataEnc, err := r.mainRedis.Get(ctx, r.enricher.UserDataKey).Result()
	r.logger.Info().Msg("Successfully obtained userdata from redis")
	if err != nil {
		r.logger.Error().Err(err).Msg("Failed to get user data")
		return nil, err
	}

	storeEnc, err := r.mainRedis.Get(ctx, r.enricher.StoreKey).Result()
	r.logger.Info().Msg("Successfully obtained store from redis")
	if err != nil {
		r.logger.Error().Err(err).Msg("Failed to get store")
		return nil, err
	}

	userData, err := r.encr.Decrypt(userDataEnc)
	r.logger.Info().Msg("Successfully decrypted user data")
	if err != nil {
		r.logger.Error().Err(err).Msg("Failed to decrypt user data")
		return nil, err
	}

	store, err := r.encr.DecryptAny(storeEnc)

	r.logger.Info().Any("store", enricher.StoreToPublic(store)).Msg("Successfully decrypted store")
	if err != nil {
		r.logger.Error().Err(err).Msg("Failed to decrypt store")
		return nil, err
	}
	return &LoginStore{
		UserData: userData,
		Find:     store,
	}, nil

}
