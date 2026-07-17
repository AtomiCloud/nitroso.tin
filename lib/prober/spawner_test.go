package prober

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
)

type fakeCountSource struct {
	exists bool
	counts count.Count
	err    error
}

func (f *fakeCountSource) GetPollerCount(context.Context, time.Time) (bool, count.Count, error) {
	return f.exists, f.counts, f.err
}

type fakeSeedStore struct {
	targets      []Target
	err          error
	recoverCalls int
}

func (f *fakeSeedStore) RecoverSession(context.Context) error {
	f.recoverCalls++
	return nil
}

func (f *fakeSeedStore) Ensure(_ context.Context, targets []Target) error {
	f.targets = append([]Target(nil), targets...)
	return f.err
}

type createdJob struct {
	epoch         int64
	shard, fanout int
	targets       []Target
}

type fakeJobSource struct {
	created []createdJob
	failAt  int
}

func (f *fakeJobSource) Create(_ context.Context, epoch int64, shard, fanout int, targets []Target) error {
	f.created = append(f.created, createdJob{epoch: epoch, shard: shard, fanout: fanout, targets: append([]Target(nil), targets...)})
	if f.failAt > 0 && len(f.created) == f.failAt {
		return errors.New("create failed")
	}
	return nil
}

func testSpawner(counts countSource, store seedStore, jobs jobSource, cfg config.ProberConfig) (*Spawner, *int64) {
	logger := zerolog.Nop()
	readEpoch := int64(-1)
	spawner := &Spawner{counts: counts, store: store, jobs: jobs, config: cfg, logger: &logger, now: time.Now}
	spawner.readTallies = func(_ context.Context, epoch int64) ([]string, error) {
		readEpoch = epoch
		return nil, nil
	}
	spawner.readExpected = func(context.Context, int64) ([]string, error) { return nil, nil }
	spawner.writeExpected = func(context.Context, int64, []string) error { return nil }
	return spawner, &readEpoch
}

func TestMissingJobsDetectsPartialFleet(t *testing.T) {
	expected := []string{"job-a", "job-b", "job-c"}
	received := map[string]struct{}{"job-a": {}, "job-c": {}}
	missing := MissingJobs(expected, received)
	if len(missing) != 1 || missing[0] != "job-b" {
		t.Fatalf("missing = %#v, want job-b", missing)
	}
}

func TestSpawnerDoesNotAlertForNoDemandEpoch(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	spawner := &Spawner{logger: &logger}
	spawner.readExpected = func(context.Context, int64) ([]string, error) { return nil, nil }
	spawner.readTallies = func(context.Context, int64) ([]string, error) { return nil, nil }
	spawner.logPreviousTally(context.Background(), 10)
	if logs.Len() != 0 {
		t.Fatalf("no-demand epoch emitted a false alert: %s", logs.String())
	}
}

func TestSpawnerCoversAllDemandWithBreadthAndFanout(t *testing.T) {
	counts := &fakeCountSource{exists: true, counts: count.Count{
		"JToW": {"01-01-2027": {"08:00:00": 1, "09:00:00": 1, "10:00:00": 1}},
		"WToJ": {"02-01-2027": {"08:00:00": 1, "09:00:00": 1}},
	}}
	store := &fakeSeedStore{}
	jobs := &fakeJobSource{}
	spawner, readEpoch := testSpawner(counts, store, jobs, config.ProberConfig{EpochMinutes: 1, JobMinutes: 2, SlotsPerJob: 2, Fanout: 2})
	var expected []string
	spawner.writeExpected = func(_ context.Context, _ int64, names []string) error {
		expected = append([]string(nil), names...)
		return nil
	}

	if err := spawner.Spawn(context.Background(), time.Unix(600, 0)); err != nil {
		t.Fatal(err)
	}
	if *readEpoch != 7 {
		t.Fatalf("read tally epoch %d, want completed epoch 7", *readEpoch)
	}
	if store.recoverCalls != 1 || len(store.targets) != 5 || len(jobs.created) != 6 || len(expected) != 6 {
		t.Fatalf("recovery=%d seeded=%d jobs=%d expected=%d", store.recoverCalls, len(store.targets), len(jobs.created), len(expected))
	}
	coverage := map[string]int{}
	for _, job := range jobs.created {
		if job.epoch != 10 || len(job.targets) > 2 {
			t.Fatalf("unexpected job: %#v", job)
		}
		for _, target := range job.targets {
			coverage[target.Key()]++
		}
	}
	for _, target := range store.targets {
		if coverage[target.Key()] != 2 {
			t.Fatalf("slot %s coverage=%d, want fanout 2", target.Key(), coverage[target.Key()])
		}
	}
}

func TestSpawnerHandlesNoDemandAndCreationFailure(t *testing.T) {
	t.Run("no snapshot", func(t *testing.T) {
		store, jobs := &fakeSeedStore{}, &fakeJobSource{}
		spawner, _ := testSpawner(&fakeCountSource{}, store, jobs, config.ProberConfig{EpochMinutes: 1, JobMinutes: 2, SlotsPerJob: 2, Fanout: 1})
		if err := spawner.Spawn(context.Background(), time.Unix(600, 0)); err != nil {
			t.Fatal(err)
		}
		if len(store.targets) != 0 || len(jobs.created) != 0 {
			t.Fatal("no demand must not seed or create Jobs")
		}
	})

	t.Run("partial creation failure", func(t *testing.T) {
		counts := &fakeCountSource{exists: true, counts: count.Count{"JToW": {"01-01-2027": {"08:00:00": 1, "09:00:00": 1, "10:00:00": 1}}}}
		jobs := &fakeJobSource{failAt: 2}
		spawner, _ := testSpawner(counts, &fakeSeedStore{}, jobs, config.ProberConfig{EpochMinutes: 1, JobMinutes: 2, SlotsPerJob: 1, Fanout: 1})
		if err := spawner.Spawn(context.Background(), time.Unix(600, 0)); err == nil {
			t.Fatal("expected creation error")
		}
		if len(jobs.created) != 3 {
			t.Fatalf("created %d Jobs; later shards were not attempted", len(jobs.created))
		}
	})
}

func TestSpawnerRejectsInvalidFleetConfig(t *testing.T) {
	spawner, _ := testSpawner(&fakeCountSource{}, &fakeSeedStore{}, &fakeJobSource{}, config.ProberConfig{})
	if err := spawner.Spawn(context.Background(), time.Now()); err == nil {
		t.Fatal("expected invalid config error")
	}
}

func TestSpawnerTreatsRequestedShutdownAsSuccess(t *testing.T) {
	spawner, _ := testSpawner(&fakeCountSource{}, &fakeSeedStore{}, &fakeJobSource{}, config.ProberConfig{
		EpochMinutes: 1, JobMinutes: 2, SlotsPerJob: 1, Fanout: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := spawner.Start(ctx); err != nil {
		t.Fatalf("requested shutdown returned %v", err)
	}
}
