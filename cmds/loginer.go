package cmds

import (
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/pool"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/urfave/cli/v2"
)

// Loginer is a one-shot command that reconciles the multi-account KTMB userData
// pool (for helium pollee jobs) against the configured accounts. It is separate
// from the reserver/enricher single-session loginer and exits when done.
func (state *State) Loginer(c *cli.Context) error {
	state.Logger.Info().Msg("Starting Loginer (userData pool reconcile)")

	ctx := c.Context
	ktmbConfig := state.Config.Ktmb

	var creds []config.Credential
	logins := state.Config.Pool.Logins
	if logins == "" {
		state.Logger.Error().Msg("pool.logins (ATOMI_POOL__LOGINS) is empty")
		return errors.New("no pool logins configured")
	}
	if err := json.Unmarshal([]byte(logins), &creds); err != nil {
		state.Logger.Error().Err(err).Msg("Failed to parse pool.logins as JSON array of {email,password}")
		return err
	}
	if len(creds) == 0 {
		state.Logger.Error().Msg("pool.logins parsed to zero accounts")
		return errors.New("no pool logins configured")
	}

	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, nil)
	mainRedis := otelredis.New(state.Config.Cache["main"])
	enc := encryptor.NewSymEncryptor[string](state.Config.Encryptor.Key, state.Logger)

	p := pool.New(&k, &mainRedis, state.Logger, state.Config.Pool.Key, enc, creds)

	if err := p.Sync(ctx); err != nil {
		state.Logger.Error().Err(err).Msg("Loginer failed")
		return err
	}

	// The pool is reconciled once on startup. The golang-chart only renders a
	// Deployment (a process that exits would restart-loop), so idle here until
	// the pod is signalled. Restart the deployment to re-sync after a config change.
	state.Logger.Info().Msg("Loginer complete; pool synced. Idling until shutdown (restart to re-sync)")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigCh:
	}
	state.Logger.Info().Msg("Loginer shutting down")
	return nil
}
