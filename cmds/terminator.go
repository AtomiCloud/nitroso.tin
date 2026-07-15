package cmds

import (
	"fmt"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/session"
	"github.com/AtomiCloud/nitroso-tin/lib/terminator"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func (state *State) Terminator(c *cli.Context) error {
	state.Logger.Info().Msg("Starting Terminator")

	ktmbConfig := state.Config.Ktmb
	termConfig := state.Config.Terminator
	enricherConfig := state.Config.Enricher
	ctx := c.Context

	mainRedis := otelredis.New(state.Config.Cache["main"])
	streamRedis := otelredis.New(state.Config.Cache["stream"])

	encr := encryptor.NewSymEncryptor[ktmb.LoginRes](state.Config.Encryptor.Key, state.Logger)

	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, nil, ktmb.WarmConfig{PoolSize: ktmbConfig.WarmPoolSize, IntervalMs: ktmbConfig.WarmIntervalMs, DnsRefreshMs: ktmbConfig.DnsRefreshMs})
	s := session.New(&k, &mainRedis, state.Logger, state.Config.Ktmb.LoginKey, encr)

	buyerConfig := state.Config.Buyer
	endpoint := fmt.Sprintf("%s://%s:%s", buyerConfig.Scheme, buyerConfig.Host, buyerConfig.Port)
	zClient, err := zinc.NewClient(endpoint,
		zinc.WithHTTPClient(otelhttp.DefaultClient),
		zinc.WithRequestEditorFn(state.Credential.RequestEditor()))
	if err != nil {
		return fmt.Errorf("create zinc client: %w", err)
	}

	term := terminator.NewTerminator(&k, &s, zClient, state.Logger, enricherConfig)
	client := terminator.New(&term, &streamRedis, state.OtelConfigurator, termConfig, state.Logger, state.Psm)

	err = client.Start(ctx)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Terminator failed")
		return err
	}
	return nil
}
