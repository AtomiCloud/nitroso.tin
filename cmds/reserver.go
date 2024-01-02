package cmds

import (
	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/reserver"
	"github.com/rs/xid"
	"github.com/urfave/cli/v2"
	"time"
)

func (state *State) Reserver(c *cli.Context) error {
	state.Logger.Info().Msg("Starting Reserver")

	loc, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		state.Logger.Error().Err(err).Msg("Failed to load location")
		return err
	}

	countConsumerId := xid.New().String()
	loginConsumerId := xid.New().String()
	ktmbConfig := state.Config.Ktmb
	ctx := c.Context

	appInfo := "{\"DeviceName\":\"Google\",\"OperatingSystemName\":\"Android\",\"OperatingSystemVersion\":\"13\",\"AppVersion\":\"1.4.1\"}"

	mainRedis := otelredis.New(state.Config.Cache["main"])
	streamRedis := otelredis.New(state.Config.Cache["stream"])
	liveRedis := otelredis.New(state.Config.Cache["live"])
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger)
	encr := encryptor.NewSymEncryptor[enricher.FindStore](state.Config.Encryptor.Key, state.Logger)
	rEncr := encryptor.NewSymEncryptor[reserver.ReserveDto](state.Config.Encryptor.Key, state.Logger)

	countReader := count.New(&mainRedis, state.Logger, state.Ps)
	countToDiff := make(chan reserver.Count)
	diffToReserve := make(chan reserver.Diff)
	countToReserve := make(chan reserver.Count)
	loginToReserve := make(chan reserver.LoginStore)

	retriever := reserver.NewRetriever(&mainRedis, encr, state.Logger, state.Config.Enricher)

	differ := reserver.NewDiffer(countToDiff, diffToReserve, &liveRedis, state.Logger)
	countSyncer := reserver.NewCountSyncer(countToDiff, countToReserve, &streamRedis, state.OtelConfigurator, state.Logger, state.Psm,
		state.Config.Stream, state.Config.Reserver, countReader)
	loginSyncer := reserver.NewLoginSyncer(loginToReserve, &streamRedis, retriever, state.OtelConfigurator,
		state.Logger, state.Psm, state.Ps, state.Config.Stream, state.Config.Reserver)

	client := reserver.New(k, state.Logger, &mainRedis, rEncr, state.Config.Reserver, state.Config.Stream, appInfo,
		state.OtelConfigurator, state.Psm, loc, loginToReserve, countToReserve, diffToReserve)

	go func() {
		e := loginSyncer.Start(ctx, loginConsumerId)
		if e != nil {
			state.Logger.Error().Err(e).Msg("Login syncer failed")
			panic(e)
		}
	}()

	go func() {
		e := countSyncer.Start(ctx, countConsumerId)
		if e != nil {
			state.Logger.Error().Err(e).Msg("Count syncer failed")
			panic(e)
		}
	}()

	go differ.Start(ctx)

	err = client.Start(ctx)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Reserver failed")
		return err
	}

	return nil
}
