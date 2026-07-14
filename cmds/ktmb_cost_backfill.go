package cmds

import (
	"fmt"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmbcost"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/session"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func (state *State) KtmbCostBackfill(c *cli.Context) error {
	state.Logger.Info().
		Bool("dryRun", c.Bool("dry-run")).
		Int("max", c.Int("max")).
		Int("pageSize", c.Int("page-size")).
		Msg("Starting KTMB actual-cost backfill")

	ktmbConfig := state.Config.Ktmb
	mainRedis := otelredis.New(state.Config.Cache["main"])
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, ktmbConfig.Proxy, ktmb.WarmConfig{})
	loginEncr := encryptor.NewSymEncryptor[ktmb.LoginRes](state.Config.Encryptor.Key, state.Logger)
	s := session.New(&k, &mainRedis, state.Logger, ktmbConfig.LoginKey, loginEncr)

	buyerConfig := state.Config.Buyer
	endpoint := fmt.Sprintf("%s://%s:%s", buyerConfig.Scheme, buyerConfig.Host, buyerConfig.Port)
	zClient, err := zinc.NewClient(endpoint,
		zinc.WithHTTPClient(otelhttp.DefaultClient),
		zinc.WithRequestEditorFn(state.Credential.RequestEditor()))
	if err != nil {
		return fmt.Errorf("create zinc client: %w", err)
	}

	client := ktmbcost.New(zClient, &k, &s, state.Logger, state.Config.Enricher.Email, state.Config.Enricher.Password, ktmbcost.Options{
		DryRun:      c.Bool("dry-run"),
		Max:         c.Int("max"),
		PageSize:    c.Int("page-size"),
		SleepBuffer: time.Duration(buyerConfig.SleepBuffer) * time.Second,
	})
	summary, err := client.Run(c.Context)
	state.Logger.Info().
		Int("fetched", summary.Fetched).
		Int("attempted", summary.Attempted).
		Int("updated", summary.Updated).
		Int("failed", summary.Failed).
		Int("dryRun", summary.DryRun).
		Msg("KTMB actual-cost backfill finished")
	return err
}
