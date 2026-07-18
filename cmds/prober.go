package cmds

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/prober"
	"github.com/AtomiCloud/nitroso-tin/lib/reserver"
	"github.com/AtomiCloud/nitroso-tin/lib/session"
	"github.com/urfave/cli/v2"
)

func (state *State) Prober(c *cli.Context) (runErr error) {
	profiler, err := prober.StartProfiler(c.Bool("pprof"), os.Stdout)
	if err != nil {
		return fmt.Errorf("start prober profiler: %w", err)
	}
	if profiler != nil {
		defer func() {
			if err := profiler.Stop(); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("stop prober profiler: %w", err))
			}
		}()
	}

	targets, err := prober.ParseTargets(c.String("data"))
	if err != nil {
		return err
	}
	interval := c.Int("interval")
	if interval <= 0 {
		return cli.Exit("interval must be positive", 1)
	}
	probeUntil := time.Now().Add(time.Duration(interval) * time.Second)
	// Reserve may consume its full 60-second timeout and compensation may need
	// another 60 seconds. Keep the process alive for cleanup after probing stops.
	ctx, cancel := context.WithTimeout(c.Context, time.Duration(interval+120)*time.Second)
	defer cancel()

	mainRedis := otelredis.New(state.Config.Cache["main"])
	streamRedis := otelredis.New(state.Config.Cache["stream"])
	ktmbConfig := state.Config.Ktmb
	warmSize := len(targets)
	if warmSize < ktmbConfig.WarmPoolSize {
		warmSize = ktmbConfig.WarmPoolSize
	}
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, ktmbConfig.Proxy,
		ktmb.WarmConfig{PoolSize: warmSize, IntervalMs: ktmbConfig.WarmIntervalMs, DnsRefreshMs: ktmbConfig.DnsRefreshMs})
	storeEncryptor := encryptor.NewSymEncryptor[enricher.FindStore](state.Config.Encryptor.Key, state.Logger)
	sessionEncryptor := encryptor.NewSymEncryptor[ktmb.LoginRes](state.Config.Encryptor.Key, state.Logger)
	sharedSession := session.New(&k, &mainRedis, state.Logger, ktmbConfig.LoginKey, sessionEncryptor)
	finder := enricher.New(k, &sharedSession, state.Logger)
	store := prober.NewStore(&mainRedis, &sharedSession, &finder, storeEncryptor, state.Config.Enricher,
		ktmbConfig.LoginKey, state.Ps+":prober:session-dead", state.Logger)
	reserveEncryptor := encryptor.NewSymEncryptor[reserver.ReserveDto](state.Config.Encryptor.Key, state.Logger)
	runner := prober.NewRunner(&k, store, &mainRedis, &streamRedis, reserveEncryptor, state.Config.Prober,
		state.Config.Stream, ktmbAppInfo, state.Ps, state.Psm, state.Location, state.Logger)
	state.Logger.Info().Int("slots", len(targets)).Int64("epoch", c.Int64("epoch")).Bool("dryRun", state.Config.Prober.DryRun).
		Msg("Starting epoch prober Job")
	return runner.Run(ctx, targets, c.Int64("epoch"), c.String("job"), probeUntil)
}
