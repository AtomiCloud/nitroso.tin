package enricher

import (
	"context"
	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"sync"
	"time"
)

type Enricher struct {
	channel          chan TriggerMessage
	client           *Client
	mainRedis        *otelredis.OtelRedis
	streamRedis      *otelredis.OtelRedis
	countReader      *count.Client
	logger           *zerolog.Logger
	stream           config.StreamConfig
	enricher         config.EnricherConfig
	encryptor        encryptor.Encryptor[FindStore]
	trigger          *Trigger
	psm              string
	otelConfigurator *telemetry.OtelConfigurator
}

type Find struct {
	Direction string
	Date      string
	Time      string
	Data      FindRes
}

type FindStore = map[string]map[string]map[string]FindRes

func NewEnricher(client *Client, trigger *Trigger, logger *zerolog.Logger, e encryptor.Encryptor[FindStore],
	mainRedis, streamRedis *otelredis.OtelRedis, enricher config.EnricherConfig, streams config.StreamConfig, psm string,
	channel chan TriggerMessage, otelConfigurator *telemetry.OtelConfigurator, countReader *count.Client) *Enricher {
	return &Enricher{
		client:           client,
		logger:           logger,
		mainRedis:        mainRedis,
		streamRedis:      streamRedis,
		stream:           streams,
		enricher:         enricher,
		encryptor:        e,
		trigger:          trigger,
		psm:              psm,
		channel:          channel,
		otelConfigurator: otelConfigurator,
		countReader:      countReader,
	}
}

func (p *Enricher) Start(ctx context.Context, uniqueId string) error {
	p.logger.Info().Ctx(ctx).Msg("Starting Random Trigger")
	go p.trigger.RandomTrigger(ctx)

	p.logger.Info().Ctx(ctx).Msg("Starting Cron Trigger")
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
		p.logger.Info().Ctx(ctx).Msgf("Enricher triggered: %s", t.Type)
		if t.Mc == nil {
			propagator := otel.GetTextMapPropagator()
			ctx = propagator.Extract(ctx, t.Mc)
		}
		err := p.loop(ctx)
		if err != nil {
			p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to enrich")
			return err
		}

	}
}

func (p *Enricher) loop(ctx context.Context) error {
	shutdown, err := p.otelConfigurator.Configure(ctx)
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to configure telemetry")
		return err
	}
	defer func() {
		deferErr := shutdown(ctx)
		if deferErr != nil {
			panic(deferErr)
		}
	}()
	tracer := otel.Tracer(p.psm)

	ctx, span := tracer.Start(ctx, "Enricher notify reserver start")
	defer span.End()

	err = p.enrich(ctx, tracer)
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to read")
		return err
	}
	return nil
}

func (p *Enricher) enrich(ctx context.Context, tracer trace.Tracer) error {
	p.logger.Info().Msg("Enriching...")

	exist, counts, err := p.countReader.GetPollerCount(ctx, time.Now())
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Enricher failed to get count")
		return err
	}
	if !exist {
		p.logger.Info().Ctx(ctx).Msgf("Key does not exist")
		return nil
	}

	p.logger.Info().Ctx(ctx).Any("counts", counts).Msgf("Obtain counts")
	userData, err := p.client.session.Login(ctx, p.enricher.Email, p.enricher.Password)
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to login")
		return err
	}

	// 1. Pull what we already have so the first pass only fetches missing slots.
	cached := p.pullCache(ctx)

	// Partition current demand: slots missing from cache (fetch first), slots we
	// already have (seed from cache now, refresh second). Stale slots no longer
	// in demand are dropped from the new store.
	store := make(FindStore)
	missing := make([]Find, 0)
	existing := make([]Find, 0)
	for dir, dirCount := range counts {
		for date, dateCount := range dirCount {
			for t := range dateCount {
				if fr, ok := getSlot(cached, dir, date, t); ok && fr.TripData != "" {
					setSlot(store, dir, date, t, fr)
					existing = append(existing, Find{Direction: dir, Date: date, Time: t})
				} else {
					missing = append(missing, Find{Direction: dir, Date: date, Time: t})
				}
			}
		}
	}
	p.logger.Info().Ctx(ctx).Int("missing", len(missing)).Int("existing", len(existing)).
		Msg("Partitioned demand against cache")

	// Never wipe a good store on a transient empty-count read.
	if len(missing) == 0 && len(existing) == 0 {
		p.logger.Info().Ctx(ctx).Msg("No demand slots; leaving store unchanged")
		return nil
	}

	// 2. Top up the new/missing trips, then write so the reserver can act on the
	//    fresh demand immediately.
	p.fetchInto(ctx, store, userData, missing)
	if err := p.write(ctx, tracer, userData, store); err != nil {
		return err
	}
	p.logger.Info().Ctx(ctx).Int("slots", countSlots(store)).Msg("Wrote store (after top-up)")

	// 3. Rotate (refresh) the slots we already had, then write again.
	if len(existing) > 0 {
		p.fetchInto(ctx, store, userData, existing)
		if err := p.write(ctx, tracer, userData, store); err != nil {
			return err
		}
		p.logger.Info().Ctx(ctx).Int("slots", countSlots(store)).Msg("Wrote store (after rotate)")
	}

	return nil
}

