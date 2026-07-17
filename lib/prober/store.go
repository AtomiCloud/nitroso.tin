package prober

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/session"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Store owns the encrypted session and trip-data references shared between the
// spawner and prober Jobs. Probers Load and may perform an in-memory Refresh;
// only the spawner mutates the shared cache through RecoverSession and Ensure.
type Store struct {
	redis          storeCache
	session        sessionProvider
	finder         finderProvider
	encryptor      encryptor.Encryptor[enricher.FindStore]
	config         config.EnricherConfig
	loginKey       string
	sessionDeadKey string
	logger         *zerolog.Logger
}

type storeCache interface {
	Get(context.Context, string) (string, error)
	SetPair(context.Context, string, string, string, string) error
	SessionDeathFingerprints(context.Context, string) ([]string, error)
	RemoveSessionDeathFingerprints(context.Context, string, []string) error
	CompareAndDeleteSession(context.Context, string, string, string, string) (bool, error)
}

type redisStoreCache struct{ redis *otelredis.OtelRedis }

func (r redisStoreCache) Get(ctx context.Context, key string) (string, error) {
	return r.redis.Get(ctx, key).Result()
}

func (r redisStoreCache) SetPair(ctx context.Context, firstKey, firstValue, secondKey, secondValue string) error {
	pipe := r.redis.TxPipeline()
	pipe.Set(ctx, firstKey, firstValue, 0)
	pipe.Set(ctx, secondKey, secondValue, 0)
	_, err := pipe.Exec(ctx)
	return err
}

func (r redisStoreCache) SessionDeathFingerprints(ctx context.Context, signalKey string) ([]string, error) {
	return r.redis.SMembers(ctx, signalKey).Result()
}

func (r redisStoreCache) RemoveSessionDeathFingerprints(ctx context.Context, signalKey string, fingerprints []string) error {
	if len(fingerprints) == 0 {
		return nil
	}
	values := make([]interface{}, len(fingerprints))
	for i, fingerprint := range fingerprints {
		values[i] = fingerprint
	}
	return r.redis.SRem(ctx, signalKey, values...).Err()
}

