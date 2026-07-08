package withdrawer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	"github.com/robfig/cron"
	"github.com/rs/zerolog"
)

// Client sweeps zinc nightly for Pending withdrawals and approves each one
// (Pending -> Processing) via zinc's approve endpoint, one by one.
//
// SINGLE REPLICA ONLY. The sweep assumes it is the only approver: two
// replicas would race to approve the same Pending withdrawal. zinc guards the
// Pending -> Processing transition server-side (so a duplicate approve fails
// rather than double-pays), but concurrent replicas would still produce noisy
// spurious failures and skewed summaries. Keep replicaCount at 1 (see the
// Helm values); do not add an HPA.
type Client struct {
	zinc             *zinc.Client
	otelConfigurator *telemetry.OtelConfigurator
	logger           *zerolog.Logger
	config           config.WithdrawerConfig
}

func New(zClient *zinc.Client, otelConfigurator *telemetry.OtelConfigurator,
	logger *zerolog.Logger, cfg config.WithdrawerConfig) *Client {
	return &Client{
		zinc:             zClient,
		otelConfigurator: otelConfigurator,
		logger:           logger,
		config:           cfg,
	}
}

func (c *Client) Start(ctx context.Context) error {
	shutdown, err := c.otelConfigurator.Configure(ctx)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to configure telemetry")
		return err
	}
	defer func() {
		deferErr := shutdown(ctx)
		if deferErr != nil {
			panic(deferErr)
		}
	}()

	ch := make(chan struct{}, 1)

	// robfig/cron v1 evaluates specs in the runner's location; force UTC so the
	// nightly sweep fires at 00:00 UTC regardless of the pod's timezone.
	cr := cron.NewWithLocation(time.UTC)
	if err = cr.AddFunc(c.config.Cron, func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	}); err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("cron", c.config.Cron).Msg("Failed to schedule withdrawer cron")
		return err
	}
	cr.Start()
	defer cr.Stop()

	// nightly only: unlike the recoverer, do NOT sweep on startup — a redeploy
	// must not approve withdrawals off-schedule. Just log readiness.
	c.logger.Info().Ctx(ctx).Str("cron", c.config.Cron).Msg("Withdrawer scheduled, waiting for cron (UTC); no sweep on startup")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
			c.logger.Info().Ctx(ctx).Msg("Withdrawer sweep starting")
			if sweepErr := c.Sweep(ctx); sweepErr != nil {
				c.logger.Error().Ctx(ctx).Err(sweepErr).Msg("Withdrawer sweep failed")
			}
			c.logger.Info().Ctx(ctx).Msg("Withdrawer sweep complete")
		}
	}
}

// Sweep lists every Pending withdrawal from zinc and approves each in turn.
// Per-item failures are logged and skipped — the sweep never aborts midway —
// and a summary tally is logged at the end. Approve is idempotent server-side
// (zinc guards Pending -> Processing), so a crash mid-sweep is safe: the next
// sweep re-lists whatever is still Pending.
func (c *Client) Sweep(ctx context.Context) error {
	withdrawals, err := c.listPending(ctx)
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to list pending withdrawals")
		return err
	}
	if len(withdrawals) == 0 {
		c.logger.Info().Ctx(ctx).Msg("No pending withdrawals to approve")
		return nil
	}

	c.logger.Info().Ctx(ctx).Int("count", len(withdrawals)).Msg("Approving pending withdrawals")
	succeeded, failed := 0, 0
	for _, w := range withdrawals {
		id := w.Id.String()
		if approveErr := c.approve(ctx, id); approveErr != nil {
			failed++
			c.logger.Error().Ctx(ctx).Err(approveErr).Str("withdrawalId", id).Msg("Failed to approve withdrawal")
			continue
		}
		succeeded++
		c.logger.Info().Ctx(ctx).Str("withdrawalId", id).Msg("Approved withdrawal")
	}
	c.logger.Info().Ctx(ctx).
		Int("total", len(withdrawals)).
		Int("succeeded", succeeded).
		Int("failed", failed).
		Msg("Withdrawer sweep summary")
	return nil
}

// listPending fetches every withdrawal currently in Pending status,
// paginating with the configured page size until a short page.
func (c *Client) listPending(ctx context.Context) ([]zinc.WithdrawalPrincipalRes, error) {
	pending := "Pending"
	limit := int32(c.config.Limit)
	if limit <= 0 {
		limit = 100
	}
	var all []zinc.WithdrawalPrincipalRes
	for skip := int32(0); ; skip += limit {
		s := skip
		resp, err := c.zinc.GetApiVVersionWithdrawal(ctx, "1.0", &zinc.GetApiVVersionWithdrawalParams{
			Status: &pending,
			Limit:  &limit,
			Skip:   &s,
		})
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to list pending withdrawals: status %d: %s", resp.StatusCode, string(content))
		}
		if readErr != nil {
			return nil, readErr
		}
		var page []zinc.WithdrawalPrincipalRes
		if err := json.Unmarshal(content, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if int32(len(page)) < limit {
			break
		}
	}
	return all, nil
}

// approve calls zinc's POST /api/v1/Withdrawal/{id}/approve.
//
// TODO: hand-rolled because lib/zinc/main.go (generated by oapi-codegen from a
// running zinc instance, see scripts/local/gen-sdk.sh) predates the approve
// endpoint. Replace this with the generated SDK method at the next regen. It
// reuses the zinc client's exported Server, Doer and RequestEditors so auth
// and otel instrumentation match the generated calls exactly.
func (c *Client) approve(ctx context.Context, id string) error {
	url := fmt.Sprintf("%s/api/v1/Withdrawal/%s/approve", strings.TrimSuffix(c.zinc.Server, "/"), id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for _, editor := range c.zinc.RequestEditors {
		if err := editor(ctx, req); err != nil {
			return err
		}
	}
	resp, err := c.zinc.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to approve withdrawal %s: status %d: %s", id, resp.StatusCode, string(body))
	}
	return nil
}
