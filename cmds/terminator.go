package cmds

import (
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/terminator"
	"github.com/urfave/cli/v2"
)

func (state *State) Terminator(c *cli.Context) error {
	state.Logger.Info().Msg("Starting Terminator")

	ktmbConfig := state.Config.Ktmb
	termConfig := state.Config.Terminator
	enricherConfig := state.Config.Enricher
	ctx := c.Context

	streamRedis := otelredis.New(state.Config.Cache["stream"])
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, nil)

	term := terminator.NewTerminator(k, state.Logger, enricherConfig)
	client := terminator.New(&term, &streamRedis, state.OtelConfigurator, termConfig, state.Logger, state.Psm)

	err := client.Start(ctx)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Terminator failed")
		return err
	}
	return nil
}
