package main

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/cmds"
	"github.com/AtomiCloud/nitroso-tin/lib/auth"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
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
				Name:   "terminator",
				Action: state.Terminator,
			},
			{
				Name: "resend",
				Action: func(context *cli.Context) error {

					mainRedis := otelredis.New(state.Config.Cache["main"])

					r := mainRedis.LPush(context.Context, "buyqueue", ``)
					cmd, e := r.Result()
					if e != nil {
						state.Logger.Err(e).Msg("failed to push")
						return e
					}
					state.Logger.Info().Int64("cmd", cmd).Msg("succeeded")
					return nil
				},
			},
			{
				Name: "decrypt",
				Action: func(context *cli.Context) error {
					eKey := os.Getenv("ENCRYPTION_KEY")
					encr := encryptor.NewSymEncryptor[enricher.FindStore](eKey, state.Logger)
					find, e := encr.DecryptAny(context.Args().First())
					if e != nil {
						state.Logger.Err(e).Msg("failed to decrypt")
					}
					state.Logger.Info().Any("find", find).Msg("decrypted")
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
