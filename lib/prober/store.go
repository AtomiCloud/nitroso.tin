package prober

import (
	"context"
	"encoding/json"
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
// spawner and prober Jobs. Probers call Load only; only the spawner may Ensure.
type Store struct {
	redis     *otelredis.OtelRedis
	session   *session.Session
	finder    *enricher.Client
	encryptor encryptor.Encryptor[enricher.FindStore]
	config    config.EnricherConfig
	logger    *zerolog.Logger
}

func NewStore(redis *otelredis.OtelRedis, session *session.Session, finder *enricher.Client,
	encr encryptor.Encryptor[enricher.FindStore], cfg config.EnricherConfig, logger *zerolog.Logger) *Store {
	return &Store{redis: redis, session: session, finder: finder, encryptor: encr, config: cfg, logger: logger}
}

func (s *Store) Load(ctx context.Context) (string, enricher.FindStore, error) {
	userDataEncrypted, err := s.redis.Get(ctx, s.config.UserDataKey).Result()
	if err != nil {
		return "", nil, fmt.Errorf("read cached userData: %w", err)
	}
	userData, err := s.encryptor.Decrypt(userDataEncrypted)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt cached userData: %w", err)
	}
	storeEncrypted, err := s.redis.Get(ctx, s.config.StoreKey).Result()
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
	if encrypted, getErr := s.redis.Get(ctx, s.config.StoreKey).Result(); getErr == nil {
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
		if existing := findSlot(cached, target); existing.TripData != "" {
			mu.Lock()
			setSlot(store, target, existing)
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(target Target) {
			defer wg.Done()
			found, findErr := s.finder.Find(userData, target.Direction, lib.ZincToHeliumDate(target.Date), target.Time)
			if findErr != nil {
				s.logger.Error().Err(findErr).Str("slot", target.Key()).Msg("Failed to seed prober slot")
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
	pipe := s.redis.TxPipeline()
	pipe.Set(ctx, s.config.UserDataKey, userDataEncrypted, 0)
	pipe.Set(ctx, s.config.StoreKey, storeEncrypted, 0)
	if _, err = pipe.Exec(ctx); err != nil {
		return fmt.Errorf("write prober cache: %w", err)
	}
	return nil
}

func (s *Store) Refresh(ctx context.Context, userData string, target Target) (enricher.FindRes, error) {
	found, err := s.finder.Find(userData, target.Direction, lib.ZincToHeliumDate(target.Date), target.Time)
	if err != nil {
		return enricher.FindRes{}, err
	}
	return found, nil
}

func findSlot(store enricher.FindStore, target Target) enricher.FindRes {
	if store[target.Direction] == nil || store[target.Direction][target.Date] == nil {
		return enricher.FindRes{}
	}
	return store[target.Direction][target.Date][target.Time]
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
