package reserver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"strings"
)

type Differ struct {
	fromCount  chan Count
	toReserver chan Diff
	rds        *otelredis.OtelRedis
	logger     *zerolog.Logger
}

func NewDiffer(fromCount chan Count, toReserver chan Diff, rds *otelredis.OtelRedis, logger *zerolog.Logger) *Differ {
	return &Differ{
		fromCount:  fromCount,
		toReserver: toReserver,
		rds:        rds,
		logger:     logger,
	}
}

func (d *Differ) getChannels(c Count) []Chan {
	var channels []Chan
	for dir, dates := range c {
		for date, _ := range dates {
			channels = append(channels, Chan{
				Direction: dir,
				Date:      date,
			})
		}
	}
	return channels
}

func (d *Differ) Start(ctx context.Context) {

	d.logger.Info().Msg("Starting differ")

	term := make(chan bool)
	done := make(chan bool)

	prev := make(Count)

	count1 := <-d.fromCount
	go d.sub(ctx, prev, count1, term, done, d.toReserver)
	for {
		count := <-d.fromCount
		d.logger.Info().Any("count", count).Msg("Received new count")
		term <- false
		<-done
		go d.sub(ctx, prev, count, term, done, d.toReserver)
	}
}

func (d *Differ) diff(dir, date string, prevState Count, newState map[string]int) (Count, map[string]DiffData, error) {

	if prevState[dir] == nil {
		prevState[dir] = make(map[string]map[string]int)
	}
	if prevState[dir][date] == nil {
		prevState[dir][date] = make(map[string]int)
	}
	prev := prevState[dir][date]

	diff := make(map[string]DiffData)

	// check if there are any diffs
	for time, count := range newState {
		pCount := prev[time]
		if pCount != count {
			diff[time] = DiffData{
				Count: count,
				Prev:  pCount,
				Delta: count - pCount,
			}
		}
	}
	prevState[dir][date] = newState
	return prevState, diff, nil
}

func (d *Differ) toString(c []Chan) []string {
	var s []string
	for _, v := range c {
		s = append(s, fmt.Sprintf("ktmb:schedule:%s:%s", v.Direction, v.Date))
	}
	return s
}

func (d *Differ) sub(ctx context.Context, prev, relevant Count, t, done chan bool, c chan Diff) error {

	listens := d.getChannels(relevant)
	cs := d.toString(listens)

	d.logger.Info().Ctx(ctx).Any("channels", cs).Msgf("Subscribing to channels: %+v\n", cs)

	subscriber := d.rds.Subscribe(ctx, cs...)
	defer func(subscriber *redis.PubSub) {
		err := subscriber.Close()
		if err != nil {
			d.logger.Fatal().Ctx(ctx).Err(err).Msg("Failed to close subscriber")
			panic(err)
		}
	}(subscriber)

	channels := subscriber.Channel()
	for {
		select {
		case m := <-channels:
			// Get message from all relevant channels
			cn := strings.TrimPrefix(m.Channel, "ktmb:schedule:")

			s := strings.Split(cn, ":")
			dir := s[0]
			date := s[1]

			var rxCount map[string]int
			err := json.Unmarshal([]byte(m.Payload), &rxCount)
			if err != nil {
				d.logger.Info().Ctx(ctx).Err(err).Msg("Failed to unmarshal message")
				return err
			}

			// filter RX count relevant only to count
			if relevant[dir] == nil {
				relevant[dir] = make(map[string]map[string]int)
			}
			if relevant[dir][date] == nil {
				relevant[dir][date] = make(map[string]int)
			}
			filterCheck := relevant[dir][date]
			filtered := make(map[string]int)

			for k, v := range rxCount {
				key := fmt.Sprintf("%s:00", k)
				if filterCheck[key] != 0 {
					filtered[k] = v
				}
			}

			// Obtain diff of that message with our own cache
			newPrev, diff, err := d.diff(dir, date, prev, filtered)
			if err != nil {
				d.logger.Info().Ctx(ctx).Err(err).Msg("Failed to diff")
				return err
			}
			prev = newPrev

			// Send the diff to reserver
			for k, v := range diff {
				di := Diff{
					Direction: dir,
					Date:      date,
					Time:      k,
					Delta:     v,
				}
				c <- di
			}
		case <-t:
			d.logger.Info().Ctx(ctx).Msg("Received termination signal, exiting differ")
			done <- true
			return nil
		}
	}

}
