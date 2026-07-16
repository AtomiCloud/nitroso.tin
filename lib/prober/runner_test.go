package prober

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/reserver"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
)

type fakeStore struct {
	userData     string
	find         enricher.FindStore
	err          error
	refresh      enricher.FindRes
	refreshErr   error
	refreshCalls int
}

func (s *fakeStore) Load(context.Context) (string, enricher.FindStore, error) {
	return s.userData, s.find, s.err
}

func (s *fakeStore) Refresh(context.Context, string, Target) (enricher.FindRes, error) {
	s.refreshCalls++
	return s.refresh, s.refreshErr
}

type fakeKTMB struct {
	mu               sync.Mutex
	reserve          ktmb.GenericRes[ktmb.ReserveRes]
	reserveErr       error
	reserveResponses []ktmb.GenericRes[ktmb.ReserveRes]
	reserveErrors    []error
	reserveCalls     int
	cancel           ktmb.GenericRes[*interface{}]
	cancelErr        error
	cancelCalls      int
}

func (k *fakeKTMB) ReserveContext(context.Context, string, string, string, string) (ktmb.GenericRes[ktmb.ReserveRes], error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.reserveCalls++
	if len(k.reserveResponses) > 0 || len(k.reserveErrors) > 0 {
		var response ktmb.GenericRes[ktmb.ReserveRes]
		var err error
		if len(k.reserveResponses) > 0 {
			response = k.reserveResponses[0]
			k.reserveResponses = k.reserveResponses[1:]
		}
		if len(k.reserveErrors) > 0 {
			err = k.reserveErrors[0]
			k.reserveErrors = k.reserveErrors[1:]
		}
		return response, err
	}
	return k.reserve, k.reserveErr
}

func (k *fakeKTMB) CancelContext(context.Context, string, string) (ktmb.GenericRes[*interface{}], error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cancelCalls++
	if k.cancelErr != nil || !k.cancel.Status {
		return k.cancel, k.cancelErr
	}
	return k.cancel, nil
}

type fakeReserveEncryptor struct{ decryptErr error }

func (fakeReserveEncryptor) Encrypt(string) (string, error)                 { return "encrypted", nil }
func (fakeReserveEncryptor) Decrypt(string) (string, error)                 { return "", nil }
func (fakeReserveEncryptor) EncryptAny(reserver.ReserveDto) (string, error) { return "encrypted", nil }
func (f fakeReserveEncryptor) DecryptAny(string) (reserver.ReserveDto, error) {
	return reserver.ReserveDto{UserData: "session", BookingData: "pending-hold", Direction: "JToW", Date: "01-01-2027", Time: "08:30:00"}, f.decryptErr
}

func testRunner(k *fakeKTMB, store probeStore, cfg config.ProberConfig, captured *JobTally) *Runner {
	logger := zerolog.Nop()
	runner := &Runner{
		ktmb: k, store: store, encryptor: fakeReserveEncryptor{}, config: cfg,
		location: time.UTC, logger: &logger,
	}
	runner.writeTally = func(_ context.Context, tally JobTally) error {
		*captured = tally
		return nil
	}
	runner.enqueue = func(context.Context, string) error { return nil }
	runner.persistRelease = func(context.Context, string) error { return nil }
	runner.listReleases = func(context.Context, int) ([]string, error) { return nil, nil }
	runner.removeRelease = func(context.Context, string) error { return nil }
	runner.signalSessionDead = func(context.Context, string) error { return nil }
	return runner
}

func seededStore() *fakeStore {
	return &fakeStore{userData: "secret-session", find: enricher.FindStore{
		"JToW": {"01-01-2027": {"08:30:00": {SearchData: "search", TripData: "trip"}}},
	}}
}

func TestRunnerDryRunReleasesHoldInsteadOfEnqueueing(t *testing.T) {
	k := &fakeKTMB{reserve: ktmb.GenericRes[ktmb.ReserveRes]{Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}}, cancel: ktmb.GenericRes[*interface{}]{Status: true}}
	var tally JobTally
	runner := testRunner(k, seededStore(), config.ProberConfig{DryRun: true}, &tally)
	runner.enqueue = func(context.Context, string) error {
		t.Fatal("dry-run must never enqueue a hold")
		return nil
	}
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if k.reserveCalls != 1 || k.cancelCalls != 1 || tally.Total.Holds != 1 {
		t.Fatalf("reserve=%d cancel=%d tally=%#v", k.reserveCalls, k.cancelCalls, tally.Total)
	}
}

