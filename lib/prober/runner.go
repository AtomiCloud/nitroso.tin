package prober

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/reserver"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
)

type ktmbClient interface {
	Reserve(userData, appInformation, searchData, tripData string) (ktmb.GenericRes[ktmb.ReserveRes], error)
	Cancel(userData, bookingData string) (ktmb.GenericRes[*interface{}], error)
}

type cacheStore interface {
	Load(context.Context) (string, enricher.FindStore, error)
	Refresh(context.Context, string, Target) (enricher.FindRes, error)
}

type Runner struct {
	ktmb        ktmbClient
	store       cacheStore
	mainRedis   *otelredis.OtelRedis
	streamRedis *otelredis.OtelRedis
	encryptor   encryptor.Encryptor[reserver.ReserveDto]
	config      config.ProberConfig
	streams     config.StreamConfig
	appInfo     string
	ps          string
	psm         string
	location    *time.Location
	logger      *zerolog.Logger
	writeTally  func(context.Context, JobTally) error
	enqueue     func(context.Context, string) error
}

func NewRunner(k ktmbClient, store *Store, mainRedis, streamRedis *otelredis.OtelRedis,
	encr encryptor.Encryptor[reserver.ReserveDto], cfg config.ProberConfig, streams config.StreamConfig,
	appInfo, ps, psm string, location *time.Location, logger *zerolog.Logger) *Runner {
	runner := &Runner{ktmb: k, store: store, mainRedis: mainRedis, streamRedis: streamRedis, encryptor: encr,
		config: cfg, streams: streams, appInfo: appInfo, ps: ps, psm: psm, location: location, logger: logger}
	runner.writeTally = func(ctx context.Context, tally JobTally) error {
		return WriteTally(ctx, mainRedis, ps, tally)
	}
	runner.enqueue = func(ctx context.Context, encrypted string) error {
		_, err := streamRedis.QueuePush(ctx, otel.Tracer(psm), streams.Reserver, encrypted)
		return err
	}
	return runner
}

