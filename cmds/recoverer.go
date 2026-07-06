package cmds

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/AtomiCloud/nitroso-tin/lib"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/recoverer"
	"github.com/AtomiCloud/nitroso-tin/lib/session"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/google/uuid"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func (state *State) buildRecoverer() (*recoverer.Client, *zinc.Client, error) {
	ktmbConfig := state.Config.Ktmb
	buyerCfg := state.Config.Buyer

	mainRedis := otelredis.New(state.Config.Cache["main"])
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, ktmbConfig.Proxy, ktmb.WarmConfig{})

	endpoint := fmt.Sprintf("%s://%s:%s", buyerCfg.Scheme, buyerCfg.Host, buyerCfg.Port)
	zClient, err := zinc.NewClient(endpoint,
		zinc.WithHTTPClient(otelhttp.DefaultClient),
		zinc.WithRequestEditorFn(state.Credential.RequestEditor()))
	if err != nil {
		state.Logger.Error().Err(err).Msg("Failed to create zinc client")
		return nil, nil, err
	}

	loginEncr := encryptor.NewSymEncryptor[ktmb.LoginRes](state.Config.Encryptor.Key, state.Logger)
	recoverEncr := encryptor.NewSymEncryptor[lib.RecoverDto](state.Config.Encryptor.Key, state.Logger)

	s := session.New(&k, &mainRedis, state.Logger, state.Config.Ktmb.LoginKey, loginEncr)

	client := recoverer.New(k, &s, &mainRedis, zClient, recoverEncr, state.OtelConfigurator,
		state.Logger, state.Config.Recoverer, state.Config.Enricher, state.Psm, state.Location)
	return client, zClient, nil
}

// Recoverer runs the hourly drain daemon
func (state *State) Recoverer(c *cli.Context) error {
	state.Logger.Info().Msg("Starting Recoverer")

	client, _, err := state.buildRecoverer()
	if err != nil {
		return err
	}

	err = client.Start(c.Context)
	if err != nil {
		state.Logger.Error().Err(err).Msg("Recoverer failed")
		return err
	}
	return nil
}

// Recover manually recovers bookings matching passport + date + time +
// direction: looks them up via zinc, confirms interactively, parks them in
// Recovering and runs the standard classification synchronously
func (state *State) Recover(c *cli.Context) error {
	args := c.Args()
	passport := args.Get(0)
	date := args.Get(1)
	t := args.Get(2)
	direction := args.Get(3)

	if passport == "" || date == "" || t == "" || (direction != "JToW" && direction != "WToJ") {
		return fmt.Errorf("usage: recover <passport> <date dd-MM-yyyy> <time HH:mm:ss> <direction JToW|WToJ>")
	}

	client, zClient, err := state.buildRecoverer()
	if err != nil {
		return err
	}

	resp, err := zClient.GetApiVVersionBooking(c.Context, "1.0", &zinc.GetApiVVersionBookingParams{
		PassportNumber: &passport,
		Date:           &date,
		Time:           &t,
		Direction:      &direction,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to search bookings: status %d: %s", resp.StatusCode, string(body))
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var bookings []zinc.BookingPrincipalRes
	if err := json.Unmarshal(content, &bookings); err != nil {
		return err
	}
	if len(bookings) == 0 {
		fmt.Println("No bookings found for the given passport/date/time/direction.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	for _, b := range bookings {
		status := lib.Deref(b.Status)
		fmt.Printf("\nBooking %s\n  passenger: %s (%s)\n  trip:      %s %s %s\n  status:    %s\n  bookingNo: %s ticketNo: %s\n",
			b.Id.String(), lib.Deref(b.Passenger.FullName), lib.Deref(b.Passenger.PassportNumber),
			lib.Deref(b.Direction), lib.Deref(b.Date), lib.Deref(b.Time), status, lib.Deref(b.BookingNo), lib.Deref(b.TicketNo))

		if status != "Pending" && status != "Buying" && status != "Recovering" {
			fmt.Printf("  -> status %s is not recoverable, skipping\n", status)
			continue
		}

		fmt.Print("  recover this booking? [y/N]: ")
		answer, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("  -> skipped")
			continue
		}

		if status == "Pending" {
			if err := postTransition(c, b.Id, "buying", zClient.PostApiVVersionBookingBuyingId); err != nil {
				return err
			}
			status = "Buying"
		}
		if status == "Buying" {
			if err := postTransition(c, b.Id, "recovering", zClient.PostApiVVersionBookingRecoveringId); err != nil {
				return err
			}
		}

		// the booking was searched by passport+date+time+direction, so its own
		// fields carry the same values the CLI args hold
		dto := recoverer.ReconstructDto(b)

		fmt.Println("  -> running recovery classification...")
		if err := client.ProcessItem(c.Context, dto); err != nil {
			state.Logger.Error().Err(err).Str("bookingId", dto.BookingId).Msg("Manual recovery failed")
			return err
		}
		fmt.Println("  -> resolved (see logs for the outcome)")
	}
	return nil
}

func postTransition(c *cli.Context, id uuid.UUID, name string,
	post func(ctx context.Context, version string, id uuid.UUID, reqEditors ...zinc.RequestEditorFn) (*http.Response, error)) error {
	resp, err := post(c.Context, "1.0", id)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to mark booking as %s: status %d: %s", name, resp.StatusCode, string(body))
	}
	return nil
}
