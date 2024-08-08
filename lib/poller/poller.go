package poller

import (
	"context"
	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/rs/zerolog"
	"time"
)

type Poller struct {
	channel     chan string
	job         *HeliumJobCreator
	trigger     *Trigger
	logger      *zerolog.Logger
	psm         string
	ps          string
	countReader *count.Client
}

func NewPoller(channel chan string, job *HeliumJobCreator, trigger *Trigger, logger *zerolog.Logger, psm, ps string,
	countReader *count.Client) *Poller {
	return &Poller{
		channel:     channel,
		trigger:     trigger,
		logger:      logger,
		job:         job,
		countReader: countReader,
		psm:         psm,
		ps:          ps,
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

	exists, counts, err := p.countReader.GetPollerCount(ctx, time.Now())

	if !exists {
		p.logger.Info().Ctx(ctx).Msg("Key does not exist")
		return nil
	}

	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to get counts")
		return err
	}

	jobs := make([]HeliumJob, 0)

	for dir, dirCount := range counts {

		for date := range dirCount {
			jobs = append(jobs, HeliumJob{
				Date: date,
				From: dir,
			})
		}
	}

	p.logger.Info().Any("jobs", jobs).Ctx(ctx).Msgf("Create %d jobs", len(jobs))
	er := p.job.CreateMultiJob(ctx, jobs)
	if er != nil {
		p.logger.Error().Ctx(ctx).Err(er).Msg("Failed to create multi job")
		return er
	}
	return nil
}
