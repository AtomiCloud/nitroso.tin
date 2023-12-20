package reserver

import (
	"context"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"time"
)

type Client struct {
	ktmb             ktmb.Ktmb
	logger           *zerolog.Logger
	reserver         config.ReserverConfig
	stream           config.StreamConfig
	appInfo          string
	fromLogin        chan LoginStore
	fromCount        chan Count
	fromDiff         chan Diff
	redis            *otelredis.OtelRedis
	encr             encryptor.Encryptor[ReserveDto]
	otelConfigurator *telemetry.OtelConfigurator
	psd              string
	loc              *time.Location
}

type DiffNormalizer struct {
	Count   int
	Delta   int
	Instant time.Time
}

type DeferredBuy struct {
	Direction string
	Date      string
	Time      string
	Instant   time.Time
}

type ReserveDto struct {
	UserData      string `json:"userData"`
	BookingData   string `json:"bookingData"`
	PassengerData string `json:"passengerData"`
	Direction     string `json:"direction"`
	Date          string `json:"date"`
	Time          string `json:"time"`
}

func New(k ktmb.Ktmb, logger *zerolog.Logger, rds *otelredis.OtelRedis, encr encryptor.Encryptor[ReserveDto],
	reserver config.ReserverConfig, stream config.StreamConfig, appInfo string, otelConfigurator *telemetry.OtelConfigurator,
	psd string, loc *time.Location,
	fromLogin chan LoginStore, fromCount chan Count, fromDiff chan Diff) *Client {
	return &Client{
		ktmb:             k,
		stream:           stream,
		redis:            rds,
		logger:           logger,
		reserver:         reserver,
		appInfo:          appInfo,
		fromLogin:        fromLogin,
		fromCount:        fromCount,
		fromDiff:         fromDiff,
		otelConfigurator: otelConfigurator,
		encr:             encr,
		psd:              psd,
		loc:              loc,
	}
}

func (c *Client) Start(ctx context.Context) error {
	c.logger.Info().Msg("Preparing Reserver")

	normalizer := make(map[string]map[string]map[string]*DiffNormalizer)

	var loginCache LoginStore = <-c.fromLogin
	var counterCache Count = <-c.fromCount

	deferred := make([]DeferredBuy, 0)

	c.logger.Info().Msg("Starting Reserver")
	for {
		select {
		case counter := <-c.fromCount:
			counterCache = counter
			c.logger.Info().
				Msg("counter updated")
		case login := <-c.fromLogin:
			loginCache = login
			c.logger.Info().
				Msg("Login updated")

			now := time.Now()
			c.logger.Info().Any("deferred", deferred).Msg("before login process")
			filtered := make([]DeferredBuy, 0)
			for _, b := range deferred {
				c.logger.Info().Any("deferred", b).Msg("in login process")
				if now.Sub(b.Instant) < time.Millisecond*3000 {
					filtered = c.reserveProcess(ctx, loginCache, now, b.Direction, b.Date, b.Time, filtered)
				}
			}
			deferred = filtered
			c.logger.Info().Any("deferred", deferred).Msg("after login process")
		case diff := <-c.fromDiff:
			diffTime := fmt.Sprintf("%s:00", diff.Time)
			now := time.Now()

			newDiff := DiffNormalizer{
				Count:   diff.Delta.Count,
				Delta:   diff.Delta.Delta,
				Instant: now,
			}

			if normalizer[diff.Direction] == nil {
				normalizer[diff.Direction] = make(map[string]map[string]*DiffNormalizer)
			}
			if normalizer[diff.Direction][diff.Date] == nil {
				normalizer[diff.Direction][diff.Date] = make(map[string]*DiffNormalizer)
			}

			prevLatest := normalizer[diff.Direction][diff.Date][diffTime]
			if prevLatest != nil {
				// if the latest diff is less than 500ms ago, skip
				if now.Sub(prevLatest.Instant) < time.Millisecond*600 {
					// update the latest diff
					normalizer[diff.Direction][diff.Date][diffTime] = &newDiff
					continue
				}
			}

			c.logger.Info().Any("diff", diff).Any("newDiff", newDiff).Msg("Diff received")

			// update the latest diff
			normalizer[diff.Direction][diff.Date][diffTime] = &newDiff

			// only continue if the delta more than 0
			if diff.Delta.Delta < 1 {
				continue
			}

			// what we need
			needed := counterCache[diff.Direction][diff.Date][diffTime]

			// what we should buy, should be min(needed, diff.Delta.Count)
			buy := needed
			if diff.Delta.Delta < needed {
				buy = diff.Delta.Delta
			}
			c.logger.Info().Msgf("Need to reserve %d tickets and there are %d tickets available. We will attempt to buy %d tickets.\n", needed, diff.Delta.Delta, buy)

			// foreach, we buy
			for i := 0; i < buy; i++ {
				c.logger.Info().Any("deferred", deferred).Msg("before reserve process")
				deferred = c.reserveProcess(ctx, loginCache, now, diff.Direction, diff.Date, diffTime, deferred)
				c.logger.Info().Any("deferred", deferred).Msg("fater reserve process")
			}
		}

	}

}

