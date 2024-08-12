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
	mainRedis        *otelredis.OtelRedis
	streamRedis      *otelredis.OtelRedis
	encr             encryptor.Encryptor[ReserveDto]
	otelConfigurator *telemetry.OtelConfigurator
	psm              string
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

func New(k ktmb.Ktmb, logger *zerolog.Logger, mainRedis, streamRedis *otelredis.OtelRedis, encr encryptor.Encryptor[ReserveDto],
	reserver config.ReserverConfig, stream config.StreamConfig, appInfo string, otelConfigurator *telemetry.OtelConfigurator,
	psm string, loc *time.Location,
	fromLogin chan LoginStore, fromCount chan Count, fromDiff chan Diff) *Client {
	return &Client{
		ktmb:             k,
		stream:           stream,
		mainRedis:        mainRedis,
		streamRedis:      streamRedis,
		logger:           logger,
		reserver:         reserver,
		appInfo:          appInfo,
		fromLogin:        fromLogin,
		fromCount:        fromCount,
		fromDiff:         fromDiff,
		otelConfigurator: otelConfigurator,
		encr:             encr,
		psm:              psm,
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
				if now.Sub(b.Instant) < time.Minute*5 {
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
			if diff.Delta.Count < 1 {
				continue
			}

			// what we need
			needed := counterCache[diff.Direction][diff.Date][diffTime]

			// what we should buy, should be min(needed, diff.Delta.Count)
			buy := needed
			if diff.Delta.Delta < needed {
				buy = diff.Delta.Delta
			}
			c.logger.Info().Msgf("Need to reserve %d tickets and there are %d tickets available. We will attempt to buy %d tickets.", needed, diff.Delta.Delta, buy)

			// foreach, we buy
			for i := 0; i < buy; i++ {
				c.logger.Info().Any("deferred", deferred).Str("time", diffTime).Str("date", diff.Date).Str("dir", diff.Direction).Msg("before reserve process")
				deferred = c.reserveProcess(ctx, loginCache, now, diff.Direction, diff.Date, diffTime, deferred)
				c.logger.Info().Any("deferred", deferred).Str("time", diffTime).Str("date", diff.Date).Str("dir", diff.Direction).Msg("after reserve process")
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

	tn := time.Now()
	isM := c.isMaintenance(tn)
	attempt := c.reserver.NormalAttempts
	concurrency := c.reserver.NormalConcurrency
	if isM {
		attempt = c.reserver.MaintenanceAttempts
		concurrency = c.reserver.MaintenanceConcurrency
	}

	term := make(chan bool, concurrency)
	for replica := 0; replica < concurrency; replica++ {
		go func(term chan bool, ct context.Context, replica int, userData, searchData, tripData string) {
			_, err := c.blockIfMaintenance()
			if err != nil {
				c.logger.Error().Err(err).Int("replica", replica).Str("time", t).Str("date", date).Str("dir", direction).Msg("Failed to block if maintenance")
				return
			}

			c.logger.Info().Int("replica", replica).Msg("Starting reserve")
			for i := 0; i < attempt; i++ {
				select {
				case <-term:
					c.logger.Info().Int("attempt", i).Str("time", t).Str("date", date).Str("dir", direction).Int("replica", replica).Msg("Received term signal from local replicas")
					term <- true
					c.logger.Info().Int("attempt", i).Str("time", t).Str("date", date).Str("dir", direction).Int("replica", replica).Msg("Re-Emitted Term Signal")
					return
				default:
					err = c.reserve(ct, direction, date, t, userData, searchData, tripData)
					if err != nil {
						c.logger.Error().Err(err).Str("time", t).Str("date", date).Str("dir", direction).Int("replica", replica).Int("attempt", i).Msg("Failed to reserve")
					} else {
						c.logger.Info().Str("time", t).Str("date", date).Str("dir", direction).Int("attempt", i).Int("replica", replica).Msg("Successfully booked")
						term <- true
						c.logger.Info().Str("time", t).Str("date", date).Str("dir", direction).Int("attempt", i).Int("replica", replica).Msg("Emitted Term Signal")
						return
					}
				}
			}
		}(term, ctx, replica, loginCache.UserData, find.SearchData, find.TripData)
	}

	return deferred
}

func (c *Client) blockIfMaintenance() (bool, error) {
	n := time.Now()
	m := c.isMaintenance(n)
	c.logger.Info().Bool("maintenance", m).Msg("Maintenance")
	if m {
		c.logger.Info().Msg("Maintenance is on, blocking till maintenance is over")
		till, err := c.maintenanceOver(n)
		if err != nil {
			c.logger.Error().Err(err).Msg("Failed to calculate maintenance over")
			return false, err
		}
		time.Sleep(time.Until(till))
	}
	return m, nil
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

	targetTime, err := time.ParseInLocation("2006-01-02 15:04:05", now.Format("2006-01-02")+" 00:14:59", c.loc)
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

	tracer := otel.Tracer(c.psm)
	ctx, span := tracer.Start(ctx, "Reserver notify buyer start")
	defer span.End()

	add, err := c.streamRedis.QueuePush(ctx, tracer, c.stream.Reserver, encrypted)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to add to queue")
		return err
	}
	c.logger.Info().Str("rediscmd", add.String()).Msg("added to queue")
	return nil
}
