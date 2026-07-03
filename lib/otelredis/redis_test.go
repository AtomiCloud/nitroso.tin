package otelredis

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
)

// QueuePush must surface a failed LPUSH as an error rather than reporting
// success. go-redis stores execution errors on the returned *IntCmd, so a bare
// (cmd, nil) return would make a connection/context failure look like a
// successful enqueue — and the recover queue is the sole durable store of a
// captured ticket's identifiers, so a silent drop can drive a wrongful refund.
func TestQueuePushPropagatesCommandError(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer rdb.Close()
	r := OtelRedis{rdb}

	// a cancelled context makes the LPUSH fail deterministically without needing
	// a live Redis: go-redis records the context error on the command.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd, err := r.QueuePush(ctx, otel.Tracer("test"), "recover", "payload")
	if err == nil {
		t.Fatal("expected QueuePush to return the command error, got nil (a redis failure would look like a successful enqueue)")
	}
	if cmd == nil {
		t.Fatal("expected QueuePush to still return the *IntCmd for logging, got nil")
	}
	if cmd.Err() == nil {
		t.Fatal("expected the returned command to carry its error")
	}
}