func TestRunnerCancelsHoldWhenQueueWriteFails(t *testing.T) {
	k := &fakeKTMB{reserve: ktmb.GenericRes[ktmb.ReserveRes]{Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}}, cancel: ktmb.GenericRes[*interface{}]{Status: true}}
	var tally JobTally
	runner := testRunner(k, seededStore(), config.ProberConfig{ErrorLimit: 1}, &tally)
	runner.enqueue = func(context.Context, string) error { return errors.New("redis unavailable") }
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if k.cancelCalls != 1 || tally.Total.Holds != 1 || tally.Total.Errors != 1 {
		t.Fatalf("cancel=%d tally=%#v", k.cancelCalls, tally.Total)
	}
}

func TestRunnerPersistsHoldWhenImmediateCancellationFails(t *testing.T) {
	k := &fakeKTMB{
		reserve: ktmb.GenericRes[ktmb.ReserveRes]{Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}},
		cancel:  ktmb.GenericRes[*interface{}]{Status: false, Messages: []string{"temporary failure"}},
	}
	var tally JobTally
	runner := testRunner(k, seededStore(), config.ProberConfig{DryRun: true}, &tally)
	persisted := ""
	runner.persistRelease = func(_ context.Context, encrypted string) error { persisted = encrypted; return nil }
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if persisted == "" || tally.Total.ReleaseFailed != 1 || tally.Total.Holds != 1 {
		t.Fatalf("persisted=%q tally=%#v", persisted, tally.Total)
	}
}

func TestRunnerResponseStateMachine(t *testing.T) {
	tests := []struct {
		name       string
		responses  []ktmb.GenericRes[ktmb.ReserveRes]
		cfg        config.ProberConfig
		wantPolls  int64
		wantSold   int64
		wantRate   int64
		wantErrors int64
	}{
		{name: "sold out then hold", responses: []ktmb.GenericRes[ktmb.ReserveRes]{{Messages: []string{"Sold Out"}}, {Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}}}, cfg: config.ProberConfig{DryRun: true, SoldOutPatterns: []string{"sold out"}, PaceMs: 1}, wantPolls: 2, wantSold: 1},
		{name: "rate limited remains uncapped", responses: []ktmb.GenericRes[ktmb.ReserveRes]{{Messages: []string{"Too Many Requests"}}, {Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}}}, cfg: config.ProberConfig{DryRun: true, RateLimitPatterns: []string{"too many requests"}}, wantPolls: 2, wantRate: 1},
		{name: "unknown stops at error limit", responses: []ktmb.GenericRes[ktmb.ReserveRes]{{Messages: []string{"mystery one"}}, {Messages: []string{"mystery two"}}}, cfg: config.ProberConfig{ErrorLimit: 2, ErrorBackoffMs: 1}, wantPolls: 2, wantErrors: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &fakeKTMB{reserveResponses: tt.responses, cancel: ktmb.GenericRes[*interface{}]{Status: true}}
			var tally JobTally
			runner := testRunner(k, seededStore(), tt.cfg, &tally)
			if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if tally.Total.Polls != tt.wantPolls || tally.Total.SoldOut != tt.wantSold || tally.Total.RateLimited != tt.wantRate || tally.Total.Errors != tt.wantErrors {
				t.Fatalf("unexpected tally: %#v", tally.Total)
			}
		})
	}
}

func TestRunnerRefreshesStaleDataOnce(t *testing.T) {
	store := seededStore()
	store.refresh = enricher.FindRes{SearchData: "fresh-search", TripData: "fresh-trip"}
	k := &fakeKTMB{reserveResponses: []ktmb.GenericRes[ktmb.ReserveRes]{{Messages: []string{"trip data expired"}}, {Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}}}, cancel: ktmb.GenericRes[*interface{}]{Status: true}}
	var tally JobTally
	runner := testRunner(k, store, config.ProberConfig{DryRun: true, StaleDataPatterns: []string{"trip data"}}, &tally)
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if store.refreshCalls != 1 || tally.Total.Stale != 1 || tally.Total.Holds != 1 {
		t.Fatalf("refreshCalls=%d tally=%#v", store.refreshCalls, tally.Total)
	}
}

