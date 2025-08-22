package buyer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/reserver"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"io"
	"math"
	"mime/multipart"
	"time"
)

type Client struct {
	buyer            *Buyer
	mainRedis        *otelredis.OtelRedis
	streamRedis      *otelredis.OtelRedis
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	streamsCfg       config.StreamConfig
	buyerCfg         config.BuyerConfig
	psm              string
	zinc             *zinc.Client
	encr             encryptor.Encryptor[reserver.ReserveDto]
}

var baseDelay = 1 * time.Second

func CreateForm(values map[string]io.Reader) (s string, reader io.Reader, err error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for key, r := range values {
		var fw io.Writer
		if fw, err = w.CreateFormFile(key, key); err != nil {
			return
		}
		if _, err = io.Copy(fw, r); err != nil {
			return
		}

	}
	w.Close()

	return w.FormDataContentType(), &b, nil
}

func New(buyer *Buyer, mainRedis, streamRedis *otelredis.OtelRedis, otelConfigurator *telemetry.OtelConfigurator, logger *zerolog.Logger,
	streamsCfg config.StreamConfig, buyerCfg config.BuyerConfig, psm string, zinc *zinc.Client, enrc encryptor.Encryptor[reserver.ReserveDto]) *Client {
	return &Client{
		buyer:            buyer,
		mainRedis:        mainRedis,
		streamRedis:      streamRedis,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		streamsCfg:       streamsCfg,
		buyerCfg:         buyerCfg,
		psm:              psm,
		zinc:             zinc,
		encr:             enrc,
	}
}

func (c *Client) Start(ctx context.Context) error {
	maxCounter := c.buyerCfg.BackoffLimit

	errorCounter := 0

	for {
		shouldExit, err := c.loop(ctx)
		if err != nil {
			if errorCounter >= maxCounter {
				c.logger.Error().Err(err).Msg("Failed all backoff attempts, exiting...")
				return err
			}
			secRetry := math.Pow(2, float64(errorCounter))
			c.logger.Info().Msgf("Retrying operation in %f seconds", secRetry)
			delay := time.Duration(secRetry) * baseDelay
			time.Sleep(delay)
			errorCounter++
		} else {
			errorCounter = 0
		}
		if shouldExit {
			break
		}
	}
	return nil
}

func (c *Client) loop(ctx context.Context) (bool, error) {
	shutdown, err := c.otelConfigurator.Configure(ctx)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to configure telemetry")
		return true, err
	}
	defer func() {
		deferErr := shutdown(ctx)
		if deferErr != nil {
			panic(deferErr)
		}
	}()

	tracer := otel.Tracer(c.psm)

	c.logger.Info().Ctx(ctx).Str("queue", c.streamsCfg.Reserver).Msg("Buyer waiting for reserver message...")
	err = c.streamRedis.QueuePop(ctx, tracer, c.streamsCfg.Reserver, func(ctx context.Context, message json.RawMessage) error {
		c.logger.Info().Ctx(ctx).Msg("Buyer received reserver emitted signal")
		var output string
		e := json.Unmarshal(message, &output)
		if e != nil {
			c.logger.Error().Err(e).Msg("Failed to unmarshal reserver emitted signal")
			return e
		}
		reserveDto, e := c.encr.DecryptAny(output)
		if e != nil {
			c.logger.Error().Err(e).Msg("Failed to decrypt reserver emitted signal")
			return e
		}
		c.logger.Info().Any("reserveDto", reserveDto).Ctx(ctx).Msg("Reserver emitted signal decrypted")
		er := c.buy(ctx, reserveDto.Direction, reserveDto.Date, reserveDto.Time, reserveDto.UserData, reserveDto.BookingData)
		if er != nil {
			c.logger.Error().Err(er).Msg("Failed to buy")
			return er
		}
		return nil
	})
	if err != nil {
		c.logger.Error().
			Err(err).
			Msg("Failed to read from redis list in buyer (from reserver)")
		return false, err
	} else {
		c.logger.Info().Msg("Buyer queue pop loop ended without failure")
	}
	return false, nil
}

func (c *Client) buy(ctx context.Context, direction, date, t, userData, bookingData string) error {

	resp, err := c.zinc.GetApiVVersionBookingReserveDirectionDateTime(ctx, "1.0", direction, date, t)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get reserved datetime")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		c.logger.Info().Msg("No booking found, releasing reservation...")
		_, e := c.buyer.Release(userData, bookingData)
		if e != nil {
			c.logger.Error().Err(e).Msg("Failed to release")
			return err
		}
		return nil
	}

	content, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		c.logger.Error().Err(readErr).Msg("Failed to read response from http response from booking reserve get endpoint")
		return readErr
	}
	var data zinc.BookingPrincipalRes
	er := json.Unmarshal(content, &data)
	if er != nil {
		c.logger.Error().Err(er).
			Str("content", string(content)).
			Msg("Failed to decode response from CDC endpoint")
		return er
	}

	reserved, err := c.zinc.PostApiVVersionBookingBuyingId(ctx, "1.0", data.Id)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get buying id")
		return err
	}
	defer reserved.Body.Close()

	if reserved.StatusCode != 200 {
		rErr := fmt.Errorf("failed to get mark booking as buying")
		c.logger.Error().Err(rErr).Msg("failed to get mark booking as buying")
		return rErr
	}

	_, readErr = io.ReadAll(reserved.Body)
	if readErr != nil {
		c.logger.Error().Err(readErr).Msg("Failed to read response from http response from booking reserve get endpoint")
		return readErr
	}

	p := Passenger{
		FullName:       *data.Passenger.FullName,
		Gender:         *data.Passenger.Gender,
		PassportExpiry: fmt.Sprintf("%sT00:00:00", lib.ZincToHeliumDate(*data.Passenger.PassportExpiry)),
		PassportNumber: *data.Passenger.PassportNumber,
	}

	c.logger.Info().Any("passenger", p).Msg("Passenger")
	buy, bookingNo, ticketNo, err := c.buyer.Buy(userData, bookingData, p)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to buy")
		return err
	}

	reader := bytes.NewReader(buy)
	contentType, rr, err := CreateForm(map[string]io.Reader{
		"file": reader,
	})
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to create form")
		return err
	}
	completed, err := c.zinc.PostApiVVersionBookingCompleteIdWithBody(ctx, "1.0", data.Id, &zinc.PostApiVVersionBookingCompleteIdParams{
		BookingNo: &bookingNo,
		TicketNo:  &ticketNo,
	}, contentType, rr)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get buying id")
		return err
	}
	defer completed.Body.Close()

	if completed.StatusCode != 200 {
		rErr := fmt.Errorf("failed to makr booking as complete")
		c.logger.Error().Err(rErr).Msg("failed to get mark booking as complete")
		return rErr
	}

	return nil
}
