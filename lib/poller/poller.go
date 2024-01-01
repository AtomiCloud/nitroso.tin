package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/rs/zerolog"
)

type Poller struct {
	channel chan string
	job     *HeliumJobCreator
	trigger *Trigger
	logger  *zerolog.Logger
	redis   *otelredis.OtelRedis
	psd     string
}

func NewPoller(channel chan string, rds *otelredis.OtelRedis, job *HeliumJobCreator, trigger *Trigger, logger *zerolog.Logger, psd string) *Poller {
	return &Poller{
		channel: channel,
		trigger: trigger,
		logger:  logger,
		job:     job,
		redis:   rds,
		psd:     psd,
	}
}

func (p *Poller) Start(ctx context.Context, uniqueId string) error {
	p.logger.Info().Ctx(ctx).Msg("Starting Cron Poller Trigger")
	p.trigger.Cron(ctx)

	p.logger.Info().Ctx(ctx).Msg("Starting RedisStream Poller Trigger")

	go func() {
		err := p.trigger.RedisStream(ctx, uniqueId)
		if err != nil {
			p.logger.Fatal().Ctx(ctx).Err(err).Msg("RedisStream Poller Trigger Failed")
			panic(err)
		}
	}()

	for {
		t := <-p.channel

		p.logger.Info().Ctx(ctx).Msgf("Triggered: %s", t)

		err := p.createPoller(ctx)
		if err != nil {
			p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to create poller")
			return err
		}

	}
}

func (p *Poller) createPoller(ctx context.Context) error {

	key := fmt.Sprintf("%s:%s", p.psd, "count")

	exists, err := p.redis.Exists(ctx, key).Result()
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to check if key exists")
		return err
	}

	if exists == 0 {
		p.logger.Info().Ctx(ctx).Msgf("Key '%s' does not exist", key)
		return nil
	}

	// getting count from Redis
	p.logger.Info().Ctx(ctx).Msgf("Getting counts from redis '%s'", key)
	countsJson, err := p.redis.Get(ctx, key).Result()
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to get counts")
		return err
	}

	var counts map[string]map[string]map[string]int
	err = json.Unmarshal([]byte(countsJson), &counts)
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to unmarshal counts")
		return err
	}

	for dir, dirCount := range counts {

		for date := range dirCount {
			p.logger.Info().Ctx(ctx).Msgf("dir: %s, date: %s", dir, date)
			er := p.job.CreateJob(ctx, date, dir)
			if er != nil {
				p.logger.Error().Ctx(ctx).Err(er).Msg("Failed to create job")
				return er
			}
		}
	}
	return nil
}
