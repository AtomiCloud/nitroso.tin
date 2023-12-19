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
	redis            *otelredis.OtelRedis
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	streamsCfg       config.StreamConfig
	buyerCfg         config.BuyerConfig
	psd              string
	zinc             *zinc.Client
	encr             encryptor.Encryptor[reserver.ReserveDto]
}

var baseDelay = 1 * time.Second

func createForm(values map[string]io.Reader) (s string, reader io.Reader, err error) {
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

func New(buyer *Buyer, redis *otelredis.OtelRedis, otelConfigurator *telemetry.OtelConfigurator, logger *zerolog.Logger,
	streamsCfg config.StreamConfig, buyerCfg config.BuyerConfig, psd string, zinc *zinc.Client, enrc encryptor.Encryptor[reserver.ReserveDto]) *Client {
	return &Client{
		buyer:            buyer,
		redis:            redis,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		streamsCfg:       streamsCfg,
		buyerCfg:         buyerCfg,
		psd:              psd,
		zinc:             zinc,
		encr:             enrc,
	}
}

func (c *Client) createGroup(ctx context.Context) {
	status := c.redis.XGroupCreateMkStream(ctx, c.streamsCfg.Reserver, c.buyerCfg.Group, "0")
	c.logger.Info().Msg("Group Create Status: " + status.String())
}

func (c *Client) Start(ctx context.Context, consumerId string) error {
	maxCounter := c.buyerCfg.BackoffLimit

	errorCounter := 0

	c.createGroup(ctx)
	for {
		shouldExit, err := c.loop(ctx, consumerId)
		if err != nil {
			if errorCounter >= maxCounter {
				c.logger.Error().Err(err).Msg("Failed all backoff attempts, exiting...")
				return err
			}
			secRetry := math.Pow(2, float64(errorCounter))
			c.logger.Info().Msgf("Retrying operation in %f seconds\n", secRetry)
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

func (c *Client) loop(ctx context.Context, consumerId string) (bool, error) {
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
	tracer := otel.Tracer(c.psd)
	ctx, span := tracer.Start(ctx, "Buyer")
	defer span.End()

	c.logger.Info().Ctx(ctx).Msg("Waiting for reserver message...")
	err = c.redis.StreamGroupRead(ctx, tracer, c.streamsCfg.Reserver, c.buyerCfg.Group, consumerId, func(ctx context.Context, message json.RawMessage) error {
		c.logger.Info().Ctx(ctx).Msg("Received reserver emitted signal")
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
		c.logger.Error().Err(err).Msg("Failed to read from redis stream in buyer (from reserver)")
		return false, err
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
		c.logger.Info().Msg("No booking found, skipping...")
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

	reserved, err := c.zinc.PostApiVVersionBookingBuyingId(ctx, "1.0", *data.Id)
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
	buy, err := c.buyer.Buy(userData, bookingData, p)
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to buy")
		return err
	}

	reader := bytes.NewReader(buy)
	contentType, rr, err := createForm(map[string]io.Reader{
		"file": reader,
	})
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to create form")
		return err
	}
	completed, err := c.zinc.PostApiVVersionBookingCompleteIdWithBody(ctx, "1.0", *data.Id, contentType, rr)
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
