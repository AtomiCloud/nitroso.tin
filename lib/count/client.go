package count

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/rs/zerolog"
	"time"
)

type Count = map[string]map[string]map[string]int

type Client struct {
	redis  *otelredis.OtelRedis
	logger *zerolog.Logger
	ps     string
}

func New(rds *otelredis.OtelRedis, logger *zerolog.Logger, ps string) *Client {
	return &Client{
		redis:  rds,
		logger: logger,
		ps:     ps,
	}
}

func (p *Client) GetCount(ctx context.Context, now time.Time) (bool, Count, error) {

	key := fmt.Sprintf("%s:%s", p.ps, "count")

	exists, err := p.redis.Exists(ctx, key).Result()
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to check if key exists")
		return false, nil, err
	}
	if exists == 0 {
		p.logger.Info().Ctx(ctx).Msgf("Key '%s' does not exist", key)
		return false, nil, nil
	}

	p.logger.Info().Ctx(ctx).Msgf("Getting counts from redis '%s'", key)
	countsJson, err := p.redis.Get(ctx, key).Result()
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to get counts")
		return false, nil, err
	}
	var counts map[string]map[string]map[string]int
	err = json.Unmarshal([]byte(countsJson), &counts)
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to unmarshal counts")
		return false, nil, err
	}

	p.logger.Info().Ctx(ctx).Any("counts", counts).Msg("Count Read obtain counts")
	// filter by now
	filtered, err := p.filter(counts, now)
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to filter counts")
		return false, nil, err
	}
	p.logger.Info().Ctx(ctx).Any("filtered", counts).Msg("Filtered counts")
	return true, filtered, nil
}

func (p *Client) filter(counts Count, now time.Time) (Count, error) {

	var r Count

	for dir, dirCount := range counts {
		for date, dateCount := range dirCount {
			for t, c := range dateCount {

				within, err := p.isWithinRange(now, date, t)
				if err != nil {
					p.logger.Error().Err(err).Str("date", date).Str("time", t).Msg("Failed to check if within range")
					return nil, err
				}
				if within {
					if r[dir] == nil {
						r[dir] = make(map[string]map[string]int)
					}
					if r[dir][date] == nil {
						r[dir][date] = make(map[string]int)
					}
					r[dir][date][t] = c
				} else {
					p.logger.Info().Msgf("Not within range: %s %s %s", dir, date, t)
				}
			}
		}
	}
	return r, nil
}

func (p *Client) parseDateTime(dateStr, timeStr string) (time.Time, error) {
	layout := "02-01-2006 15:04:05"
	return time.Parse(layout, dateStr+" "+timeStr)
}

func (p *Client) lastMomentOfDay(t time.Time) time.Time {
	return t.AddDate(0, 0, 1).Truncate(24 * time.Hour).Add(-time.Second)
}

func (p *Client) isWithinRange(now time.Time, dateStr, timeStr string) (bool, error) {
	givenTime, err := p.parseDateTime(dateStr, timeStr)
	if err != nil {
		p.logger.Error().Err(err).Str("date", dateStr).Str("time", timeStr).Msg("Failed to parse date time")
		return false, err
	}
	plus30m := givenTime.Add(30 * time.Minute)
	sixMonthsLater := givenTime.AddDate(0, 6, 0)
	endOfSixMonths := p.lastMomentOfDay(sixMonthsLater)

	return now.After(plus30m) && now.Before(endOfSixMonths), nil
}