func TestRunnerLiveModeEnqueuesEncryptedHold(t *testing.T) {
	k := &fakeKTMB{reserve: ktmb.GenericRes[ktmb.ReserveRes]{Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}}}
	var tally JobTally
	runner := testRunner(k, seededStore(), config.ProberConfig{}, &tally)
	enqueued := ""
	runner.enqueue = func(_ context.Context, encrypted string) error { enqueued = encrypted; return nil }
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if enqueued != "encrypted" || k.cancelCalls != 0 || tally.Total.Holds != 1 {
		t.Fatalf("enqueued=%q cancel=%d tally=%#v", enqueued, k.cancelCalls, tally.Total)
	}
}

func TestRunnerRejectsSuccessWithoutBookingData(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "dry-run"}[dryRun], func(t *testing.T) {
			k := &fakeKTMB{reserve: ktmb.GenericRes[ktmb.ReserveRes]{Status: true}}
			var tally JobTally
			runner := testRunner(k, seededStore(), config.ProberConfig{DryRun: dryRun}, &tally)
			enqueued := false
			runner.enqueue = func(context.Context, string) error { enqueued = true; return nil }
			if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if enqueued || k.cancelCalls != 0 || tally.Total.Errors != 1 || tally.Total.Holds != 0 {
				t.Fatalf("enqueued=%v cancel=%d tally=%#v", enqueued, k.cancelCalls, tally.Total)
			}
		})
	}
}

func TestRunnerBailsEpochOnSessionMessage(t *testing.T) {
	k := &fakeKTMB{reserve: ktmb.GenericRes[ktmb.ReserveRes]{Messages: []string{"Session has expired"}}}
	var tally JobTally
	runner := testRunner(k, seededStore(), config.ProberConfig{SessionPatterns: []string{"session"}}, &tally)
	signaled := false
	runner.signalSessionDead = func(_ context.Context, fingerprint string) error {
		signaled = fingerprint == SessionFingerprint("secret-session")
		return nil
	}
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if tally.Total.SessionDead != 1 || k.reserveCalls != 1 || !signaled {
		t.Fatalf("unexpected session tally: %#v", tally.Total)
	}
}

func TestRunnerClassifiesTypedHTTPFailure(t *testing.T) {
	k := &fakeKTMB{reserveErr: &ktmb.HttpStatusError{StatusCode: 401}}
	var tally JobTally
	runner := testRunner(k, seededStore(), config.ProberConfig{SessionPatterns: []string{"session"}}, &tally)
	signaled := false
	runner.signalSessionDead = func(_ context.Context, fingerprint string) error {
		signaled = fingerprint == SessionFingerprint("secret-session")
		return nil
	}
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if tally.Total.SessionDead != 1 || tally.Total.Errors != 0 || !signaled {
		t.Fatalf("signaled=%v tally=%#v", signaled, tally.Total)
	}
}

func TestRunnerClassifiesTypedHTTPStateMachineBranches(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		cfg       config.ProberConfig
		wantStale int64
		wantRate  int64
		wantSold  int64
	}{
		{name: "stale", status: 409, body: "trip data expired", cfg: config.ProberConfig{DryRun: true, StaleDataPatterns: []string{"trip data"}}, wantStale: 1},
		{name: "rate", status: 429, body: "gateway throttle", cfg: config.ProberConfig{DryRun: true}, wantRate: 1},
		{name: "sold out", status: 409, body: "sold out", cfg: config.ProberConfig{DryRun: true, SoldOutPatterns: []string{"sold out"}}, wantSold: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := seededStore()
			store.refresh = enricher.FindRes{SearchData: "fresh", TripData: "fresh"}
			k := &fakeKTMB{
				reserveResponses: []ktmb.GenericRes[ktmb.ReserveRes]{{}, {Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}}},
				reserveErrors:    []error{&ktmb.HttpStatusError{StatusCode: tt.status, Body: tt.body}, nil},
				cancel:           ktmb.GenericRes[*interface{}]{Status: true},
			}
			var tally JobTally
			runner := testRunner(k, store, tt.cfg, &tally)
			if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if tally.Total.Stale != tt.wantStale || tally.Total.RateLimited != tt.wantRate || tally.Total.SoldOut != tt.wantSold || tally.Total.Holds != 1 {
				t.Fatalf("unexpected tally: %#v", tally.Total)
			}
		})
	}
}

