package prober

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Spawner struct {
	counts        countSource
	store         seedStore
	jobs          jobSource
	config        config.ProberConfig
	logger        *zerolog.Logger
	now           func() time.Time
	readTallies   func(context.Context, int64) ([]string, error)
	writeExpected func(context.Context, int64, []string) error
	readExpected  func(context.Context, int64) ([]string, error)
}

type countSource interface {
	GetPollerCount(context.Context, time.Time) (bool, count.Count, error)
}

type seedStore interface {
	RecoverSession(context.Context) error
	Ensure(context.Context, []Target) error
}

type jobSource interface {
	Create(context.Context, int64, int, int, []Target) error
}

func NewSpawner(counts *count.Client, store *Store, jobs *JobCreator, redis *otelredis.OtelRedis,
	cfg config.ProberConfig, ps string, logger *zerolog.Logger) *Spawner {
	spawner := &Spawner{counts: counts, store: store, jobs: jobs, config: cfg, logger: logger, now: time.Now}
	spawner.readTallies = func(ctx context.Context, epoch int64) ([]string, error) {
		return redis.LRange(ctx, TallyKey(ps, epoch), 0, -1).Result()
	}
	spawner.writeExpected = func(ctx context.Context, epoch int64, names []string) error {
		data, err := json.Marshal(names)
		if err != nil {
			return err
		}
		return redis.Set(ctx, ExpectedKey(ps, epoch), data, 24*time.Hour).Err()
	}
	spawner.readExpected = func(ctx context.Context, epoch int64) ([]string, error) {
		data, err := redis.Get(ctx, ExpectedKey(ps, epoch)).Result()
		if err == goredis.Nil {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var names []string
		if err := json.Unmarshal([]byte(data), &names); err != nil {
			return nil, err
		}
		return names, nil
	}
	return spawner
}

func (s *Spawner) Start(ctx context.Context) error {
	epochDuration := time.Duration(s.config.EpochMinutes) * time.Minute
	if epochDuration <= 0 {
		return fmt.Errorf("prober.epochMinutes must be positive")
	}
	for {
		if err := s.Spawn(ctx, s.now()); err != nil {
			s.logger.Error().Err(err).Msg("Failed to spawn prober epoch")
		}
		now := s.now()
		next := now.Truncate(epochDuration).Add(epochDuration)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (s *Spawner) Spawn(ctx context.Context, now time.Time) error {
	epochSeconds := int64(s.config.EpochMinutes * 60)
	if epochSeconds <= 0 || s.config.JobMinutes <= 0 || s.config.SlotsPerJob <= 0 || s.config.Fanout <= 0 {
		return fmt.Errorf("invalid prober fleet config: epochMinutes, jobMinutes, slotsPerJob and fanout must be positive")
	}
	epoch := now.Unix() / epochSeconds
	// Jobs intentionally overlap. Inspect the newest epoch guaranteed to have
	// crossed its deadline, plus one tick for tally flushing.
	overlap := (s.config.JobMinutes + s.config.EpochMinutes - 1) / s.config.EpochMinutes
	s.logPreviousTally(ctx, epoch-int64(overlap)-1)

	exists, counts, err := s.counts.GetPollerCount(ctx, now)
	if err != nil {
		return err
	}
	if !exists {
		s.logger.Info().Int64("epoch", epoch).Msg("No demand snapshot for prober epoch")
		return nil
	}
	targets := TargetsFromCount(counts)
	if len(targets) == 0 {
		s.logger.Info().Int64("epoch", epoch).Msg("No demanded slots for prober epoch")
		return nil
	}
	if err := s.store.RecoverSession(ctx); err != nil {
		return err
	}
	if err := s.store.Ensure(ctx, targets); err != nil {
		return err
	}
	shards := ShardTargets(targets, s.config.SlotsPerJob)
	expected := make([]string, 0, len(shards)*s.config.Fanout)
	for shard := range shards {
		for fanout := 0; fanout < s.config.Fanout; fanout++ {
			expected = append(expected, JobName(epoch, shard, fanout))
		}
	}
	if err := s.writeExpected(ctx, epoch, expected); err != nil {
		return fmt.Errorf("persist expected prober fleet: %w", err)
	}
	var createErrors []error
	for shard, targets := range shards {
		for fanout := 0; fanout < s.config.Fanout; fanout++ {
			if err := s.jobs.Create(ctx, epoch, shard, fanout, targets); err != nil {
				createErrors = append(createErrors, err)
			}
		}
	}
	s.logger.Info().Int64("epoch", epoch).Int("slots", len(targets)).Int("shards", len(shards)).
		Int("fanout", s.config.Fanout).Int("jobs", len(shards)*s.config.Fanout).Msg("Spawned prober epoch")
	return errors.Join(createErrors...)
}

func (s *Spawner) logPreviousTally(ctx context.Context, epoch int64) {
	expected, expectedErr := s.readExpected(ctx, epoch)
	if expectedErr != nil {
		s.logger.Error().Err(expectedErr).Int64("epoch", epoch).Msg("Failed to read expected prober fleet")
	}
	values, err := s.readTallies(ctx, epoch)
	if err != nil && err != goredis.Nil {
		s.logger.Error().Err(err).Int64("epoch", epoch).Msg("Failed to read previous prober tally")
		return
	}
	if len(expected) == 0 && len(values) == 0 {
		return
	}
	if len(values) == 0 {
		s.logger.Warn().Int64("epoch", epoch).Msg("Previous prober epoch has no tallies")
	}
	var polls, holds, rateLimited int64
	received := make(map[string]struct{}, len(values))
	for _, value := range values {
		var tally JobTally
		if json.Unmarshal([]byte(value), &tally) == nil {
			received[tally.Job] = struct{}{}
			polls += tally.Total.Polls
			holds += tally.Total.Holds
			rateLimited += tally.Total.RateLimited
		}
	}
	missing := MissingJobs(expected, received)
	if len(missing) > 0 {
		s.logger.Error().Int64("epoch", epoch).Strs("missingJobs", missing).Msg("Prober epoch is missing Job tallies")
	}
	event := s.logger.Info()
	if polls == 0 {
		event = s.logger.Error()
	}
	event.Int64("epoch", epoch).Int("jobs", len(values)).Int64("polls", polls).Int64("holds", holds).
		Int64("rateLimited", rateLimited).Msg("Previous prober epoch tally")
}

func ExpectedKey(ps string, epoch int64) string {
	return fmt.Sprintf("%s:prober:expected:%d", ps, epoch)
}

func MissingJobs(expected []string, received map[string]struct{}) []string {
	missing := make([]string, 0)
	for _, name := range expected {
		if _, ok := received[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}
