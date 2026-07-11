package prober

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Spawner struct {
	counts *count.Client
	store  *Store
	jobs   *JobCreator
	redis  *otelredis.OtelRedis
	config config.ProberConfig
	ps     string
	logger *zerolog.Logger
	now    func() time.Time
}

func NewSpawner(counts *count.Client, store *Store, jobs *JobCreator, redis *otelredis.OtelRedis,
	cfg config.ProberConfig, ps string, logger *zerolog.Logger) *Spawner {
	return &Spawner{counts: counts, store: store, jobs: jobs, redis: redis, config: cfg, ps: ps, logger: logger, now: time.Now}
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
			return ctx.Err()
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
	s.logPreviousTally(ctx, epoch-1)

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
	if err := s.store.Ensure(ctx, targets); err != nil {
		return err
	}
	shards := ShardTargets(targets, s.config.SlotsPerJob)
	for shard, targets := range shards {
		for fanout := 0; fanout < s.config.Fanout; fanout++ {
			if err := s.jobs.Create(ctx, epoch, shard, fanout, targets); err != nil {
				return err
			}
		}
	}
	s.logger.Info().Int64("epoch", epoch).Int("slots", len(targets)).Int("shards", len(shards)).
		Int("fanout", s.config.Fanout).Int("jobs", len(shards)*s.config.Fanout).Msg("Spawned prober epoch")
	return nil
}

func (s *Spawner) logPreviousTally(ctx context.Context, epoch int64) {
	values, err := s.redis.LRange(ctx, TallyKey(s.ps, epoch), 0, -1).Result()
	if err != nil && err != redis.Nil {
		s.logger.Error().Err(err).Int64("epoch", epoch).Msg("Failed to read previous prober tally")
		return
	}
	if len(values) == 0 {
		s.logger.Warn().Int64("epoch", epoch).Msg("Previous prober epoch has no tallies")
		return
	}
	var polls, holds, rateLimited int64
	for _, value := range values {
		var tally JobTally
		if json.Unmarshal([]byte(value), &tally) == nil {
			polls += tally.Total.Polls
			holds += tally.Total.Holds
			rateLimited += tally.Total.RateLimited
		}
	}
	event := s.logger.Info()
	if polls == 0 {
		event = s.logger.Error()
	}
	event.Int64("epoch", epoch).Int("jobs", len(values)).Int64("polls", polls).Int64("holds", holds).
		Int64("rateLimited", rateLimited).Msg("Previous prober epoch tally")
}
