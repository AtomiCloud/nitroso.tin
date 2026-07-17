package prober

import (
	"context"
	"crypto/sha256"
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
	ReserveContext(context.Context, string, string, string, string) (ktmb.GenericRes[ktmb.ReserveRes], error)
	CancelContext(context.Context, string, string) (ktmb.GenericRes[*interface{}], error)
}

type probeStore interface {
	Load(context.Context) (string, enricher.FindStore, error)
	Refresh(context.Context, string, Target) (enricher.FindRes, error)
}

type Runner struct {
	ktmb              ktmbClient
	store             probeStore
	encryptor         encryptor.Encryptor[reserver.ReserveDto]
	config            config.ProberConfig
	appInfo           string
	location          *time.Location
	logger            *zerolog.Logger
	writeTally        func(context.Context, JobTally) error
	enqueue           func(context.Context, string) error
	persistRelease    func(context.Context, string) error
	listReleases      func(context.Context, int) ([]string, error)
	removeRelease     func(context.Context, string) error
	signalSessionDead func(context.Context, string) error
}

func NewRunner(k ktmbClient, store *Store, mainRedis, streamRedis *otelredis.OtelRedis,
	encr encryptor.Encryptor[reserver.ReserveDto], cfg config.ProberConfig, streams config.StreamConfig,
	appInfo, ps, psm string, location *time.Location, logger *zerolog.Logger) *Runner {
	runner := &Runner{ktmb: k, store: store, encryptor: encr,
		config: cfg, appInfo: appInfo, location: location, logger: logger}
	runner.writeTally = func(ctx context.Context, tally JobTally) error {
		return WriteTally(ctx, mainRedis, ps, tally)
	}
	runner.enqueue = func(ctx context.Context, encrypted string) error {
		_, err := streamRedis.QueuePush(ctx, otel.Tracer(psm), streams.Reserver, encrypted)
		return err
	}
	releaseKey := ps + ":prober:release"
	runner.persistRelease = func(ctx context.Context, encrypted string) error {
		return mainRedis.LPush(ctx, releaseKey, encrypted).Err()
	}
	runner.listReleases = func(ctx context.Context, limit int) ([]string, error) {
		return mainRedis.LRange(ctx, releaseKey, 0, int64(limit-1)).Result()
	}
	runner.removeRelease = func(ctx context.Context, encrypted string) error {
		return mainRedis.LRem(ctx, releaseKey, 1, encrypted).Err()
	}
	runner.signalSessionDead = func(ctx context.Context, fingerprint string) error {
		key := ps + ":prober:session-dead"
		pipe := mainRedis.TxPipeline()
		pipe.SAdd(ctx, key, fingerprint)
		pipe.Expire(ctx, key, 15*time.Minute)
		_, err := pipe.Exec(ctx)
		return err
	}
	return runner
}

