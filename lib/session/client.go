package session

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
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
	l := s.logger.With().Ctx(ctx).Str("redisKey", s.key).Logger()
	if token, found, err := s.cached(ctx); err != nil || found {
		return token, err
	}

	// Every module sharing the funded account uses this Redis lock. It prevents
	// concurrent cache-miss logins from invalidating each other's KTMB session.
	lockKey := s.key + ":login-lock"
	owner := uuid.NewString()
	for {
		acquired, err := s.main.SetNX(ctx, lockKey, owner, 90*time.Second).Result()
		if err != nil {
			return "", err
		}
		if acquired {
			break
		}
		if token, found, err := s.cached(ctx); err != nil || found {
			return token, err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	defer s.releaseLock(context.WithoutCancel(ctx), lockKey, owner)

	// A lock waiter may have filled the cache immediately before we acquired it.
	if token, found, err := s.cached(ctx); err != nil || found {
		return token, err
	}
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

func (s *Session) cached(ctx context.Context) (string, bool, error) {
	encrypted, err := s.main.Get(ctx, s.key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	token, err := s.encryptor.Decrypt(encrypted)
	return token, err == nil, err
}

func (s *Session) releaseLock(ctx context.Context, lockKey, owner string) {
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`
	if err := s.main.Eval(ctx, script, []string{lockKey}, owner).Err(); err != nil {
		s.logger.Error().Err(err).Msg("Failed to release KTMB login lock")
	}
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
