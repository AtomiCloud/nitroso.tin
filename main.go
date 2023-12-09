package main

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/AtomiCloud/nitroso-tin/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/xid"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"log"
	"os"
)

func main() {

	// Setup

	landscape := os.Getenv("LANDSCAPE")
	if landscape == "" {
		landscape = "lapras"
	}
	baseConfig := os.Getenv("BASE_CONFIG")
	if baseConfig == "" {
		baseConfig = "./config/app"
	}

	loader := config.Loader{
		Landscape:  landscape,
		BaseConfig: baseConfig,
	}
	cfg, cfgErr := loader.Load()
	if cfgErr != nil {
		panic(cfgErr)
	}

	metricConfigurator := telemetry.MetricConfigurator{Cfg: cfg.Otel.Metric}
	traceConfigurator := telemetry.TraceConfigurator{Cfg: cfg.Otel.Trace}
	otelConfigurator := telemetry.OtelConfigurator{
		App:    cfg.App,
		Otel:   cfg.Otel,
		Trace:  traceConfigurator,
		Metric: metricConfigurator,
	}
	logFactory := telemetry.LoggerFactory{
		Cfg: cfg.Otel.Log,
	}
	logger, loggerErr := logFactory.Get()
	if loggerErr != nil {
		panic(loggerErr)
	}

	redis := otelredis.New(cfg.Cache["MAIN"])

	groupName := "pikachu"
	streamName := "sultur"

	app := &cli.App{
		Name: "nitroso-tin",
		Commands: []*cli.Command{
			{
				Name: "start",
				Action: func(c *cli.Context) error {
					return nil
				},
			},
			{
				Name: "stream-write",
				Action: func(c *cli.Context) error {

					ctx := c.Context
					logger.Info().Msg("Starting Publisher...")
					reader := bufio.NewReader(os.Stdin)

					for {
						b, topLevelErr := func() (bool, error) {
							shutdown, err := otelConfigurator.Configure(ctx)
							if err != nil {
								return false, err
							}
							defer func() {
								deferErr := shutdown(ctx)
								if deferErr != nil {
									panic(deferErr)
								}
							}()
							tracer := otel.Tracer("nitroso-tin")
							ctx, span := tracer.Start(ctx, "publisher")
							defer span.End()

							// Application start
							logger.Info().Ctx(ctx).Msg("Enter a value: ")
							input, _ := reader.ReadString('\n')

							r, err := redis.StreamAdd(ctx, tracer, streamName, input)
							if err != nil {
								logger.Error().Err(err).Msg("Failed to publish")
								return false, err
							}
							logger.Info().Ctx(ctx).Msg("Publish Result: " + r.String())
							logger.Info().Ctx(ctx).Msg("Published: " + input)

							return false, nil
						}()

						if topLevelErr != nil {
							logger.Error().Err(topLevelErr).Msg("Failed during publishing")
						}
						if b {
							break
						}
					}

					return nil
				},
			},
			{
				Name: "stream-read",
				Action: func(c *cli.Context) error {
					logger.Info().Msg("Starting Subscriber...")
					ctx := c.Context

					// Always create group
					status := redis.XGroupCreateMkStream(ctx, streamName, groupName, "0")
					logger.Info().Msg("Group Create Status: " + status.String())

					uniqueID := xid.New().String()

					for {
						b, topLevelErr := func() (bool, error) {
							shutdown, err := otelConfigurator.Configure(ctx)
							if err != nil {
								return false, err
							}
							defer func() {
								deferErr := shutdown(ctx)
								if deferErr != nil {
									panic(deferErr)
								}
							}()
							tracer := otel.Tracer("nitroso-tin")
							ctx, span := tracer.Start(ctx, "subscriber")
							defer span.End()

							// Application start
							logger.Info().Ctx(ctx).Msg("Waiting for message...")
							err = redis.StreamGroupRead(ctx, tracer, streamName, groupName, uniqueID, func(ctx context.Context, message json.RawMessage) error {
								logger.Info().Ctx(ctx).Msg("Received: " + string(message))
								return nil
							})
							if err != nil {
								logger.Error().Err(err).Msg("Failed to read")
								return false, err
							}
							return false, nil
						}()

						if topLevelErr != nil {
							logger.Error().Err(topLevelErr).Msg("Failed during publishing")
						}
						if b {
							break
						}
					}

					return nil
				},
			},
			{
				Name: "pub",
				Action: func(c *cli.Context) error {

					ctx := c.Context
					logger.Info().Msg("Starting Publisher...")
					reader := bufio.NewReader(os.Stdin)

					for {
						b, topLevelErr := func() (bool, error) {
							shutdown, err := otelConfigurator.Configure(ctx)
							if err != nil {
								return false, err
							}
							defer func() {
								deferErr := shutdown(ctx)
								if deferErr != nil {
									panic(deferErr)
								}
							}()
							tracer := otel.Tracer("nitroso-tin")
							ctx, span := tracer.Start(ctx, "publisher")
							defer span.End()

							// Application start
							logger.Info().Ctx(ctx).Msg("Enter a value: ")
							input, _ := reader.ReadString('\n')

							r, err := redis.Pub(ctx, tracer, logger, "sample", input)
							if err != nil {
								logger.Error().Err(err).Msg("Failed to publish")
								return false, err
							}
							logger.Info().Ctx(ctx).Msg("Publish Result: " + r.String())
							//redis.Publish(ctx, "sample", input)
							logger.Info().Ctx(ctx).Msg("Published: " + input)

							return false, nil
						}()

						if topLevelErr != nil {
							logger.Error().Err(topLevelErr).Msg("Failed during publishing")
						}
						if b {
							break
						}
					}

					return nil
				},
			},
			{
				Name: "sub",
				Action: func(c *cli.Context) error {
					logger.Info().Msg("Starting Subscriber...")
					ctx := c.Context
					//subscriber := redis.Sub(ctx, "sample")

					subscriber := redis.Sub(ctx, "sample")

					for {
						b, topLevelErr := func() (bool, error) {
							shutdown, err := otelConfigurator.Configure(ctx)
							if err != nil {
								return false, err
							}
							defer func() {
								deferErr := shutdown(ctx)
								if deferErr != nil {
									panic(deferErr)
								}
							}()
							tracer := otel.Tracer("nitroso-tin")
							ctx, span := tracer.Start(ctx, "subscriber")
							defer span.End()

							// Application start
							logger.Info().Ctx(ctx).Msg("Waiting for message...")
							err = subscriber.Recv(ctx, tracer, func(ctx context.Context, message json.RawMessage) error {
								logger.Info().Ctx(ctx).Msg("Received: " + string(message))
								return nil
							})
							if err != nil {
								return false, err
							}
							return false, nil
						}()

						if topLevelErr != nil {
							logger.Error().Err(topLevelErr).Msg("Failed during publishing")
						}
						if b {
							break
						}
					}

					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