func (r *Runner) Run(ctx context.Context, targets []Target, epoch int64, job string, probeUntil time.Time) error {
	userData, store, err := r.store.Load(ctx)
	releaseUserData := ""
	if err == nil {
		releaseUserData = userData
	}
	if releaseErr := r.drainReleases(ctx, releaseUserData); releaseErr != nil {
		r.logger.Error().Err(releaseErr).Msg("Failed to drain durable prober hold releases")
	}
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
			tallies[index] = r.probeSlot(ctx, cancel, userData, store, target, probeUntil)
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
	store enricher.FindStore, target Target, probeUntil time.Time) SlotTally {
	tally := SlotTally{Slot: target.Key()}
	find := findSlot(store, target)
	if !completeFind(find) {
		var err error
		find, err = r.refreshSlot(ctx, userData, target)
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
		if !time.Now().Before(probeUntil) {
			return tally
		}
		maintenanceCtx, maintenanceCancel := context.WithDeadline(ctx, probeUntil)
		maintenanceErr := r.waitForMaintenance(maintenanceCtx)
		maintenanceCancel()
		if maintenanceErr != nil {
			return tally
		}
		if !time.Now().Before(probeUntil) {
			return tally
		}
		select {
		case <-ctx.Done():
			return tally
		default:
		}

		requestCtx, requestCancel := context.WithTimeout(ctx, 60*time.Second)
		response, err := r.ktmb.ReserveContext(requestCtx, userData, r.appInfo, find.SearchData, find.TripData)
		requestCancel()
		tally.Polls++
		if err != nil {
			var statusErr *ktmb.HttpStatusError
			if errors.As(err, &statusErr) {
				messages := []string{statusErr.Body}
				switch {
				case statusErr.StatusCode == 401 || statusErr.StatusCode == 403 || Matches(messages, r.config.SessionPatterns):
					tally.SessionDead++
					r.markSessionDead(ctx, cancelEpoch, userData, target, messages)
					return tally
				case Matches(messages, r.config.StaleDataPatterns):
					tally.Stale++
					if refreshed {
						tally.Errors++
						return tally
					}
					refreshed = true
					find, err = r.refreshSlot(ctx, userData, target)
					if err != nil {
						tally.Errors++
						return tally
					}
					continue
				case statusErr.StatusCode == 429 || Matches(messages, r.config.RateLimitPatterns):
					tally.RateLimited++
					continue
				case Matches(messages, r.config.SoldOutPatterns):
					tally.SoldOut++
					if !sleepContext(ctx, pace) {
						return tally
					}
					continue
				default:
					r.logger.Warn().Str("slot", target.Key()).Int("status", statusErr.StatusCode).
						Str("body", statusErr.Body).Msg("Unclassified KTMB HTTP response")
				}
			}
			tally.Errors++
			errorsSeen++
			if errorsSeen >= errorLimit || !sleepContext(ctx, exponential(baseBackoff, errorsSeen)) {
				return tally
			}
			continue
		}
		if response.Status {
			if response.Data.BookingData == "" {
				tally.Errors++
				r.logger.Error().Str("slot", target.Key()).Msg("KTMB Reserve success omitted bookingData")
				return tally
			}
			// The KTMB hold exists as soon as Reserve succeeds. Delivery or
			// compensation failures are separate outcomes and must not erase the
			// acquisition from the hit-rate tally.
			tally.Holds++
			releaseFailed, acceptErr := r.acceptHold(ctx, userData, response.Data.BookingData, target)
			if acceptErr != nil {
				tally.Errors++
				if releaseFailed {
					tally.ReleaseFailed++
				}
				r.logger.Error().Err(acceptErr).Str("slot", target.Key()).Bool("releaseFailed", releaseFailed).
					Msg("Failed to deliver or release acquired KTMB hold")
				// A real hold now exists. If it cannot be delivered or released,
				// stop this slot rather than risk accumulating orphan holds.
				return tally
			}
			errorsSeen = 0
			continue
		}

		switch {
		case Matches(response.Messages, r.config.SessionPatterns):
			tally.SessionDead++
			r.markSessionDead(ctx, cancelEpoch, userData, target, response.Messages)
			return tally
		case Matches(response.Messages, r.config.StaleDataPatterns):
			tally.Stale++
			if refreshed {
				tally.Errors++
				return tally
			}
			refreshed = true
			find, err = r.refreshSlot(ctx, userData, target)
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
			r.logger.Warn().Str("slot", target.Key()).Strs("messages", response.Messages).
				Msg("Unclassified KTMB Reserve response")
			errorsSeen++
			if errorsSeen >= errorLimit || !sleepContext(ctx, exponential(baseBackoff, errorsSeen)) {
				return tally
			}
		}
	}
	return tally
}

func (r *Runner) markSessionDead(ctx context.Context, cancelEpoch context.CancelFunc, userData string, target Target, messages []string) {
	cancelEpoch()
	signalCtx, signalCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer signalCancel()
	if err := r.signalSessionDead(signalCtx, SessionFingerprint(userData)); err != nil {
		r.logger.Error().Err(err).Str("slot", target.Key()).Strs("messages", messages).
			Msg("Failed to persist dead KTMB session signal")
	}
}

func (r *Runner) refreshSlot(ctx context.Context, userData string, target Target) (enricher.FindRes, error) {
	find, err := r.store.Refresh(ctx, userData, target)
	if err != nil {
		return enricher.FindRes{}, err
	}
	if !completeFind(find) {
		return enricher.FindRes{}, errors.New("enrichment returned incomplete searchData/tripData")
	}
	return find, nil
}

func (r *Runner) acceptHold(ctx context.Context, userData, bookingData string, target Target) (bool, error) {
	dto := reserver.ReserveDto{UserData: userData, BookingData: bookingData, Direction: target.Direction, Date: target.Date, Time: target.Time}
	if r.config.DryRun {
		return r.compensateHold(ctx, dto, errors.New("dry-run hold must be released"))
	}
	encrypted, err := r.encryptor.EncryptAny(dto)
	if err != nil {
		return r.compensateHold(ctx, dto, fmt.Errorf("encrypt hold DTO: %w", err))
	}
	if err = r.enqueue(ctx, encrypted); err != nil {
		return r.compensateHold(ctx, dto, fmt.Errorf("enqueue hold DTO: %w", err))
	}
	return false, nil
}

