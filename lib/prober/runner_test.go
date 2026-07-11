package prober

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/reserver"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
)

type fakeStore struct {
	userData string
	find     enricher.FindStore
	err      error
}

func (s *fakeStore) Load(context.Context) (string, enricher.FindStore, error) {
	return s.userData, s.find, s.err
}

func (s *fakeStore) Refresh(context.Context, string, Target) (enricher.FindRes, error) {
	return enricher.FindRes{}, errors.New("unexpected refresh")
}

type fakeKTMB struct {
	reserve      ktmb.GenericRes[ktmb.ReserveRes]
	reserveErr   error
	reserveCalls int
	cancelCalls  int
}

func (k *fakeKTMB) Reserve(string, string, string, string) (ktmb.GenericRes[ktmb.ReserveRes], error) {
	k.reserveCalls++
	return k.reserve, k.reserveErr
}

func (k *fakeKTMB) Cancel(string, string) (ktmb.GenericRes[*interface{}], error) {
	k.cancelCalls++
	return ktmb.GenericRes[*interface{}]{Status: true}, nil
}

type fakeReserveEncryptor struct{}

func (fakeReserveEncryptor) Encrypt(string) (string, error)                 { return "encrypted", nil }
func (fakeReserveEncryptor) Decrypt(string) (string, error)                 { return "", nil }
func (fakeReserveEncryptor) EncryptAny(reserver.ReserveDto) (string, error) { return "encrypted", nil }
func (fakeReserveEncryptor) DecryptAny(string) (reserver.ReserveDto, error) {
	return reserver.ReserveDto{}, nil
}

func testRunner(k *fakeKTMB, store cacheStore, cfg config.ProberConfig, captured *JobTally) *Runner {
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
	return runner
}

func seededStore() *fakeStore {
	return &fakeStore{userData: "secret-session", find: enricher.FindStore{
		"JToW": {"01-01-2027": {"08:30:00": {SearchData: "search", TripData: "trip"}}},
	}}
}

func TestRunnerDryRunReleasesHoldInsteadOfEnqueueing(t *testing.T) {
	k := &fakeKTMB{reserve: ktmb.GenericRes[ktmb.ReserveRes]{Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}}}
	var tally JobTally
	runner := testRunner(k, seededStore(), config.ProberConfig{DryRun: true}, &tally)
	runner.enqueue = func(context.Context, string) error {
		t.Fatal("dry-run must never enqueue a hold")
		return nil
	}
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job"); err != nil {
		t.Fatal(err)
	}
	if k.reserveCalls != 1 || k.cancelCalls != 1 || tally.Total.Holds != 1 {
		t.Fatalf("reserve=%d cancel=%d tally=%#v", k.reserveCalls, k.cancelCalls, tally.Total)
	}
}

func TestRunnerCancelsHoldWhenQueueWriteFails(t *testing.T) {
	k := &fakeKTMB{reserve: ktmb.GenericRes[ktmb.ReserveRes]{Status: true, Data: ktmb.ReserveRes{BookingData: "hold"}}}
	var tally JobTally
	runner := testRunner(k, seededStore(), config.ProberConfig{ErrorLimit: 1}, &tally)
	runner.enqueue = func(context.Context, string) error { return errors.New("redis unavailable") }
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job"); err != nil {
		t.Fatal(err)
	}
	if k.cancelCalls != 1 || tally.Total.Holds != 0 || tally.Total.Errors != 1 {
		t.Fatalf("cancel=%d tally=%#v", k.cancelCalls, tally.Total)
	}
}

func TestRunnerBailsEpochOnSessionMessage(t *testing.T) {
	k := &fakeKTMB{reserve: ktmb.GenericRes[ktmb.ReserveRes]{Messages: []string{"Session has expired"}}}
	var tally JobTally
	runner := testRunner(k, seededStore(), config.ProberConfig{SessionPatterns: []string{"session"}}, &tally)
	if err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job"); err != nil {
		t.Fatal(err)
	}
	if tally.Total.SessionDead != 1 || k.reserveCalls != 1 {
		t.Fatalf("unexpected session tally: %#v", tally.Total)
	}
}

func TestRunnerTalliesCacheMissWithoutLoggingIn(t *testing.T) {
	k := &fakeKTMB{}
	var tally JobTally
	runner := testRunner(k, &fakeStore{err: errors.New("missing cache")}, config.ProberConfig{}, &tally)
	err := runner.Run(context.Background(), []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 1}}, 4, "job")
	if err == nil || tally.Total.Skipped != 1 || k.reserveCalls != 0 {
		t.Fatalf("err=%v reserve=%d tally=%#v", err, k.reserveCalls, tally.Total)
	}
}
