package main

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/cmds"
	"github.com/AtomiCloud/nitroso-tin/lib/auth"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/urfave/cli/v2"
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

	cfgApp := cfg.App

	cred, authErr := auth.NewDescopeM2MCredentialProvider(cfg.Auth.Descope)
	if authErr != nil {
		panic(authErr)
	}

	psd := fmt.Sprintf("%s.%s.%s", cfgApp.Platform, cfgApp.Service, cfgApp.Module)

	state := cmds.State{
		Landscape:        landscape,
		Config:           cfg,
		OtelConfigurator: &otelConfigurator,
		Logger:           &logger,
		Credential:       cred,
		Psd:              psd,
	}

	logger.Debug().Any("cfg", cfg).Msg("Loaded config")

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
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
