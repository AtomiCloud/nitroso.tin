package recoverer

import (
	"context"
	"net/http"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/walletlow"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func held(context.Context) bool { return true }

// While the KTMB wallet is low, an exhausted recovery budget must HOLD the
// booking for a human, never refund it as Duplicate: the passport conflicts
// that spent the budget are our own unpaid reservations.
func TestRecycleExhaustedWhileWalletLowParksInsteadOfDuplicate(t *testing.T) {
	stub := &zincStub{t: t, recycleStatus: http.StatusConflict,
		recycleBody: `{"type":"recovery_retries_exhausted"}`}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.walletLow = held

	l := zerolog.Nop()
	if err := c.recycleOrDuplicate(context.Background(), l, testBookingId, true); err != nil {
		t.Fatal(err)
	}
	if len(stub.duplicated) != 0 {
		t.Errorf("must not refund as Duplicate while the wallet is low, got %v", stub.duplicated)
	}
	if len(stub.parked) != 1 || stub.parked[0] != testBookingId {
		t.Errorf("expected the booking parked for manual intervention, got %v", stub.parked)
	}
}

// The hold does not touch a successful recycle.
func TestRecycleWhileWalletLowStillRecycles(t *testing.T) {
	stub := &zincStub{t: t}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.walletLow = held

	l := zerolog.Nop()
	if err := c.recycleOrDuplicate(context.Background(), l, testBookingId, true); err != nil {
		t.Fatal(err)
	}
	if len(stub.recycled) != 1 || len(stub.duplicated) != 0 || len(stub.parked) != 0 {
		t.Errorf("recycled=%v duplicated=%v parked=%v", stub.recycled, stub.duplicated, stub.parked)
	}
}

// While held, recycled bookings are not expired (expiry ends them as Duplicate).
func TestExpireRecoverySkippedWhileWalletLow(t *testing.T) {
	stub := &zincStub{t: t, expireBody: []zinc.BookingPrincipalRes{{Id: uuid.New()}}}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.walletLow = held

	if err := c.ExpireRecovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stub.expired != 0 {
		t.Errorf("recovery-expire must not be called while the wallet is low, got %d calls", stub.expired)
	}
}

// End to end through redis: the buyer's flag is what the recoverer reads, and
// once it lapses the normal Duplicate fallback resumes.
func TestHoldFollowsTheSharedRedisFlag(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	stub := &zincStub{t: t, recycleStatus: http.StatusConflict}
	c, closeSrv := conflictClient(t, stub)
	defer closeSrv()
	c.mainRedis = &otelredis.OtelRedis{Client: rc}
	ctx := context.Background()

	if c.holdForWallet(ctx) {
		t.Fatal("no flag → no hold")
	}
	mr.Set(walletlow.Key, "1")
	if !c.holdForWallet(ctx) {
		t.Fatal("flag up → hold")
	}
	mr.Del(walletlow.Key)
	l := zerolog.Nop()
	if err := c.recycleOrDuplicate(ctx, l, testBookingId, true); err != nil {
		t.Fatal(err)
	}
	if len(stub.duplicated) != 1 {
		t.Errorf("flag lapsed → normal Duplicate fallback, got duplicated=%v parked=%v", stub.duplicated, stub.parked)
	}
}

// An unreadable flag is treated as held: the cautious side is a park, never a refund.
func TestHoldOnRedisErrorIsCautious(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mr.Close()
	l := zerolog.Nop()
	c := &Client{logger: &l, mainRedis: &otelredis.OtelRedis{Client: rc}}
	if !c.holdForWallet(context.Background()) {
		t.Error("a redis error must read as held")
	}
}
