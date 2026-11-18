// Package walletlow is the shared "KTMB wallet is low" hold flag.
//
// The buyer raises it whenever KTMB refuses a payment because the KTM e-wallet
// cannot cover the fare (Buyer.RevertPatterns), refreshing its TTL on every
// refusal. While it is up, bookings are HELD rather than refunded: the
// recoverer never ends a booking as Duplicate (a budget-exhausted booking is
// parked as RequireManualIntervention instead) and it skips the recycled-
// booking expiry. The flag lapses on its own once the wallet is topped up and
// no refusal has been seen for the TTL.
package walletlow

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Key is the main-redis key that carries the flag (value: unix seconds of the
// latest refusal). Shared by the buyer (writer) and the recoverer (reader).
const Key = "nitroso.tin:ktmb-wallet-low"

// Mark raises (or refreshes) the flag for ttl.
func Mark(ctx context.Context, r redis.Cmdable, ttl time.Duration, now time.Time) error {
	if ttl <= 0 {
		return errors.New("walletlow: ttl must be positive")
	}
	return r.Set(ctx, Key, strconv.FormatInt(now.Unix(), 10), ttl).Err()
}

// IsLow reports whether the flag is up.
func IsLow(ctx context.Context, r redis.Cmdable) (bool, error) {
	n, err := r.Exists(ctx, Key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
