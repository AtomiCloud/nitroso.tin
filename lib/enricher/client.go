package enricher

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/rs/zerolog"
	ti "time"
)

type Client struct {
	ktmb   ktmb.Ktmb
	logger *zerolog.Logger
}

type FindRes struct {
	SearchData string
	TripData   string
}

func New(ktmb ktmb.Ktmb, logger *zerolog.Logger) Client {
	return Client{
		ktmb:   ktmb,
		logger: logger,
	}
}

func (c *Client) Find(userData, dir string, date string, time string) (FindRes, error) {
	c.logger.Info().Msg("Initializing enricher")
	c.logger.Info().Msgf("Date: %s", date)
	c.logger.Info().Msgf("Dir: %s", dir)

	d := fmt.Sprintf("%sT00:00:00", date)
	dt := fmt.Sprintf("%sT%s", date, time)

	all, err := c.ktmb.StationsAll(userData)
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

	ti.Sleep(2 * ti.Second)
	stations, err := c.ktmb.SearchStations(userData, fId, fData, tId, tData, d, 1)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to search station")
		return FindRes{}, err
	}

	if !stations.Status {
		c.logger.Error().Strs("errors", stations.Messages).Msg("Failed to search station")
		return FindRes{}, fmt.Errorf("failed to search station: %v", stations.Messages)
	}

	ti.Sleep(2 * ti.Second)
	trip, err := c.ktmb.Trip(userData, d, stations.Data.SearchData)
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
