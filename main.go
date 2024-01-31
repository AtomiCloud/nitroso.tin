package main

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/cmds"
	"github.com/AtomiCloud/nitroso-tin/lib/auth"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
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
				Name: "test",
				Action: func(context *cli.Context) error {
					ktmbConfig := state.Config.Ktmb
					state.Logger.Info().Str("request", ktmbConfig.RequestSignature).Msg("Starting Test")
					state.Logger.Info().Any("ktmbConfig", ktmbConfig).Msg("Configurations")
					k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, ktmbConfig.Proxy)
					login, e := k.Login("xxluna001@gmail.com", "Pokemon1288!")
					if e != nil {
						state.Logger.Error().Err(e).Msg("Failed to login")
						return e
					}
					state.Logger.Info().Any("login", login).Msg("Login success")
					sd, e := k.StationsAll(login.Data.UserData)
					if e != nil {
						state.Logger.Error().Err(e).Msg("Failed to login")
						return e
					}
					state.Logger.Info().Any("sd", sd).Msg("StationsAll success")
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
