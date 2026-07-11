package withdrawer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/AtomiCloud/nitroso-tin/system/telemetry"
	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	"github.com/robfig/cron"
	"github.com/rs/zerolog"
)

// Client runs two cron sweeps against zinc:
//
// The NIGHTLY APPROVE SWEEP (config.Cron) lists Pending withdrawals and
// approves each one (Pending -> Processing) via zinc's approve endpoint, one
// by one. It also RE-DRIVES stuck Processing withdrawals: zinc can leave a
// withdrawal in Processing with no payout confirmation when an approve's
// gateway call failed ambiguously or the pod died mid-flight. Re-approving
// such a withdrawal is safe and expected — zinc re-drives the payout
// idempotently with the same gateway request id. Processing withdrawals that
// already have a payout confirmation are rejected by zinc's state guard with
// an InvalidWithdrawalOperation error, which the sweep treats as a benign
// skip (see Sweep).
//
// The 6-HOURLY RECONCILE SWEEP (config.ReconcileCron) lists Processing
// withdrawals and POSTs each to zinc's reconcile endpoint, which looks the
// transfer up at Airwallex and settles/fails/parks the withdrawal
// server-side. A withdrawal no longer in Processing by the time the reconcile
// lands is rejected by zinc with the same InvalidWithdrawalOperation error —
// benign, someone else already resolved it (see Reconcile).
//
// SINGLE REPLICA ONLY. The sweeps assume they are the only approver: two
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

	approveCh := make(chan struct{}, 1)
	reconcileCh := make(chan struct{}, 1)

	// robfig/cron v1 evaluates specs in the runner's location; force UTC so
	// both sweeps fire on UTC wall time regardless of the pod's timezone.
	cr := cron.NewWithLocation(time.UTC)
	if err = cr.AddFunc(c.config.Cron, func() {
		select {
		case approveCh <- struct{}{}:
		default:
		}
	}); err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("cron", c.config.Cron).Msg("Failed to schedule withdrawer approve cron")
		return err
	}
	if err = cr.AddFunc(c.config.ReconcileCron, func() {
		select {
		case reconcileCh <- struct{}{}:
		default:
		}
	}); err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Str("cron", c.config.ReconcileCron).Msg("Failed to schedule withdrawer reconcile cron")
		return err
	}
	cr.Start()
	defer cr.Stop()

	// cron only: unlike the recoverer, do NOT sweep on startup — a redeploy
	// must not approve or reconcile withdrawals off-schedule. Just log which
	// cron drives which sweep.
	c.logger.Info().Ctx(ctx).
		Str("approveCron", c.config.Cron).
		Str("reconcileCron", c.config.ReconcileCron).
		Msg("Withdrawer scheduled (UTC): approve sweep of Pending+Processing on approveCron, reconcile sweep of Processing on reconcileCron; no sweep on startup")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-approveCh:
			// approvals can be turned off (withdrawals then only pay out when a
			// human clicks approve in zinc) while the reconcile sweep keeps
			// running — in-flight Processing payouts must still settle
			if !c.config.ApproveEnable {
				c.logger.Info().Ctx(ctx).Msg("Withdrawer approve sweep disabled by config, skipping")
				continue
			}
			c.logger.Info().Ctx(ctx).Msg("Withdrawer approve sweep starting")
			if sweepErr := c.Sweep(ctx); sweepErr != nil {
				c.logger.Error().Ctx(ctx).Err(sweepErr).Msg("Withdrawer approve sweep failed")
			}
			c.logger.Info().Ctx(ctx).Msg("Withdrawer approve sweep complete")
		case <-reconcileCh:
			c.logger.Info().Ctx(ctx).Msg("Withdrawer reconcile sweep starting")
			if sweepErr := c.Reconcile(ctx); sweepErr != nil {
				c.logger.Error().Ctx(ctx).Err(sweepErr).Msg("Withdrawer reconcile sweep failed")
			}
			c.logger.Info().Ctx(ctx).Msg("Withdrawer reconcile sweep complete")
		}
	}
}

