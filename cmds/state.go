package cmds

import (
	"github.com/AtomiCloud/nitroso-tin/lib/auth"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/rs/zerolog"
)

type State struct {
	Landscape        string
	Config           config.RootConfig
	OtelConfigurator *telemetry.OtelConfigurator
	Logger           *zerolog.Logger
	Credential       auth.CredentialsProvider
	Psd              string
}
