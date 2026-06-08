package poller

import (
	"context"
	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/rs/zerolog"
	"sort"
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
	maxStreams  int
}

func NewPoller(channel chan string, job *HeliumJobCreator, trigger *Trigger, logger *zerolog.Logger, psm, ps string,
	countReader *count.Client, maxStreams int) *Poller {
	return &Poller{
		channel:     channel,
		trigger:     trigger,
		logger:      logger,
		job:         job,
		countReader: countReader,
		psm:         psm,
		ps:          ps,
		maxStreams:  maxStreams,
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

	// Cap total streams to avoid overloading the system: sort targets by date
	// (earliest first) and keep the first maxStreams. When there are fewer than
	// the cap, all are kept (even if they extend past ~3 weeks).
	jobs = p.capStreams(ctx, jobs)

	p.logger.Info().Any("jobs", jobs).Ctx(ctx).Msgf("Create %d jobs", len(jobs))
	er := p.job.CreateMultiJob(ctx, jobs)
	if er != nil {
		p.logger.Error().Ctx(ctx).Err(er).Msg("Failed to create multi job")
		return er
	}
	return nil
}

// capStreams sorts targets by date ascending (then direction) and keeps the
// first maxStreams. maxStreams <= 0 means no cap.
func (p *Poller) capStreams(ctx context.Context, jobs []HeliumJob) []HeliumJob {
	sort.SliceStable(jobs, func(i, j int) bool {
		ti, tj := parsePollDate(jobs[i].Date), parsePollDate(jobs[j].Date)
		if ti.Equal(tj) {
			return jobs[i].From < jobs[j].From
		}
		return ti.Before(tj)
	})

	if p.maxStreams > 0 && len(jobs) > p.maxStreams {
		p.logger.Info().Ctx(ctx).
			Int("total", len(jobs)).
			Int("cap", p.maxStreams).
			Msg("Capping streams to earliest dates")
		jobs = jobs[:p.maxStreams]
	}
	return jobs
}

// parsePollDate parses a "dd-mm-yyyy" target date for chronological sorting.
// Unparseable dates sort first (zero time) so they are not silently dropped.
func parsePollDate(d string) time.Time {
	t, err := time.Parse("02-01-2006", d)
	if err != nil {
		return time.Time{}
	}
	return t
}
