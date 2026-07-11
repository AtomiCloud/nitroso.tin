package enricher

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSleepContextStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := sleepContext(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context canceled", err)
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatal("context-aware enrichment sleep did not stop promptly")
	}
}