// sweepSummary tallies one sweep's per-item outcomes.
type sweepSummary struct {
	total     int
	succeeded int
	skipped   int
	failed    int
}

// Sweep lists every Pending withdrawal plus every Processing withdrawal from
// zinc and approves each in turn. Per-item failures are logged and skipped —
// the sweep never aborts midway — and a summary tally is logged at the end.
// Approve is idempotent server-side (zinc guards the state transitions), so a
// crash mid-sweep is safe: the next sweep re-lists whatever is still
// approvable.
//
// Processing withdrawals are swept for the re-drive described on Client. We
// cannot tell stuck (no payout confirmation) from confirmed ones client-side:
// the generated zinc.WithdrawalPrincipalRes predates zinc's payout field, so
// confirmationNumber is not readable through the SDK. Instead we approve
// EVERY Processing withdrawal and rely on zinc's guard: stuck ones are
// re-driven idempotently, confirmed ones are rejected with a 4xx
// InvalidWithdrawalOperation, which we treat as a benign skip (info log, not
// counted as failed). This self-corrects once the SDK is regenerated — the
// guard keeps rejecting the confirmed ones we could then filter out locally.
func (c *Client) Sweep(ctx context.Context) error {
	summary, err := c.sweep(ctx)
	if err != nil {
		return err
	}
	c.logger.Info().Ctx(ctx).
		Int("total", summary.total).
		Int("succeeded", summary.succeeded).
		Int("skipped", summary.skipped).
		Int("failed", summary.failed).
		Msg("Withdrawer sweep summary")
	return nil
}

func (c *Client) sweep(ctx context.Context) (sweepSummary, error) {
	pending, err := c.listByStatus(ctx, "Pending")
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to list pending withdrawals")
		return sweepSummary{}, err
	}
	processing, err := c.listByStatus(ctx, "Processing")
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to list processing withdrawals")
		return sweepSummary{}, err
	}

	withdrawals := dedupeById(append(pending, processing...))
	if len(withdrawals) == 0 {
		c.logger.Info().Ctx(ctx).Msg("No withdrawals to approve")
		return sweepSummary{}, nil
	}

	c.logger.Info().Ctx(ctx).Int("count", len(withdrawals)).Msg("Approving withdrawals")
	summary := sweepSummary{total: len(withdrawals)}
	for _, w := range withdrawals {
		id := w.Id.String()
		approveErr := c.approve(ctx, id)
		switch {
		case approveErr == nil:
			summary.succeeded++
			c.logger.Info().Ctx(ctx).Str("withdrawalId", id).Msg("Approved withdrawal")
		case errors.Is(approveErr, errNotApprovable):
			// benign: zinc's state guard rejected the approve because the
			// withdrawal is no longer approvable — a Processing one whose payout
			// is already confirmed, or one cancelled/rejected mid-sweep. Nothing
			// to do and nothing failed.
			summary.skipped++
			c.logger.Info().Ctx(ctx).Str("withdrawalId", id).Str("reason", approveErr.Error()).Msg("Skipped withdrawal not in an approvable state")
		default:
			summary.failed++
			c.logger.Error().Ctx(ctx).Err(approveErr).Str("withdrawalId", id).Msg("Failed to approve withdrawal")
		}
	}
	return summary, nil
}

// Reconcile lists every Processing withdrawal from zinc and POSTs each to
// zinc's reconcile endpoint, which looks the transfer up at Airwallex and
// settles/fails/parks the withdrawal server-side. Per-item failures are
// logged and skipped — the sweep never aborts midway — and a summary tally is
// logged at the end. Reconcile is safe to repeat: zinc responds 200 on any
// conclusive-or-counted outcome and rejects withdrawals no longer in
// Processing with an InvalidWithdrawalOperation error, which the sweep treats
// as a benign skip (someone else already resolved it).
func (c *Client) Reconcile(ctx context.Context) error {
	summary, err := c.reconcileSweep(ctx)
	if err != nil {
		return err
	}
	c.logger.Info().Ctx(ctx).
		Int("total", summary.total).
		Int("succeeded", summary.succeeded).
		Int("skipped", summary.skipped).
		Int("failed", summary.failed).
		Msg("Withdrawer reconcile summary")
	return nil
}

