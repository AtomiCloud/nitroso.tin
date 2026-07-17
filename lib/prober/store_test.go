package prober

import (
	"context"
	"errors"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type memoryStoreCache struct {
	values               map[string]string
	signals              map[string]map[string]struct{}
	writeErr             error
	replaceBeforeCompare string
}

func (c *memoryStoreCache) Get(_ context.Context, key string) (string, error) {
	value, ok := c.values[key]
	if !ok {
		return "", redis.Nil
	}
	return value, nil
}

func (c *memoryStoreCache) SetPair(_ context.Context, firstKey, firstValue, secondKey, secondValue string) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	c.values[firstKey] = firstValue
	c.values[secondKey] = secondValue
	return nil
}

func (c *memoryStoreCache) SessionDeathFingerprints(_ context.Context, signalKey string) ([]string, error) {
	var values []string
	for value := range c.signals[signalKey] {
		values = append(values, value)
	}
	return values, nil
}

func (c *memoryStoreCache) RemoveSessionDeathFingerprints(_ context.Context, signalKey string, fingerprints []string) error {
	for _, fingerprint := range fingerprints {
		delete(c.signals[signalKey], fingerprint)
	}
	return nil
}

func (c *memoryStoreCache) CompareAndDeleteSession(_ context.Context, signalKey, loginKey, userDataKey, expectedEncrypted string) (bool, error) {
	if c.replaceBeforeCompare != "" {
		c.values[loginKey] = c.replaceBeforeCompare
		c.replaceBeforeCompare = ""
	}
	if c.values[loginKey] != expectedEncrypted {
		return false, nil
	}
	delete(c.values, loginKey)
	delete(c.values, userDataKey)
	delete(c.signals, signalKey)
	return true, nil
}

func TestStoreUsesMatchingSignalWhenOldAndCurrentGenerationsAccumulate(t *testing.T) {
	cache := &memoryStoreCache{values: map[string]string{}, signals: map[string]map[string]struct{}{}}
	store, encr := newTestStore(t, cache, &fakeSessionProvider{}, &fakeFinderProvider{})
	cache.signals["session-dead"] = map[string]struct{}{
		SessionFingerprint("old-token"):     {},
		SessionFingerprint("current-token"): {},
	}
	cache.values["login"], _ = encr.Encrypt("current-token")
	cache.values["user"], _ = encr.Encrypt("current-token")
	if err := store.RecoverSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, exists := cache.values["login"]; exists {
		t.Fatal("matching current-token signal was lost behind an old-token signal")
	}
}

func TestStoreAtomicComparePreservesSessionInstalledDuringRecovery(t *testing.T) {
	cache := &memoryStoreCache{values: map[string]string{}, signals: map[string]map[string]struct{}{}}
	store, encr := newTestStore(t, cache, &fakeSessionProvider{}, &fakeFinderProvider{})
	cache.signals["session-dead"] = map[string]struct{}{SessionFingerprint("old-token"): {}}
	cache.values["login"], _ = encr.Encrypt("old-token")
	cache.values["user"], _ = encr.Encrypt("old-token")
	newEncrypted, _ := encr.Encrypt("new-token")
	cache.replaceBeforeCompare = newEncrypted
	if err := store.RecoverSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cache.values["login"] != newEncrypted {
		t.Fatal("session installed during recovery was deleted")
	}
}

type fakeSessionProvider struct {
	userData string
	err      error
	calls    int
}

func (s *fakeSessionProvider) Login(context.Context, string, string) (string, error) {
	s.calls++
	return s.userData, s.err
}

type fakeFinderProvider struct {
	result enricher.FindRes
	err    error
	calls  int
}

func (f *fakeFinderProvider) FindContext(context.Context, string, string, string, string) (enricher.FindRes, error) {
	f.calls++
	return f.result, f.err
}

func newTestStore(t *testing.T, cache *memoryStoreCache, session *fakeSessionProvider, finder *fakeFinderProvider) (*Store, encryptor.Encryptor[enricher.FindStore]) {
	t.Helper()
	logger := zerolog.Nop()
	encr := encryptor.NewSymEncryptor[enricher.FindStore]("0123456789abcdef0123456789abcdef", &logger)
	return &Store{
		redis: cache, session: session, finder: finder, encryptor: encr,
		config:   config.EnricherConfig{Email: "funded@example.com", Password: "pw", UserDataKey: "user", StoreKey: "store"},
		loginKey: "login", sessionDeadKey: "session-dead", logger: &logger,
	}, encr
}

func TestStoreConsumesSessionDeathBeforeNextEnsure(t *testing.T) {
	cache := &memoryStoreCache{values: map[string]string{}, signals: map[string]map[string]struct{}{}}
	store, encr := newTestStore(t, cache, &fakeSessionProvider{userData: "fresh-session"}, &fakeFinderProvider{})
	cache.signals["session-dead"] = map[string]struct{}{SessionFingerprint("dead-token"): {}}
	cache.values["login"], _ = encr.Encrypt("dead-token")
	cache.values["user"], _ = encr.Encrypt("dead-token")
	if err := store.RecoverSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"login", "user"} {
		if _, exists := cache.values[key]; exists {
			t.Fatalf("%s was not invalidated", key)
		}
	}
}

