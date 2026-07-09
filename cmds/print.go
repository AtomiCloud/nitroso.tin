package cmds

import (
	"fmt"
	"os"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/session"
	"github.com/urfave/cli/v2"
)

// PrintTicket re-downloads a single ticket PDF from KTMB and writes it to disk.
// It exists to restore a ticket file that was lost from object storage for an
// already-Completed booking, which neither `recover` (status-gated to
// Pending/Buying/Recovering) nor zinc's guarded complete endpoint can service.
//
// It reuses the same login session the recoverer/buyer use (session.Login reads
// the cached, encrypted token from Redis), so running it against a landscape's
// live account does not create a second KTMB session. PrintTicketPdf is
// idempotent on KTMB's side; the ticket must still belong to the enricher
// account and not have departed.
//
//	print-ticket <bookingNo> <ticketNo> <outPath>
func (state *State) PrintTicket(c *cli.Context) error {
	args := c.Args()
	bookingNo := args.Get(0)
	ticketNo := args.Get(1)
	outPath := args.Get(2)
	if bookingNo == "" || ticketNo == "" || outPath == "" {
		return fmt.Errorf("usage: print-ticket <bookingNo> <ticketNo> <outPath>")
	}

	ktmbConfig := state.Config.Ktmb
	mainRedis := otelredis.New(state.Config.Cache["main"])
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, ktmbConfig.Proxy, ktmb.WarmConfig{})
	loginEncr := encryptor.NewSymEncryptor[ktmb.LoginRes](state.Config.Encryptor.Key, state.Logger)
	s := session.New(&k, &mainRedis, state.Logger, ktmbConfig.LoginKey, loginEncr)

	state.Logger.Info().
		Str("bookingNo", bookingNo).
		Str("ticketNo", ticketNo).
		Msg("Reusing cached KTMB session to re-download ticket")

	userData, err := s.Login(c.Context, state.Config.Enricher.Email, state.Config.Enricher.Password)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Failed to obtain KTMB session")
		return err
	}

	pdf, err := k.PrintTicket(userData, bookingNo, ticketNo)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Failed to re-download ticket PDF")
		return err
	}

	if err := os.WriteFile(outPath, pdf, 0o600); err != nil {
		state.Logger.Error().Err(err).Str("path", outPath).Msg("Failed to write ticket PDF")
		return err
	}

	state.Logger.Info().Int("bytes", len(pdf)).Str("path", outPath).Msg("Ticket PDF written")
	return nil
}
