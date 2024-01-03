package main

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/cmds"
	"github.com/AtomiCloud/nitroso-tin/lib/auth"
	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/urfave/cli/v2"
	"log"
	"os"
	"time"
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

	cfgApp := cfg.App

	cred, authErr := auth.NewDescopeM2MCredentialProvider(cfg.Auth.Descope)
	if authErr != nil {
		panic(authErr)
	}

	psm := fmt.Sprintf("%s.%s.%s", cfgApp.Platform, cfgApp.Service, cfgApp.Module)
	ps := fmt.Sprintf("%s.%s", cfgApp.Platform, cfgApp.Service)

	loc, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load location")
		panic(err)
	}

	state := cmds.State{
		Landscape:        landscape,
		Config:           cfg,
		OtelConfigurator: &otelConfigurator,
		Logger:           &logger,
		Credential:       cred,
		Psm:              psm,
		Ps:               ps,
		Location:         loc,
	}

	app := &cli.App{
		Name: "nitroso-tin",
		Commands: []*cli.Command{
			{
				Name:   "cdc",
				Action: state.Cdc,
			},
			{
				Name:   "poller",
				Action: state.Poller,
			},
			{
				Name:   "enricher",
				Action: state.Enricher,
			},
			{
				Name:   "reserver",
				Action: state.Reserver,
			},
			{
				Name:   "buyer",
				Action: state.Buyer,
			},
			{
				Name: "count",
				Action: func(c *cli.Context) error {
					state.Logger.Info().Msg("Starting Count")

					rds := otelredis.New(state.Config.Cache["main"])
					cr := count.New(&rds, state.Logger, state.Ps, state.Location)

					exist, count, e := cr.GetCount(c.Context, time.Now())
					if e != nil {
						state.Logger.Error().Err(e).Msg("Failed to get count")
						return e
					}
					state.Logger.Info().Bool("exist", exist).Any("count", count).Msg("Count")
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