// compensateHold confirms cancellation or durably records the encrypted hold
// for retry by this and subsequent prober Jobs. Returning releaseFailed=true
// means immediate cancellation was not confirmed, even if persistence worked.
func (r *Runner) compensateHold(ctx context.Context, dto reserver.ReserveDto, cause error) (bool, error) {
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	response, cancelErr := r.ktmb.CancelContext(cancelCtx, dto.UserData, dto.BookingData)
	if cancelErr == nil && response.Status {
		if r.config.DryRun {
			return false, nil
		}
		return false, cause
	}
	if cancelErr == nil {
		cancelErr = fmt.Errorf("KTMB cancel rejected: %v", response.Messages)
	}
	encrypted, encryptErr := r.encryptor.EncryptAny(dto)
	if encryptErr != nil {
		return true, errors.Join(cause, cancelErr, fmt.Errorf("encrypt durable release: %w", encryptErr))
	}
	persistErr := r.persistRelease(context.WithoutCancel(ctx), encrypted)
	if persistErr != nil {
		return true, errors.Join(cause, cancelErr, fmt.Errorf("persist durable release: %w", persistErr))
	}
	return true, errors.Join(cause, cancelErr, errors.New("hold queued for durable release retry"))
}

func (r *Runner) drainReleases(ctx context.Context, currentUserData string) error {
	limit := r.config.ReleaseDrainLimit
	if limit <= 0 {
		limit = 10
	}
	budget := time.Duration(r.config.ReleaseDrainBudgetMs) * time.Millisecond
	if budget <= 0 {
		budget = 5 * time.Second
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	values, err := r.listReleases(cleanupCtx, limit)
	if err != nil {
		return err
	}
	for index, encrypted := range values {
		if index >= limit || cleanupCtx.Err() != nil {
			break
		}
		dto, decryptErr := r.encryptor.DecryptAny(encrypted)
		if decryptErr != nil {
			r.logger.Error().Err(decryptErr).Msg("Unreadable durable prober release entry")
			if removeErr := r.removeRelease(cleanupCtx, encrypted); removeErr != nil {
				return removeErr
			}
			continue
		}
		cancelUserData := dto.UserData
		if currentUserData != "" {
			// Every prober hold belongs to the one funded account. A newer session
			// for that account can release a hold created by an older invalidated
			// session, avoiding a retry loop on dead userData.
			cancelUserData = currentUserData
		}
		response, cancelErr := r.ktmb.CancelContext(cleanupCtx, cancelUserData, dto.BookingData)
		if cancelErr != nil || !response.Status {
			messages := response.Messages
			sessionRejected := cancelErr == nil && Matches(messages, r.config.SessionPatterns)
			if cancelErr != nil {
				var statusErr *ktmb.HttpStatusError
				if errors.As(cancelErr, &statusErr) {
					messages = []string{statusErr.Body}
					sessionRejected = statusErr.StatusCode == 401 || statusErr.StatusCode == 403 ||
						Matches(messages, r.config.SessionPatterns)
				}
			}
			if sessionRejected {
				if signalErr := r.signalSessionDead(cleanupCtx, SessionFingerprint(cancelUserData)); signalErr != nil {
					r.logger.Error().Err(signalErr).Msg("Failed to signal session rejected during durable hold release")
				}
				r.logger.Error().Err(cancelErr).Strs("messages", messages).
					Msg("Session-rejected durable prober hold release remains pending")
				return nil
			}
			if cancelErr == nil && Matches(response.Messages, r.config.ReleaseTerminalPatterns) {
				if removeErr := r.removeRelease(cleanupCtx, encrypted); removeErr != nil {
					return removeErr
				}
				r.logger.Warn().Strs("messages", response.Messages).Msg("Removed terminal durable prober release")
				continue
			}
			r.logger.Error().Err(cancelErr).Strs("messages", response.Messages).
				Msg("Durable prober hold release remains pending")
			continue
		}
		if removeErr := r.removeRelease(cleanupCtx, encrypted); removeErr != nil {
			return removeErr
		}
		r.logger.Info().Str("slot", Target{Direction: dto.Direction, Date: dto.Date, Time: dto.Time}.Key()).
			Msg("Released durable prober hold")
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
		Int64("releaseFailed", tally.ReleaseFailed).Int64("epoch", epoch).Str("job", job).Msg(message)
}

func SessionFingerprint(userData string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(userData)))
}
