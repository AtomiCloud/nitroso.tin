package cmds

import (
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/buyer"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/reserver"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func (state *State) Buyer(c *cli.Context) error {
	state.Logger.Info().Msg("Starting Buyer")

	ktmbConfig := state.Config.Ktmb
	buyerCfg := state.Config.Buyer
	ctx := c.Context

	mainRedis := otelredis.New(state.Config.Cache["main"])
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, nil)
	encr := encryptor.NewSymEncryptor[reserver.ReserveDto](state.Config.Encryptor.Key, state.Logger)

	endpoint := fmt.Sprintf("%s://%s:%s", buyerCfg.Scheme, buyerCfg.Host, buyerCfg.Port)

	zClient, er := zinc.NewClient(endpoint,
		zinc.WithHTTPClient(otelhttp.DefaultClient),
		zinc.WithRequestEditorFn(state.Credential.RequestEditor()))
	if er != nil {
		state.Logger.Error().Err(er).Msg("Failed to create zinc client")
		return er
	}

	b := buyer.NewBuyer(k, state.Logger, state.Config.Buyer.ContactNumber, state.Config.Buyer.SleepBuffer)
	client := buyer.New(&b, &mainRedis, state.OtelConfigurator, state.Logger, state.Config.Stream, state.Config.Buyer, state.Psm, zClient, encr)

	err := client.Start(ctx)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Buyer failed")
		return err
	}
	return nil
}
