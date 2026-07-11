package enricher

import (
	"context"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/session"
	"github.com/rs/zerolog"
	ti "time"
)

type Client struct {
	ktmb    ktmb.Ktmb
	session *session.Session
	logger  *zerolog.Logger
}

type FindRes struct {
	SearchData string
	TripData   string
}

func New(ktmb ktmb.Ktmb, session *session.Session, logger *zerolog.Logger) Client {
	return Client{
		ktmb:    ktmb,
		session: session,
		logger:  logger,
	}
}

func (c *Client) Find(userData, dir string, date string, time string) (FindRes, error) {
	return c.FindContext(context.Background(), userData, dir, date, time)
}

func (c *Client) FindContext(ctx context.Context, userData, dir string, date string, time string) (FindRes, error) {
	c.logger.Info().Msg("Initializing enricher")
	c.logger.Info().Msgf("Date: %s", date)
	c.logger.Info().Msgf("Dir: %s", dir)

	d := fmt.Sprintf("%sT00:00:00", date)
	dt := fmt.Sprintf("%sT%s", date, time)

	all, err := c.ktmb.StationsAllContext(ctx, userData)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get all station")
		return FindRes{}, err
	}
	if !all.Status {
		c.logger.Error().Strs("errors", all.Messages).Msg("Failed to get all station")
		return FindRes{}, fmt.Errorf("failed to get all station: %v", all.Messages)
	}

	jId := "37500"
	wId := "37600"
	jData := ""
	wData := ""

	for _, s := range all.Data.Stations {
		if s.ID == jId {
			jData = s.StationData
		}
		if s.ID == wId {
			wData = s.StationData
		}
	}

	fId := ""
	fData := ""
	tId := ""
	tData := ""
	if dir == "JToW" {

		fId = jId
		fData = jData
		tId = wId
		tData = wData

	} else {
		fId = wId
		fData = wData
		tId = jId
		tData = jData
	}

	if err := sleepContext(ctx, 2*ti.Second); err != nil {
		return FindRes{}, err
	}
	stations, err := c.ktmb.SearchStationsContext(ctx, userData, fId, fData, tId, tData, d, 1)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to search station")
		return FindRes{}, err
	}

	if !stations.Status {
		c.logger.Error().Strs("errors", stations.Messages).Msg("Failed to search station")
		return FindRes{}, fmt.Errorf("failed to search station: %v", stations.Messages)
	}

	if err := sleepContext(ctx, 2*ti.Second); err != nil {
		return FindRes{}, err
	}
	trip, err := c.ktmb.TripContext(ctx, userData, d, stations.Data.SearchData)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get trips")
		return FindRes{}, err
	}

	if !trip.Status {
		c.logger.Error().Strs("errors", trip.Messages).Msg("Failed to get trips")
		return FindRes{}, fmt.Errorf("failed to get trips: %v", trip.Messages)
	}

	td := ""

	c.logger.Info().Any("trip", trip).Msg("trip response")
	for _, v := range trip.Data.Trips {
		if v.DepartDateTime == dt {
			td = v.TripData
		}
	}
	if td == "" {
		return FindRes{}, fmt.Errorf("no trip found")
	}

	return FindRes{
		SearchData: stations.Data.SearchData,
		TripData:   td,
	}, nil

}

func sleepContext(ctx context.Context, duration ti.Duration) error {
	timer := ti.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