func TestRunnerSessionFailureCancelsMultiTargetEpoch(t *testing.T) {
	store := seededStore()
	store.find["JToW"]["01-01-2027"]["09:30:00"] = enricher.FindRes{SearchData: "search-2", TripData: "trip-2"}
	k := &fakeKTMB{reserve: ktmb.GenericRes[ktmb.ReserveRes]{Messages: []string{"Session has expired"}}}
	var tally JobTally
	runner := testRunner(k, store, config.ProberConfig{SessionPatterns: []string{"session"}}, &tally)
	targets := []Target{
		{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1},
		{Direction: "JToW", Date: "01-01-2027", Time: "09:30:00", Needed: 1},
	}
	if err := runner.Run(context.Background(), targets, 4, "job", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if tally.Total.SessionDead < 1 || k.reserveCalls > len(targets) {
		t.Fatalf("session failure did not terminate epoch: calls=%d tally=%#v", k.reserveCalls, tally.Total)
	}
}

func TestRunnerTalliesCacheMissWithoutLoggingIn(t *testing.T) {
	k := &fakeKTMB{}
	var tally JobTally
	runner := testRunner(k, &fakeStore{err: errors.New("missing cache")}, config.ProberConfig{}, &tally)
	err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute))
	if err == nil || tally.Total.Skipped != 1 || k.reserveCalls != 0 {
		t.Fatalf("err=%v reserve=%d tally=%#v", err, k.reserveCalls, tally.Total)
	}
}

func TestRunnerRetriesAndRemovesDurableReleaseBeforeProbing(t *testing.T) {
	k := &fakeKTMB{cancel: ktmb.GenericRes[*interface{}]{Status: true}}
	var tally JobTally
	runner := testRunner(k, &fakeStore{err: errors.New("missing cache")}, config.ProberConfig{}, &tally)
	runner.listReleases = func(context.Context, int) ([]string, error) { return []string{"encrypted-release"}, nil }
	removed := ""
	runner.removeRelease = func(_ context.Context, encrypted string) error { removed = encrypted; return nil }
	_ = runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute))
	if k.cancelCalls != 1 || removed != "encrypted-release" {
		t.Fatalf("cancel=%d removed=%q", k.cancelCalls, removed)
	}
}

func TestRunnerBoundsDurableReleaseDrainAttempts(t *testing.T) {
	k := &fakeKTMB{cancel: ktmb.GenericRes[*interface{}]{Status: false, Messages: []string{"expired"}}}
	var tally JobTally
	runner := testRunner(k, &fakeStore{err: errors.New("missing cache")}, config.ProberConfig{ReleaseDrainLimit: 3, ReleaseDrainBudgetMs: 1000}, &tally)
	runner.listReleases = func(_ context.Context, limit int) ([]string, error) {
		if limit != 3 {
			t.Fatalf("list limit=%d, want 3", limit)
		}
		return []string{"one", "two", "three", "four", "five"}, nil
	}
	_ = runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute))
	if k.cancelCalls != 3 {
		t.Fatalf("cleanup attempts=%d, want configured limit 3", k.cancelCalls)
	}
}

func TestRunnerRemovesUnreadableAndTerminalReleaseEntries(t *testing.T) {
	tests := []struct {
		name       string
		decryptErr error
		cancel     ktmb.GenericRes[*interface{}]
		patterns   []string
	}{
		{name: "unreadable", decryptErr: errors.New("old encryption key")},
		{name: "terminal", cancel: ktmb.GenericRes[*interface{}]{Messages: []string{"booking expired"}}, patterns: []string{"expired"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &fakeKTMB{cancel: tt.cancel}
			var tally JobTally
			runner := testRunner(k, &fakeStore{err: errors.New("missing cache")}, config.ProberConfig{ReleaseTerminalPatterns: tt.patterns}, &tally)
			runner.encryptor = fakeReserveEncryptor{decryptErr: tt.decryptErr}
			runner.listReleases = func(context.Context, int) ([]string, error) { return []string{"entry"}, nil }
			removed := ""
			runner.removeRelease = func(_ context.Context, encrypted string) error { removed = encrypted; return nil }
			_ = runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job", time.Now().Add(time.Minute))
			if removed != "entry" {
				t.Fatalf("entry was not removed: %q", removed)
			}
		})
	}
}
