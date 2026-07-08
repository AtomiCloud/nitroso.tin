package cmds

import (
	"fmt"

	"github.com/AtomiCloud/nitroso-tin/lib/withdrawer"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func (state *State) buildWithdrawer() (*withdrawer.Client, error) {
	buyerCfg := state.Config.Buyer

	endpoint := fmt.Sprintf("%s://%s:%s", buyerCfg.Scheme, buyerCfg.Host, buyerCfg.Port)
	zClient, err := zinc.NewClient(endpoint,
		zinc.WithHTTPClient(otelhttp.DefaultClient),
		zinc.WithRequestEditorFn(state.Credential.RequestEditor()))
	if err != nil {
		state.Logger.Error().Err(err).Msg("Failed to create zinc client")
		return nil, err
	}

	client := withdrawer.New(zClient, state.OtelConfigurator, state.Logger, state.Config.Withdrawer)
	return client, nil
}

// Withdrawer runs the nightly daemon that approves Pending withdrawals and
// re-drives Processing withdrawals stuck without a payout confirmation
func (state *State) Withdrawer(c *cli.Context) error {
	state.Logger.Info().Msg("Starting Withdrawer")

	client, err := state.buildWithdrawer()
	if err != nil {
		return err
	}

	err = client.Start(c.Context)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Withdrawer failed")
		return err
	}
	return nil
}