// pullCache reads and decrypts the existing store. On miss or decrypt error it
// returns an empty store so enrichment proceeds from scratch.
func (p *Enricher) pullCache(ctx context.Context) FindStore {
	enc, err := p.mainRedis.Get(ctx, p.enricher.StoreKey).Result()
	if err != nil {
		p.logger.Info().Ctx(ctx).Msg("No cached store; starting fresh")
		return make(FindStore)
	}
	store, err := p.encryptor.DecryptAny(enc)
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to decrypt cached store; starting fresh")
		return make(FindStore)
	}
	return store
}

// fetchInto concurrently enriches the given slots into store. It is tolerant: a
// slot whose Find fails is logged and skipped, never aborting the run. A
// configurable delay (ms) paces request launches.
func (p *Enricher) fetchInto(ctx context.Context, store FindStore, userData string, slots []Find) {
	delay := time.Duration(p.enricher.Delay) * time.Millisecond
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, s := range slots {
		time.Sleep(delay)
		wg.Add(1)
		go func(s Find) {
			defer wg.Done()
			d := lib.ZincToHeliumDate(s.Date)
			find, e := p.client.Find(userData, s.Direction, d, s.Time)
			if e != nil {
				p.logger.Error().Ctx(ctx).Err(e).Str("dir", s.Direction).Str("date", s.Date).
					Str("time", s.Time).Msg("Failed to get find (skipping slot)")
				return
			}
			mu.Lock()
			setSlot(store, s.Direction, s.Date, s.Time, find)
			mu.Unlock()
		}(s)
	}
	wg.Wait()
}

// write encrypts and persists userData + store, then signals the reserver.
func (p *Enricher) write(ctx context.Context, tracer trace.Tracer, userData string, store FindStore) error {
	ud, err := p.encryptor.Encrypt(userData)
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to encrypt userData")
		return err
	}
	storeEn, err := p.encryptor.EncryptAny(store)
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to encrypt store")
		return err
	}
	if _, err := p.mainRedis.Set(ctx, p.enricher.UserDataKey, ud, 0).Result(); err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to set userData")
		return err
	}
	if _, err := p.mainRedis.Set(ctx, p.enricher.StoreKey, storeEn, 0).Result(); err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to set store")
		return err
	}
	if _, err := p.streamRedis.StreamAdd(ctx, tracer, p.stream.Enrich, "ping"); err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to emit enrich signal")
		return err
	}
	return nil
}

func getSlot(s FindStore, dir, date, t string) (FindRes, bool) {
	if s[dir] == nil || s[dir][date] == nil {
		return FindRes{}, false
	}
	fr, ok := s[dir][date][t]
	return fr, ok
}

func setSlot(s FindStore, dir, date, t string, fr FindRes) {
	if s[dir] == nil {
		s[dir] = map[string]map[string]FindRes{}
	}
	if s[dir][date] == nil {
		s[dir][date] = map[string]FindRes{}
	}
	s[dir][date][t] = fr
}

func countSlots(s FindStore) int {
	n := 0
	for _, dd := range s {
		for _, tt := range dd {
			n += len(tt)
		}
	}
	return n
}