func TestStoreIgnoresLateSignalFromOlderSessionGeneration(t *testing.T) {
	cache := &memoryStoreCache{values: map[string]string{}, signals: map[string]map[string]struct{}{}}
	store, encr := newTestStore(t, cache, &fakeSessionProvider{}, &fakeFinderProvider{})
	cache.signals["session-dead"] = map[string]struct{}{SessionFingerprint("old-token"): {}}
	cache.values["login"], _ = encr.Encrypt("new-token")
	cache.values["user"], _ = encr.Encrypt("new-token")
	if err := store.RecoverSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, exists := cache.values["login"]; !exists {
		t.Fatal("late old-token signal deleted the healthy new session")
	}
}

func TestStoreEnsureReusesDemandedCacheAndPrunesOldSlots(t *testing.T) {
	cache := &memoryStoreCache{values: map[string]string{}}
	session := &fakeSessionProvider{userData: "session"}
	finder := &fakeFinderProvider{}
	store, encr := newTestStore(t, cache, session, finder)
	cached := enricher.FindStore{
		"JToW": {
			"01-01-2027": {"08:30:00": {SearchData: "keep-search", TripData: "keep-trip"}},
			"02-01-2027": {"09:30:00": {SearchData: "drop-search", TripData: "drop-trip"}},
		},
	}
	cache.values["store"], _ = encr.EncryptAny(cached)

	if err := store.Ensure(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}); err != nil {
		t.Fatal(err)
	}
	written, err := encr.DecryptAny(cache.values["store"])
	if err != nil {
		t.Fatal(err)
	}
	if finder.calls != 0 || countSlotsForTest(written) != 1 || findSlot(written, Target{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00"}).TripData != "keep-trip" {
		t.Fatalf("finder=%d written=%#v", finder.calls, written)
	}
}

func TestStoreEnsureEnrichesMissingAndUnreadableCache(t *testing.T) {
	cache := &memoryStoreCache{values: map[string]string{"store": "not-encrypted"}}
	session := &fakeSessionProvider{userData: "session"}
	finder := &fakeFinderProvider{result: enricher.FindRes{SearchData: "new-search", TripData: "new-trip"}}
	store, encr := newTestStore(t, cache, session, finder)
	target := Target{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}

	if err := store.Ensure(context.Background(), []Target{target}); err != nil {
		t.Fatal(err)
	}
	written, err := encr.DecryptAny(cache.values["store"])
	if err != nil {
		t.Fatal(err)
	}
	userData, err := encr.Decrypt(cache.values["user"])
	if err != nil || userData != "session" || finder.calls != 1 || findSlot(written, target).TripData != "new-trip" {
		t.Fatalf("user=%q finder=%d written=%#v err=%v", userData, finder.calls, written, err)
	}
}

func TestStoreEnsureRequiresCompleteSearchAndTripData(t *testing.T) {
	target := Target{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}

	t.Run("refreshes incomplete cache", func(t *testing.T) {
		cache := &memoryStoreCache{values: map[string]string{}}
		finder := &fakeFinderProvider{result: enricher.FindRes{SearchData: "fresh-search", TripData: "fresh-trip"}}
		store, encr := newTestStore(t, cache, &fakeSessionProvider{userData: "session"}, finder)
		cached := enricher.FindStore{"JToW": {"01-01-2027": {"08:30:00": {TripData: "cached-trip"}}}}
		cache.values["store"], _ = encr.EncryptAny(cached)

		if err := store.Ensure(context.Background(), []Target{target}); err != nil {
			t.Fatal(err)
		}
		written, err := encr.DecryptAny(cache.values["store"])
		if err != nil {
			t.Fatal(err)
		}
		if finder.calls != 1 || !completeFind(findSlot(written, target)) {
			t.Fatalf("finder=%d written=%#v", finder.calls, written)
		}
	})

	t.Run("does not cache incomplete enrichment", func(t *testing.T) {
		cache := &memoryStoreCache{values: map[string]string{}}
		finder := &fakeFinderProvider{result: enricher.FindRes{TripData: "trip-only"}}
		store, encr := newTestStore(t, cache, &fakeSessionProvider{userData: "session"}, finder)

		if err := store.Ensure(context.Background(), []Target{target}); err != nil {
			t.Fatal(err)
		}
		written, err := encr.DecryptAny(cache.values["store"])
		if err != nil {
			t.Fatal(err)
		}
		if finder.calls != 1 || countSlotsForTest(written) != 0 {
			t.Fatalf("finder=%d written=%#v", finder.calls, written)
		}
	})
}

func TestStoreEnsurePropagatesLoginAndWriteFailures(t *testing.T) {
	tests := []struct {
		name       string
		sessionErr error
		writeErr   error
	}{
		{name: "login", sessionErr: errors.New("login failed")},
		{name: "write", writeErr: errors.New("redis failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &memoryStoreCache{values: map[string]string{}, writeErr: tt.writeErr}
			store, _ := newTestStore(t, cache, &fakeSessionProvider{userData: "session", err: tt.sessionErr},
				&fakeFinderProvider{result: enricher.FindRes{SearchData: "search", TripData: "trip"}})
			err := store.Ensure(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}})
			if err == nil {
				t.Fatal("expected failure")
			}
		})
	}
}

func countSlotsForTest(store enricher.FindStore) int {
	total := 0
	for _, dates := range store {
		for _, times := range dates {
			total += len(times)
		}
	}
	return total
}