func (c *Client) reserveProcess(ctx context.Context, loginCache LoginStore, n time.Time, direction, date, t string, deferred []DeferredBuy) []DeferredBuy {
	if loginCache.Find[direction] == nil {
		loginCache.Find[direction] = make(map[string]map[string]enricher.FindRes)
	}
	if loginCache.Find[direction][date] == nil {
		loginCache.Find[direction][date] = make(map[string]enricher.FindRes)
	}
	find := loginCache.Find[direction][date][t]
	if find.TripData == "" {
		c.logger.Error().Msg("Trip data is empty")
		deferred = append(deferred, DeferredBuy{
			Direction: direction,
			Date:      date,
			Time:      t,
			Instant:   n,
		})
		return deferred
	}
	go func(ct context.Context, userData, searchData, tripData string) {

		err := c.blockIfMaintenance()
		if err != nil {
			c.logger.Error().Err(err).Msg("Failed to block if maintenance")
			return
		}
		c.logger.Info().Msg("Starting reserve")
		for i := 0; i < 100; i++ {
			err = c.reserve(ct, direction, date, t, userData, searchData, tripData)
			if err != nil {
				c.logger.Error().Err(err).Msg("Failed to reserve")
			} else {
				c.logger.Info().Msg("Successfully booked")
				return
			}

		}
	}(ctx, loginCache.UserData, find.SearchData, find.TripData)
	return deferred
}

func (c *Client) blockIfMaintenance() error {
	n := time.Now()
	m := c.isMaintenance(n)
	c.logger.Info().Bool("maintenance", m).Msg("Maintenance")
	if m {
		c.logger.Info().Msg("Maintenance is on, blocking till maintenance is over")
		till, err := c.maintenanceOver(n)
		if err != nil {
			c.logger.Error().Err(err).Msg("Failed to calculate maintenance over")
			return err
		}
		time.Sleep(time.Until(till))
	}
	return nil
}

func (c *Client) isMaintenance(t time.Time) bool {

	timeNow := t.In(c.loc)

	h := timeNow.Hour()
	m := timeNow.Minute()
	s := timeNow.Second()

	return h == 23 || (h == 0 && m < 14) || (h == 0 && m == 14 && s <= 55)
}

func (c *Client) maintenanceOver(t time.Time) (time.Time, error) {

	now := t.In(c.loc)

	targetTime, err := time.ParseInLocation("2006-01-02 15:04:05", now.Format("2006-01-02")+" 00:14:55", c.loc)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to parse time")
		return now, err
	}

	if now.After(targetTime) {
		targetTime = targetTime.Add(24 * time.Hour)
	}

	if now.After(targetTime) {
		targetTime = targetTime.Add(24 * time.Hour)
	}
	return targetTime, nil
}

func (c *Client) reserve(ctx context.Context, direction, date, t, userData, searchData, tripData string) error {

	shutdown, err := c.otelConfigurator.Configure(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to configure telemetry")
		return err
	}
	defer func() {
		deferErr := shutdown(ctx)
		if deferErr != nil {
			panic(deferErr)
		}
	}()
	tracer := otel.Tracer(c.psd)
	ctx, span := tracer.Start(ctx, "Enricher notify start")
	defer span.End()

	reserve, err := c.ktmb.Reserve(userData, c.appInfo, searchData, tripData)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to reserve")
		return err
	}
	if !reserve.Status {
		e := fmt.Errorf("failed to reserve")
		c.logger.Error().Err(e).Strs("errors", reserve.Messages).Msg("Failed to reserve")
		return e
	}

	dto := ReserveDto{
		UserData:    userData,
		BookingData: reserve.Data.BookingData,
		Direction:   direction,
		Date:        date,
		Time:        t,
	}

	encrypted, err := c.encr.EncryptAny(dto)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to encrypt")
		return err
	}

	add, err := c.redis.StreamAdd(ctx, tracer, c.stream.Reserver, encrypted)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to add to stream")
		return err
	}
	c.logger.Info().Str("rediscmd", add.String()).Msgf("added to stream")
	return nil
}