func (r redisStoreCache) CompareAndDeleteSession(ctx context.Context, signalKey, loginKey, userDataKey, expectedEncrypted string) (bool, error) {
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  redis.call("DEL", KEYS[1], KEYS[2], KEYS[3])
  return 1
end
return 0`
	result, err := r.redis.Eval(ctx, script, []string{loginKey, userDataKey, signalKey}, expectedEncrypted).Int()
	return result == 1, err
}

type sessionProvider interface {
	Login(context.Context, string, string) (string, error)
}

type finderProvider interface {
	FindContext(context.Context, string, string, string, string) (enricher.FindRes, error)
}

func NewStore(redis *otelredis.OtelRedis, session *session.Session, finder *enricher.Client,
	encr encryptor.Encryptor[enricher.FindStore], cfg config.EnricherConfig, loginKey, sessionDeadKey string, logger *zerolog.Logger) *Store {
	return &Store{redis: redisStoreCache{redis: redis}, session: session, finder: finder, encryptor: encr,
		config: cfg, loginKey: loginKey, sessionDeadKey: sessionDeadKey, logger: logger}
}

// RecoverSession is spawner-only. Prober Jobs merely signal session death; the
// single spawner consumes that signal and removes the shared cached token before
// Ensure performs the next cache-miss login.
func (s *Store) RecoverSession(ctx context.Context) error {
	fingerprints, err := s.redis.SessionDeathFingerprints(ctx, s.sessionDeadKey)
	if err != nil {
		return fmt.Errorf("consume dead session signal: %w", err)
	}
	if len(fingerprints) == 0 {
		return nil
	}
	encrypted, err := s.redis.Get(ctx, s.loginKey)
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read signalled KTMB session: %w", err)
	}
	current, err := s.encryptor.Decrypt(encrypted)
	if err != nil {
		return fmt.Errorf("decrypt signalled KTMB session: %w", err)
	}
	currentFingerprint := SessionFingerprint(current)
	matched := false
	for _, fingerprint := range fingerprints {
		if fingerprint == currentFingerprint {
			matched = true
			break
		}
	}
	if !matched {
		if err := s.redis.RemoveSessionDeathFingerprints(ctx, s.sessionDeadKey, fingerprints); err != nil {
			return fmt.Errorf("remove stale dead-session signals: %w", err)
		}
		s.logger.Info().Msg("Ignored stale dead-session signal from an older prober generation")
		return nil
	}
	deleted, err := s.redis.CompareAndDeleteSession(ctx, s.sessionDeadKey, s.loginKey, s.config.UserDataKey, encrypted)
	if err != nil {
		return fmt.Errorf("invalidate dead KTMB session: %w", err)
	}
	if !deleted {
		s.logger.Info().Msg("KTMB session changed during recovery; preserved the newer token")
		return nil
	}
	s.logger.Warn().Msg("Invalidated dead shared KTMB session for next epoch")
	return nil
}

func (s *Store) Load(ctx context.Context) (string, enricher.FindStore, error) {
	userDataEncrypted, err := s.redis.Get(ctx, s.config.UserDataKey)
	if err != nil {
		return "", nil, fmt.Errorf("read cached userData: %w", err)
	}
	userData, err := s.encryptor.Decrypt(userDataEncrypted)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt cached userData: %w", err)
	}
	storeEncrypted, err := s.redis.Get(ctx, s.config.StoreKey)
	if err != nil {
		return "", nil, fmt.Errorf("read cached trip store: %w", err)
	}
	store, err := s.encryptor.DecryptAny(storeEncrypted)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt cached trip store: %w", err)
	}
	return userData, store, nil
}

// Ensure logs in only through the shared cached Session, enriches missing slots,
// and atomically replaces the encrypted warm store. Existing slots are retained;
// stale-data refresh is handled by a prober on the first matching KTMB response.
func (s *Store) Ensure(ctx context.Context, targets []Target) error {
	userData, err := s.session.Login(ctx, s.config.Email, s.config.Password)
	if err != nil {
		return fmt.Errorf("ensure shared KTMB session: %w", err)
	}
	cached := make(enricher.FindStore)
	if encrypted, getErr := s.redis.Get(ctx, s.config.StoreKey); getErr == nil {
		if existing, decryptErr := s.encryptor.DecryptAny(encrypted); decryptErr == nil {
			cached = existing
		} else {
			s.logger.Error().Err(decryptErr).Msg("Ignoring unreadable prober trip store")
		}
	} else if getErr != redis.Nil {
		return fmt.Errorf("read existing trip store: %w", getErr)
	}
	// Build a demand-only store so departed or cancelled slots do not accumulate.
	store := make(enricher.FindStore)

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, target := range targets {
		if existing := findSlot(cached, target); completeFind(existing) {
			mu.Lock()
			setSlot(store, target, existing)
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(target Target) {
			defer wg.Done()
			found, findErr := s.finder.FindContext(ctx, userData, target.Direction, lib.ZincToHeliumDate(target.Date), target.Time)
			if findErr != nil {
				s.logger.Error().Err(findErr).Str("slot", target.Key()).Msg("Failed to seed prober slot")
				return
			}
			if !completeFind(found) {
				s.logger.Error().Str("slot", target.Key()).Msg("Enricher returned incomplete searchData/tripData")
				return
			}
			mu.Lock()
			setSlot(store, target, found)
			mu.Unlock()
		}(target)
	}
	wg.Wait()

	userDataEncrypted, err := s.encryptor.Encrypt(userData)
	if err != nil {
		return fmt.Errorf("encrypt userData: %w", err)
	}
	storeEncrypted, err := s.encryptor.EncryptAny(store)
	if err != nil {
		return fmt.Errorf("encrypt trip store: %w", err)
	}
	if err = s.redis.SetPair(ctx, s.config.UserDataKey, userDataEncrypted, s.config.StoreKey, storeEncrypted); err != nil {
		return fmt.Errorf("write prober cache: %w", err)
	}
	return nil
}

func (s *Store) Refresh(ctx context.Context, userData string, target Target) (enricher.FindRes, error) {
	found, err := s.finder.FindContext(ctx, userData, target.Direction, lib.ZincToHeliumDate(target.Date), target.Time)
	if err != nil {
		return enricher.FindRes{}, err
	}
	if !completeFind(found) {
		return enricher.FindRes{}, errors.New("enricher returned incomplete searchData/tripData")
	}
	return found, nil
}

func findSlot(store enricher.FindStore, target Target) enricher.FindRes {
	if store[target.Direction] == nil || store[target.Direction][target.Date] == nil {
		return enricher.FindRes{}
	}
	return store[target.Direction][target.Date][target.Time]
}

func completeFind(found enricher.FindRes) bool {
	return found.SearchData != "" && found.TripData != ""
}

func setSlot(store enricher.FindStore, target Target, found enricher.FindRes) {
	if store[target.Direction] == nil {
		store[target.Direction] = make(map[string]map[string]enricher.FindRes)
	}
	if store[target.Direction][target.Date] == nil {
		store[target.Direction][target.Date] = make(map[string]enricher.FindRes)
	}
	store[target.Direction][target.Date][target.Time] = found
}

func TallyKey(ps string, epoch int64) string {
	return fmt.Sprintf("%s:prober:tally:%d", ps, epoch)
}

func WriteTally(ctx context.Context, redis *otelredis.OtelRedis, ps string, tally JobTally) error {
	data, err := json.Marshal(tally)
	if err != nil {
		return err
	}
	pipe := redis.TxPipeline()
	key := TallyKey(ps, tally.Epoch)
	pipe.LPush(ctx, key, data)
	pipe.Expire(ctx, key, 24*time.Hour)
	_, err = pipe.Exec(ctx)
	return err
}
