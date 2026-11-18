package buyer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/walletlow"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/alicebob/miniredis/v2"
	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const revertBookingId = "aaaaaaaa-0000-4000-8000-0000000000aa"

// revertHarness wires a buyer Client to a fake zinc revert endpoint, a
// recording KTMB release and an in-memory redis, logging every side effect in
// order so tests can assert the release happens before zinc is asked.
type revertHarness struct {
	mu           sync.Mutex
	events       []string
	revertStatus []int // answered in order; the last one repeats
	releaseErr   error
	mr           *miniredis.Miniredis
}

func (h *revertHarness) record(e string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, e)
}

func (h *revertHarness) client(t *testing.T) *Client {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/revert/") {
			t.Errorf("unexpected zinc call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h.record("revert force=" + r.URL.Query().Get("force"))
		status := h.revertStatus[min(calls, len(h.revertStatus)-1)]
		calls++
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	zc, err := zinc.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	h.mr = miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: h.mr.Addr()})
	l := zerolog.Nop()
	return &Client{
		zinc:      zc,
		logger:    &l,
		mainRedis: &otelredis.OtelRedis{Client: rc},
		buyerCfg:  config.BuyerConfig{ParkRetries: 2, WalletLowHoldMinutes: 30},
		releaseFn: func(_, _ string) error {
			h.record("release")
			return h.releaseErr
		},
	}
}

func revertId(t *testing.T) openapi_types.UUID {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(revertBookingId)); err != nil {
		t.Fatal(err)
	}
	return id
}

func withFastBackoff(t *testing.T) {
	old := baseDelay
	baseDelay = time.Millisecond
	t.Cleanup(func() { baseDelay = old })
}

// Today's incident: the non-forced revert was refused (booking < 5 min in
// Buying), revert() returned before releasing, the passport stayed bound to an
// unpaid KTMB hold, and the buyer crash-looped. Now: release first, force the
// revert, raise the wallet-low hold, never error.
func TestRevertReleasesFirstForcesAndRaisesHold(t *testing.T) {
	withFastBackoff(t)
	h := &revertHarness{revertStatus: []int{http.StatusOK}}
	c := h.client(t)

	if err := c.revert(context.Background(), revertId(t), "ud", "bd"); err != nil {
		t.Fatalf("revert must never error, got %v", err)
	}
	want := []string{"release", "revert force=true"}
	if strings.Join(h.events, ",") != strings.Join(want, ",") {
		t.Errorf("events = %v, want %v (release BEFORE asking zinc)", h.events, want)
	}
	if !h.mr.Exists(walletlow.Key) {
		t.Fatal("the wallet-low hold flag must be raised")
	}
	if ttl := h.mr.TTL(walletlow.Key); ttl != 30*time.Minute {
		t.Errorf("hold TTL = %v, want the configured 30m", ttl)
	}
}

// zinc refusing (or erroring) must not crash the buyer: the reservation is
// already released and the reverter cron recycles the stuck booking.
func TestRevertZincRefusalIsNotAnError(t *testing.T) {
	withFastBackoff(t)
	h := &revertHarness{revertStatus: []int{http.StatusBadRequest}}
	c := h.client(t)

	if err := c.revert(context.Background(), revertId(t), "ud", "bd"); err != nil {
		t.Fatalf("a refused revert must be logged, not returned (crash loop), got %v", err)
	}
	if h.events[0] != "release" {
		t.Errorf("the KTMB hold must be released even when zinc refuses, events = %v", h.events)
	}
	reverts := 0
	for _, e := range h.events {
		if strings.HasPrefix(e, "revert") {
			reverts++
		}
	}
	if reverts != 2 {
		t.Errorf("expected ParkRetries (2) revert attempts, got %d", reverts)
	}
}

// A transient zinc blip is retried.
func TestRevertRetriesTransientZincFailure(t *testing.T) {
	withFastBackoff(t)
	h := &revertHarness{revertStatus: []int{http.StatusBadGateway, http.StatusOK}}
	c := h.client(t)

	if err := c.revert(context.Background(), revertId(t), "ud", "bd"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.events, ","); got != "release,revert force=true,revert force=true" {
		t.Errorf("events = %s", got)
	}
}

// A failed KTMB release (it expires on its own) must not stop the revert.
func TestRevertContinuesWhenReleaseFails(t *testing.T) {
	withFastBackoff(t)
	h := &revertHarness{revertStatus: []int{http.StatusOK}, releaseErr: errors.New("ktmb down")}
	c := h.client(t)

	if err := c.revert(context.Background(), revertId(t), "ud", "bd"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.events, ","); got != "release,revert force=true" {
		t.Errorf("events = %s", got)
	}
	if !h.mr.Exists(walletlow.Key) {
		t.Error("the hold flag must still be raised")
	}
}

// An unset WalletLowHoldMinutes must not raise a flag that never lapses.
func TestRevertDefaultsHoldWhenUnset(t *testing.T) {
	withFastBackoff(t)
	h := &revertHarness{revertStatus: []int{http.StatusOK}}
	c := h.client(t)
	c.buyerCfg.WalletLowHoldMinutes = 0

	_ = c.revert(context.Background(), revertId(t), "ud", "bd")
	if ttl := h.mr.TTL(walletlow.Key); ttl != defaultWalletLowHold {
		t.Errorf("hold TTL = %v, want default %v", ttl, defaultWalletLowHold)
	}
}
