package cmds

import (
	"github.com/AtomiCloud/nitroso-tin/lib/cdc"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/rs/xid"
	"github.com/urfave/cli/v2"
)

func (state *State) Cdc(c *cli.Context) error {
	state.Logger.Info().Msg("Starting Count Syncer")

	mainRedis := otelredis.New(state.Config.Cache["main"])
	streamRedis := otelredis.New(state.Config.Cache["stream"])

	cdc := cdc.NewCdc(&mainRedis, &streamRedis, state.Config.Cdc, state.Config.Stream, state.Logger, state.OtelConfigurator, state.Psm, state.Ps, state.Credential)

	uniqueID := xid.New().String()

	err := cdc.Start(c.Context, uniqueID)
	if err != nil {
		return err
	}
	return nil
}