func (r *Runner) Run(ctx context.Context, targets []Target, epoch int64, job string) error {
	userData, store, err := r.store.Load(ctx)
	if err != nil {
		tallies := make([]SlotTally, len(targets))
		for i, target := range targets {
			tallies[i] = SlotTally{Slot: target.Key(), Errors: 1, Skipped: 1}
		}
		_ = r.writeTally(context.WithoutCancel(ctx), SumTallies(epoch, job, tallies))
		return fmt.Errorf("prober is a cache-only session consumer: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tallies := make([]SlotTally, len(targets))
	var wg sync.WaitGroup
	for index, target := range targets {
		wg.Add(1)
		go func(index int, target Target) {
			defer wg.Done()
			tallies[index] = r.probeSlot(ctx, cancel, userData, store, target)
		}(index, target)
	}
	wg.Wait()
	tally := SumTallies(epoch, job, tallies)
	for _, slot := range tally.Slots {
		r.logTally(slot, epoch, job, "Prober slot tally")
	}
	r.logTally(tally.Total, epoch, job, "Prober Job tally")
	if err := r.writeTally(context.WithoutCancel(ctx), tally); err != nil {
		return fmt.Errorf("write prober tally: %w", err)
	}
	return nil
}

func (r *Runner) probeSlot(ctx context.Context, cancelEpoch context.CancelFunc, userData string,
	store enricher.FindStore, target Target) SlotTally {
	tally := SlotTally{Slot: target.Key()}
	find := findSlot(store, target)
	if find.TripData == "" {
		var err error
		find, err = r.store.Refresh(ctx, userData, target)
		if err != nil {
			tally.Errors++
			tally.Skipped++
			r.logger.Error().Err(err).Str("slot", target.Key()).Msg("Skipping unseeded prober slot")
			return tally
		}
	}
	pace := time.Duration(r.config.PaceMs) * time.Millisecond
	errorLimit := r.config.ErrorLimit
	if errorLimit <= 0 {
		errorLimit = 5
	}
	baseBackoff := time.Duration(r.config.ErrorBackoffMs) * time.Millisecond
	if baseBackoff <= 0 {
		baseBackoff = 100 * time.Millisecond
	}
	errorsSeen := 0
	refreshed := false
	for tally.Holds < int64(target.Needed) {
		if err := r.waitForMaintenance(ctx); err != nil {
			return tally
		}
		select {
		case <-ctx.Done():
			return tally
		default:
		}

		response, err := r.ktmb.Reserve(userData, r.appInfo, find.SearchData, find.TripData)
		tally.Polls++
		if err != nil {
			tally.Errors++
			errorsSeen++
			if errorsSeen >= errorLimit || !sleepContext(ctx, exponential(baseBackoff, errorsSeen)) {
				return tally
			}
			continue
		}
		if response.Status {
			if err := r.acceptHold(ctx, userData, response.Data.BookingData, target); err != nil {
				tally.Errors++
				// A real hold now exists. If it cannot be delivered or released,
				// stop this slot rather than risk accumulating orphan holds.
				return tally
			}
			tally.Holds++
			errorsSeen = 0
			continue
		}

		switch {
		case Matches(response.Messages, r.config.SessionPatterns):
			tally.SessionDead++
			cancelEpoch()
			return tally
		case Matches(response.Messages, r.config.StaleDataPatterns):
			tally.Stale++
			if refreshed {
				tally.Errors++
				return tally
			}
			refreshed = true
			find, err = r.store.Refresh(ctx, userData, target)
			if err != nil {
				tally.Errors++
				return tally
			}
		case Matches(response.Messages, r.config.RateLimitPatterns):
			tally.RateLimited++
			// Observed only: X-Real-IP rotation is the mitigation. Do not throttle.
		case Matches(response.Messages, r.config.SoldOutPatterns):
			tally.SoldOut++
			if !sleepContext(ctx, pace) {
				return tally
			}
		default:
			tally.Errors++
			errorsSeen++
			if errorsSeen >= errorLimit || !sleepContext(ctx, exponential(baseBackoff, errorsSeen)) {
				return tally
			}
		}
	}
	return tally
}

func (r *Runner) acceptHold(ctx context.Context, userData, bookingData string, target Target) error {
	if r.config.DryRun {
		response, err := r.ktmb.Cancel(userData, bookingData)
		if err != nil {
			return fmt.Errorf("release dry-run hold: %w", err)
		}
		if !response.Status {
			return fmt.Errorf("release dry-run hold: %v", response.Messages)
		}
		return nil
	}
	dto := reserver.ReserveDto{UserData: userData, BookingData: bookingData, Direction: target.Direction, Date: target.Date, Time: target.Time}
	encrypted, err := r.encryptor.EncryptAny(dto)
	if err != nil {
		_, _ = r.ktmb.Cancel(userData, bookingData)
		return err
	}
	if err = r.enqueue(ctx, encrypted); err != nil {
		_, _ = r.ktmb.Cancel(userData, bookingData)
		return err
	}
	return nil
}

func (r *Runner) waitForMaintenance(ctx context.Context) error {
	now := time.Now().In(r.location)
	inMaintenance := now.Hour() == 23 || (now.Hour() == 0 && (now.Minute() < 14 || now.Minute() == 14 && now.Second() <= 55))
	if !inMaintenance {
		return nil
	}
	target, err := time.ParseInLocation("2006-01-02 15:04:05", now.Format("2006-01-02")+" 00:14:59", r.location)
	if err != nil {
		return err
	}
	if !target.After(now) {
		target = target.Add(24 * time.Hour)
	}
	timer := time.NewTimer(time.Until(target))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func ParseTargets(data string) ([]Target, error) {
	var targets []Target
	if err := json.Unmarshal([]byte(data), &targets); err != nil {
		return nil, fmt.Errorf("parse prober targets: %w", err)
	}
	for _, target := range targets {
		if target.Direction == "" || target.Date == "" || target.Time == "" || target.Needed <= 0 {
			return nil, errors.New("each prober target requires dir, date, time and needed > 0")
		}
	}
	return targets, nil
}

func exponential(base time.Duration, attempt int) time.Duration {
	if attempt > 6 {
		attempt = 6
	}
	return base * time.Duration(1<<uint(attempt-1))
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *Runner) logTally(tally SlotTally, epoch int64, job, message string) {
	r.logger.Info().Str("slot", tally.Slot).Int64("polls", tally.Polls).Int64("holds", tally.Holds).
		Int64("soldOut", tally.SoldOut).Int64("stale", tally.Stale).Int64("errors", tally.Errors).
		Int64("rateLimited", tally.RateLimited).Int64("sessionDead", tally.SessionDead).Int64("skipped", tally.Skipped).
		Int64("epoch", epoch).Str("job", job).Msg(message)
}
