package enricher

import (
	"context"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"time"
)

type Enricher struct {
	channel          chan string
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
	channel chan string, otelConfigurator *telemetry.OtelConfigurator, countReader *count.Client) *Enricher {
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
		p.logger.Info().Ctx(ctx).Msgf("Enricher triggered: %s", t)
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

	exist, counts, err := p.countReader.GetCount(ctx, time.Now())
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Enricher failed to get count")
		return err
	}

	if !exist {
		p.logger.Info().Ctx(ctx).Msgf("Key does not exist")
		return nil
	}

	login, err := p.client.ktmb.Login(p.enricher.Email, p.enricher.Password)
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to login")
		return err
	}
	if !login.Status {
		p.logger.Error().Ctx(ctx).Strs("errors", login.Messages).Msg("Failed to login")
		return fmt.Errorf("failed to login: %v", login.Messages)
	}
	userData := login.Data.UserData

	var store = make(FindStore)

	c := make(chan Find)
	errC := make(chan error)

	slots := 0
	for dir, dirCount := range counts {
		for date, dateCount := range dirCount {
			for time, _ := range dateCount {

				go func(ch chan Find, eCh chan error, dir, date, time string) {

					d := lib.ZincToHeliumDate(date)

					find, e := p.client.Find(userData, dir, d, time)
					if e != nil {
						p.logger.Error().Ctx(ctx).Err(e).
							Str("dir", dir).
							Str("date", date).
							Str("time", time).
							Msg("Failed to get find")
						eCh <- e
						return
					}
					ch <- Find{
						Direction: dir,
						Date:      date,
						Time:      time,
						Data:      find,
					}
				}(c, errC, dir, date, time)
				slots = slots + 1
			}
		}
	}

	errs := make([]error, 0)

	for i := 0; i < slots; i++ {
		select {
		case find := <-c:
			if _, ok := store[find.Direction]; !ok {
				store[find.Direction] = map[string]map[string]FindRes{}
			}
			if _, ok := store[find.Direction][find.Date]; !ok {
				store[find.Direction][find.Date] = map[string]FindRes{}
			}
			store[find.Direction][find.Date][find.Time] = find.Data
		case e := <-errC:
			p.logger.Error().Ctx(ctx).Err(e).Msg("Failed to get find")
			errs = append(errs, e)
		}
	}

	if len(errs) > 0 {
		p.logger.Error().Ctx(ctx).Err(errs[0]).Msg("Failed to get find")
		return errs[0]
	}

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

	udr, err := p.mainRedis.Set(ctx, p.enricher.UserDataKey, ud, 0).Result()
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Str("rediscmd", udr).Msg("Failed to set userData")
		return err
	}
	p.logger.Info().Ctx(ctx).Msgf("Set userData: %s", udr)

	sr, err := p.mainRedis.Set(ctx, p.enricher.StoreKey, storeEn, 0).Result()
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Str("rediscmd", sr).Msg("Failed to set store")
		return err
	}
	p.logger.Info().Ctx(ctx).Msgf("Set store: %s", sr)

	// we should emit for reserver to sync up
	p.logger.Info().Ctx(ctx).Msg("Emitting for reserver to sync up")

	add, err := p.streamRedis.StreamAdd(ctx, tracer, p.stream.Enrich, "ping")
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Str("rediscmd", add.String()).Msg("Failed to add to stream")
		return err
	}
	return nil
}
