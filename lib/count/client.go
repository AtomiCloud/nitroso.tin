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

	// filter by now
	//var filtered map[string]map[string]map[string]int
	//
	//for

	return true, counts, nil
}
