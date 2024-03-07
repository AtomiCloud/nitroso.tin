package count

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
	"time"
)

type Count = map[string]map[string]map[string]int

type Client struct {
	redis  *otelredis.OtelRedis
	logger *zerolog.Logger
	ps     string
	loc    *time.Location
	buffer config.BufferConfig
}

func New(buffer config.BufferConfig, rds *otelredis.OtelRedis, logger *zerolog.Logger, ps string, loc *time.Location) *Client {
	return &Client{
		redis:  rds,
		logger: logger,
		ps:     ps,
		loc:    loc,
		buffer: buffer,
	}
}

func (p *Client) GetPollerCount(ctx context.Context, now time.Time) (bool, Count, error) {
	exist, counts, err := p.getCount(ctx, now)
	if counts != nil {
		counts, err = p.filterPoller(counts, now)
	}
	return exist, counts, err
}

func (p *Client) GetReserverCount(ctx context.Context, now time.Time) (bool, Count, error) {
	exist, counts, err := p.getCount(ctx, now)
	if counts != nil {
		counts, err = p.filterReserve(counts, now)
	}
	return exist, counts, err
}

func (p *Client) getCount(ctx context.Context, now time.Time) (bool, Count, error) {

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
	return true, counts, nil
}

func (p *Client) filterReserve(counts Count, now time.Time) (Count, error) {

	r := make(Count)

	for dir, dirCount := range counts {
		for date, dateCount := range dirCount {
			for t, c := range dateCount {

				within, err := p.isReservable(now, date, t)
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
				}
			}
		}
	}
	return r, nil
}

func (p *Client) filterPoller(counts Count, now time.Time) (Count, error) {

	r := make(Count)

	for dir, dirCount := range counts {
		for date, dateCount := range dirCount {
			for t, c := range dateCount {

				within, err := p.isPollable(now, date, t)
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
				}
			}
		}
	}
	return r, nil
}

func (p *Client) parseDateTime(dateStr, timeStr string) (time.Time, error) {
	layout := "02-01-2006 15:04:05"
	return time.ParseInLocation(layout, dateStr+" "+timeStr, p.loc)
}

// Generate the range of reservable/poll-able, to based on the time of the given
func (p *Client) genRange(n time.Time) (time.Time, time.Time) {

	now := n.In(p.loc)
	plus30m := now.Add(time.Duration(p.buffer.Closing) * time.Minute)
	sixMonthsLater := now.AddDate(0, 6, 0)
	lastDay := time.Date(sixMonthsLater.Year(), sixMonthsLater.Month()+1, 0, 0, 0, 0, 0, p.loc)
	lastMinute := time.Date(lastDay.Year(), lastDay.Month(), lastDay.Day(), 23, 59, 59, 0, p.loc)

	return plus30m, lastMinute
}

func (p *Client) isPollable(n time.Time, dateStr, timeStr string) (bool, error) {
	givenTime, err := p.parseDateTime(dateStr, timeStr)
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to parse date time")
		return false, err
	}
	start, end := p.genRange(n)
	return givenTime.After(start) && givenTime.Before(end), err
}

func (p *Client) isReservable(n time.Time, dateStr, timeStr string) (bool, error) {
	givenTime, err := p.parseDateTime(dateStr, timeStr)
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to parse date time")
		return false, err
	}
	start, _ := p.genRange(n)
	return givenTime.After(start), err
}
