package cmds

import (
	"github.com/AtomiCloud/nitroso-tin/lib/count"
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

	ch := make(chan enricher.TriggerMessage)
	mainRedis := otelredis.New(state.Config.Cache["main"])
	streamRedis := otelredis.New(state.Config.Cache["stream"])
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, ktmbConfig.Proxy)
	encr := encryptor.NewSymEncryptor[enricher.FindStore](state.Config.Encryptor.Key, state.Logger)

	client := enricher.New(k, state.Logger)

	trigger := enricher.NewTrigger(ch, state.Logger, &streamRedis, state.Config.Stream, state.Config.Enricher, state.OtelConfigurator, state.Psm, state.Location)

	counterReader := count.New(state.Config.Buffer, &mainRedis, state.Logger, state.Ps, state.Location)

	en := enricher.NewEnricher(&client, trigger, state.Logger, encr, &mainRedis, &streamRedis,
		state.Config.Enricher, state.Config.Stream, state.Psm, ch, state.OtelConfigurator, counterReader)

	err := en.Start(ctx, uniqueID)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Enricher failed")
		return err
	}

	return nil
}