func (c *Client) reconcileSweep(ctx context.Context) (sweepSummary, error) {
	processing, err := c.listByStatus(ctx, "Processing")
	if err != nil {
		c.logger.Error().Ctx(ctx).Err(err).Msg("Failed to list processing withdrawals")
		return sweepSummary{}, err
	}

	withdrawals := dedupeById(processing)
	if len(withdrawals) == 0 {
		c.logger.Info().Ctx(ctx).Msg("No withdrawals to reconcile")
		return sweepSummary{}, nil
	}

	c.logger.Info().Ctx(ctx).Int("count", len(withdrawals)).Msg("Reconciling withdrawals")
	summary := sweepSummary{total: len(withdrawals)}
	for _, w := range withdrawals {
		id := w.Id.String()
		reconcileErr := c.reconcile(ctx, id)
		switch {
		case reconcileErr == nil:
			summary.succeeded++
			c.logger.Info().Ctx(ctx).Str("withdrawalId", id).Msg("Reconciled withdrawal")
		case errors.Is(reconcileErr, errNotReconcilable):
			// benign: zinc's state guard rejected the reconcile because the
			// withdrawal is no longer in Processing — someone else (an admin, the
			// webhook, a concurrent sweep) already resolved it. Nothing to do and
			// nothing failed.
			summary.skipped++
			c.logger.Info().Ctx(ctx).Str("withdrawalId", id).Str("reason", reconcileErr.Error()).Msg("Skipped withdrawal not in a reconcilable state")
		default:
			summary.failed++
			c.logger.Error().Ctx(ctx).Err(reconcileErr).Str("withdrawalId", id).Msg("Failed to reconcile withdrawal")
		}
	}
	return summary, nil
}

// dedupeById drops repeated withdrawal ids, keeping first occurrences in
// order. Skip/Limit paging in listByStatus runs over a set that shrinks while
// paging (users cancel, admins reject mid-sweep), so an id can come back
// twice — or be skipped, in which case the next nightly sweep picks it up.
// TODO: keyset pagination would be better once the SDK is regenerated with a
// cursor-capable listing.
func dedupeById(withdrawals []zinc.WithdrawalPrincipalRes) []zinc.WithdrawalPrincipalRes {
	seen := make(map[openapi_types.UUID]struct{}, len(withdrawals))
	deduped := make([]zinc.WithdrawalPrincipalRes, 0, len(withdrawals))
	for _, w := range withdrawals {
		if _, ok := seen[w.Id]; ok {
			continue
		}
		seen[w.Id] = struct{}{}
		deduped = append(deduped, w)
	}
	return deduped
}

// maxListPages bounds a single sweep's paging: if withdrawals are created as
// fast as they are paged, the short-page exit may never trigger and an
// unbounded loop would block the sweep while growing memory without limit.
// Anything past the cap is simply picked up by the next nightly sweep.
const maxListPages = 50

