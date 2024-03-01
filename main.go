package main

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/cmds"
	"github.com/AtomiCloud/nitroso-tin/lib/auth"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/terminator"
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
				Name:   "terminator",
				Action: state.Terminator,
			},
			{
				Name: "test",
				Action: func(context *cli.Context) error {
					ktmbConfig := state.Config.Ktmb
					enricherConfig := state.Config.Enricher

					k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, ktmbConfig.Proxy)
					term := terminator.NewTerminator(k, state.Logger, enricherConfig)

					e := term.Terminate(terminator.BookingTermination{
						BookingNo: "KST240255503142",
						TicketNo:  "TST240247666907",
					})
					if e != nil {
						state.Logger.Err(e).Msg("Failed to terminate")
						return e
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
