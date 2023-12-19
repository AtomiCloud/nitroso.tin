package cmds

import (
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/rs/xid"
	"github.com/urfave/cli/v2"
)

func (state *State) Enricher(c *cli.Context) error {
	state.Logger.Info().Msg("Starting Enricher")

	uniqueID := xid.New().String()
	ktmbConfig := state.Config.Ktmb
	ctx := c.Context

	ch := make(chan string)
	rds := otelredis.New(state.Config.Cache["main"])
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger)
	encr := encryptor.NewSymEncryptor[enricher.FindStore](state.Config.Encryptor.Key, state.Logger)

	client := enricher.New(k, state.Logger)
	trigger := enricher.NewTrigger(ch, state.Logger, &rds, state.Config.Stream, state.Config.Enricher, state.OtelConfigurator, state.Psd)
	en := enricher.NewEnricher(&client, trigger, state.Logger, encr, &rds,
		state.Config.Enricher, state.Config.Stream, state.Psd, ch, state.OtelConfigurator)

	err := en.Start(ctx, uniqueID)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Enricher failed")
		return err
	}
	return nil
}
