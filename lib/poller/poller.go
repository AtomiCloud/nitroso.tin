package poller

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/redis/go-redis/v9"
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

	// Release boost: dates KTMB just released (written by helium's schedule
	// updater into LIVE redis) are watched at full power on top of the
	// maxStreams window.
	liveRedis *otelredis.OtelRedis
	tiers     Tiers
}

// Tiers configures the three watch levels beyond the full-power window:
// freshly released dates (full power, up to MaxBoost extra targets) and every
// later date (Light profile, up to MaxLight targets; <= 0 disables it).
type Tiers struct {
	BoostKey string
	MaxBoost int
	MaxLight int
	Light    PollProfile
}

func NewPoller(channel chan string, job *HeliumJobCreator, trigger *Trigger, logger *zerolog.Logger, psm, ps string,
	countReader *count.Client, maxStreams int, liveRedis *otelredis.OtelRedis, tiers Tiers) *Poller {
	return &Poller{
		channel:     channel,
		trigger:     trigger,
		logger:      logger,
		job:         job,
		countReader: countReader,
		psm:         psm,
		ps:          ps,
		maxStreams:  maxStreams,
		liveRedis:   liveRedis,
		tiers:       tiers,
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

	// Three watch levels: the earliest maxStreams targets (~3 weeks) and any
	// freshly released dates at full power; every other date at the light
	// profile.
	boost := p.readBoost(ctx)
	full, light := p.selectTargets(ctx, jobs, boost, time.Now())

	p.logger.Info().Any("jobs", full).Ctx(ctx).Msgf("Create %d full-power jobs", len(full))
	if er := p.job.CreateMultiJob(ctx, full, p.job.FullProfile()); er != nil {
		p.logger.Error().Ctx(ctx).Err(er).Msg("Failed to create full-power multi job")
		return er
	}
	if len(light) > 0 {
		p.logger.Info().Ctx(ctx).Msgf("Create %d light jobs", len(light))
		if er := p.job.CreateMultiJob(ctx, light, p.tiers.Light); er != nil {
			p.logger.Error().Ctx(ctx).Err(er).Msg("Failed to create light multi job")
			return er
		}
	}
	return nil
}

// releaseBoost is the JSON helium writes at boostKey: released date
// ("dd-mm-yyyy") -> unix-seconds expiry.
type releaseBoost struct {
	Dates map[string]int64 `json:"dates"`
}

// readBoost returns the released-date boost set; empty on any problem so a
// missing or bad key only ever falls back to the normal window.
func (p *Poller) readBoost(ctx context.Context) map[string]int64 {
	if p.liveRedis == nil || p.tiers.BoostKey == "" || p.tiers.MaxBoost <= 0 {
		return nil
	}
	raw, err := p.liveRedis.Get(ctx, p.tiers.BoostKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Str("key", p.tiers.BoostKey).Msg("Failed to read release boost; using normal window")
		return nil
	}
	var b releaseBoost
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Str("key", p.tiers.BoostKey).Msg("Bad release boost JSON; using normal window")
		return nil
	}
	return b.Dates
}

// selectTargets splits targets into the full-power set and the light set.
// Full power: the earliest maxStreams targets (capStreams) plus every later
// target on an unexpired released date, earliest first, up to MaxBoost extra.
// Light: every remaining target, earliest first, up to MaxLight. Boosted pods
// are what make the cluster scale up while a newly released month is open;
// when the boost expires those dates fall back to the light set.
func (p *Poller) selectTargets(ctx context.Context, jobs []HeliumJob, boost map[string]int64, now time.Time) ([]HeliumJob, []HeliumJob) {
	window := p.capStreams(ctx, jobs)
	rest := jobs[len(window):]
	full := append(make([]HeliumJob, 0, len(window)), window...)
	light := make([]HeliumJob, 0)
	boosted := 0
	for _, j := range rest {
		if exp, ok := boost[j.Date]; ok && exp > now.Unix() && boosted < p.tiers.MaxBoost {
			full = append(full, j)
			boosted++
			continue
		}
		if p.tiers.MaxLight > 0 && len(light) < p.tiers.MaxLight {
			light = append(light, j)
		}
	}
	if len(rest) > 0 {
		p.logger.Info().Ctx(ctx).
			Int("window", len(window)).
			Int("boosted", boosted).
			Int("light", len(light)).
			Int("dropped", len(rest)-boosted-len(light)).
			Msg("Watch tiers selected")
	}
	return full, light
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