// listByStatus fetches every withdrawal currently in the given status,
// paginating with the configured page size until a short page (or the
// maxListPages safety cap).
func (c *Client) listByStatus(ctx context.Context, status string) ([]zinc.WithdrawalPrincipalRes, error) {
	limit := int32(c.config.Limit)
	if limit <= 0 {
		limit = 100
	}
	var all []zinc.WithdrawalPrincipalRes
	for page, skip := 0, int32(0); page < maxListPages; page, skip = page+1, skip+limit {
		s := skip
		resp, err := c.zinc.GetApiVVersionWithdrawal(ctx, "1.0", &zinc.GetApiVVersionWithdrawalParams{
			Status: &status,
			Limit:  &limit,
			Skip:   &s,
		})
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to list %s withdrawals: status %d: %s", status, resp.StatusCode, string(content))
		}
		if readErr != nil {
			return nil, readErr
		}
		var items []zinc.WithdrawalPrincipalRes
		if err := json.Unmarshal(content, &items); err != nil {
			return nil, err
		}
		all = append(all, items...)
		if int32(len(items)) < limit {
			return all, nil
		}
	}
	c.logger.Warn().Str("status", status).Int("pages", maxListPages).
		Msg("hit paging cap while listing withdrawals; remainder deferred to the next sweep")
	return all, nil
}

// errNotApprovable marks zinc's InvalidWithdrawalOperation rejection: the
// withdrawal's current state does not allow an approve (e.g. Processing with
// a payout confirmation already recorded, or Cancelled/Rejected mid-sweep).
// Sweep treats it as a benign skip, never a failure.
var errNotApprovable = errors.New("withdrawal is not in an approvable state")

// errNotReconcilable is the reconcile counterpart of errNotApprovable: zinc
// rejects a reconcile with InvalidWithdrawalOperation when the withdrawal is
// no longer in Processing (someone else already resolved it). Reconcile
// treats it as a benign skip, never a failure.
var errNotReconcilable = errors.New("withdrawal is not in a reconcilable state")

// isInvalidWithdrawalOperation reports whether a failed approve or reconcile
// response is zinc's InvalidWithdrawalOperation domain problem. zinc serialises domain
// problems as RFC 7807 problem details whose type URL ends in the problem id
// ("invalid_withdrawal_operation") when the error portal is enabled; match
// the title too so detection survives the portal being disabled.
func isInvalidWithdrawalOperation(status int, body []byte) bool {
	if status < 400 || status >= 500 {
		return false
	}
	var problem struct {
		Type  string `json:"type"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		return false
	}
	return strings.HasSuffix(problem.Type, "/invalid_withdrawal_operation") ||
		strings.EqualFold(problem.Title, "Invalid Withdrawal Operation")
}

// approve calls zinc's POST /api/v1.0/Withdrawal/{id}/approve (same version segment as the generated SDK calls).
func (c *Client) approve(ctx context.Context, id string) error {
	return c.postWithdrawalAction(ctx, id, "approve", errNotApprovable)
}

// reconcile calls zinc's POST /api/v1.0/Withdrawal/{id}/reconcile.
func (c *Client) reconcile(ctx context.Context, id string) error {
	return c.postWithdrawalAction(ctx, id, "reconcile", errNotReconcilable)
}

// postWithdrawalAction calls zinc's POST /api/v1.0/Withdrawal/{id}/{action}
// with an empty JSON body, mapping zinc's InvalidWithdrawalOperation problem
// onto the given benign sentinel so callers can tally it as a skip.
//
// TODO: hand-rolled because lib/zinc/main.go (generated by oapi-codegen from a
// running zinc instance, see scripts/local/gen-sdk.sh) predates the approve
// and reconcile endpoints. Replace this with the generated SDK methods at the
// next regen. It reuses the zinc client's exported Server, Doer and
// RequestEditors so auth and otel instrumentation match the generated calls
// exactly.
func (c *Client) postWithdrawalAction(ctx context.Context, id, action string, benign error) error {
	url := fmt.Sprintf("%s/api/v1.0/Withdrawal/%s/%s", strings.TrimSuffix(c.zinc.Server, "/"), id, action)
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
		if isInvalidWithdrawalOperation(resp.StatusCode, body) {
			return fmt.Errorf("withdrawal %s: %w: status %d: %s", id, benign, resp.StatusCode, string(body))
		}
		return fmt.Errorf("failed to %s withdrawal %s: status %d: %s", action, id, resp.StatusCode, string(body))
	}
	return nil
}
