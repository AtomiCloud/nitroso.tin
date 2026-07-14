package session

import (
	"context"
	"errors"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"strings"
)

type Session struct {
	ktmb      *ktmb.Ktmb
	main      *otelredis.OtelRedis
	logger    *zerolog.Logger
	key       string
	encryptor encryptor.Encryptor[ktmb.LoginRes]
}

func New(ktmb *ktmb.Ktmb, main *otelredis.OtelRedis, logger *zerolog.Logger, key string, encryptor encryptor.Encryptor[ktmb.LoginRes]) Session {
	return Session{
		ktmb:      ktmb,
		main:      main,
		logger:    logger,
		key:       key,
		encryptor: encryptor,
	}
}

func (s *Session) Login(ctx context.Context, email, password string) (string, error) {
	// check cache
	l := s.logger.With().Ctx(ctx).Str("redisKey", s.key).Logger()

	l.Info().Msgf("Checking cache for existing login using key %s...", s.key)

	exists, err := s.main.Exists(ctx, s.key).Result()
	if err != nil {
		l.Error().
			Err(err).
			Msgf("Failed to check if key %s for login session exists", s.key)
		return "", err
	}

	// cache hit
	if exists != 0 {
		l.Info().Msgf("Login session found in cache, retrieving token...")
		tokenEnc, er := s.main.Get(ctx, s.key).Result()
		if er != nil {
			l.Error().Err(er).Msgf("Failed to get login session with key %s", s.key)
			return "", er
		}
		l.Info().Str("token", tokenEnc).Msg("Decrypting login session token...")
		token, er := s.encryptor.Decrypt(tokenEnc)
		if er != nil {
			l.Error().Str("token", tokenEnc).Err(err).Msg("Failed to decrypt login session token")
			return "", err
		}
		l.Info().Msg("Successfully decrypted login session")
		return token, nil

	}
	// cache miss
	l.Info().Msg("Login session not found in cache, logging in...")
	login, err := s.ktmb.Login(email, password)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to login")
		return "", err
	}
	if !login.Status {
		s.logger.Error().Strs("errors", login.Messages).Msg("Failed to login")
		return "", errors.New(strings.Join(login.Messages, ", "))
	}
	l.Info().Msg("Successfully logged in. Encrypting login session token...")
	token := login.Data.UserData
	enc, err := s.encryptor.Encrypt(token)
	if err != nil {
		l.Error().Err(err).Msg("Failed to encrypt login session token")
		return "", err
	}
	l.Info().Str("token", enc).Msg("Successfully encrypted login session token")

	l.Info().Str("token", enc).Msg("Saving login session token to cache...")
	result, err := s.main.Set(ctx, s.key, enc, 0).Result()
	if err != nil {
		l.Error().Err(err).
			Str("redisCmd", result).
			Msgf("Failed to set key: %s. Result: %s", s.key, result)
		return "", err
	}

	l.Info().Msg("Successfully save login session to cache")
	return token, nil
}

// Invalidate removes token from the shared login cache only if it is still the
// value stored there. The compare-and-delete protects a fresh token written by
// another consumer between the rejected request and this invalidation.
func (s *Session) Invalidate(ctx context.Context, token string) error {
	l := s.logger.With().Ctx(ctx).Str("redisKey", s.key).Logger()

	tokenEnc, err := s.main.Get(ctx, s.key).Result()
	if errors.Is(err, redis.Nil) {
		l.Info().Msg("Login session cache is already empty")
		return nil
	}
	if err != nil {
		l.Error().Err(err).Msg("Failed to read cached login session for invalidation")
		return err
	}

	cached, err := s.encryptor.Decrypt(tokenEnc)
	if err != nil {
		l.Error().Err(err).Msg("Failed to decrypt cached login session for invalidation")
		return err
	}
	if cached != token {
		l.Info().Msg("Login session was already refreshed; keeping replacement token")
		return nil
	}

	const compareAndDelete = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`
	deleted, err := s.main.Eval(ctx, compareAndDelete, []string{s.key}, tokenEnc).Int64()
	if err != nil {
		l.Error().Err(err).Msg("Failed to invalidate cached login session")
		return err
	}
	l.Info().Bool("deleted", deleted != 0).Msg("Invalidated rejected login session")
	return nil
}
