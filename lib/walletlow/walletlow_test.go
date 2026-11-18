package walletlow

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestMarkRaisesFlagUntilTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx := context.Background()

	if low, err := IsLow(ctx, r); err != nil || low {
		t.Fatalf("fresh redis: IsLow = %v, %v; want false, nil", low, err)
	}
	if err := Mark(ctx, r, time.Hour, time.Unix(1700000000, 0)); err != nil {
		t.Fatal(err)
	}
	if low, err := IsLow(ctx, r); err != nil || !low {
		t.Fatalf("after Mark: IsLow = %v, %v; want true, nil", low, err)
	}
	if got, _ := mr.Get(Key); got != "1700000000" {
		t.Errorf("flag value = %q, want the refusal's unix seconds", got)
	}
	mr.FastForward(time.Hour + time.Second)
	if low, _ := IsLow(ctx, r); low {
		t.Error("flag must lapse once its TTL passes without a new refusal")
	}
}

func TestMarkRefreshesTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx := context.Background()

	_ = Mark(ctx, r, time.Hour, time.Now())
	mr.FastForward(50 * time.Minute)
	_ = Mark(ctx, r, time.Hour, time.Now())
	mr.FastForward(50 * time.Minute)
	if low, _ := IsLow(ctx, r); !low {
		t.Error("a new refusal must restart the hold window")
	}
}

func TestMarkRejectsNonPositiveTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	if err := Mark(context.Background(), r, 0, time.Now()); err == nil {
		t.Error("a zero TTL would raise a flag that never lapses; must error")
	}
}
