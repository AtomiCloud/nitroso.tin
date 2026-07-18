package otelredis

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
)

func TestFQDNHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{
			name: "external hostname",
			host: "relaxed-foxhound-32927.upstash.io",
			want: "relaxed-foxhound-32927.upstash.io.",
		},
		{
			name: "cluster short name",
			host: "zinc-maincache",
			want: "zinc-maincache",
		},
		{
			name: "cluster FQDN",
			host: "foo.bar.svc.cluster.local",
			want: "foo.bar.svc.cluster.local.",
		},
		{
			name: "already absolute",
			host: "x.y.",
			want: "x.y.",
		},
		{
			name: "IPv4 address",
			host: "10.0.0.1",
			want: "10.0.0.1",
		},
		{
			name: "IPv6 address",
			host: "2001:db8::1",
			want: "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fqdnHost(tt.host); got != tt.want {
				t.Fatalf("fqdnHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestNewUsesAbsoluteDialHostAndBareTLSServerName(t *testing.T) {
	const host = "relaxed-foxhound-32927.upstash.io"

	rdb := New(config.CacheConfig{
		Password: "fake-password",
		Ssl:      true,
		Endpoints: map[int]string{
			0: net.JoinHostPort(host, "6379"),
		},
	})
	t.Cleanup(func() { _ = rdb.Close() })

	opt := rdb.Options()
	dialHost, _, err := net.SplitHostPort(opt.Addr)
	if err != nil {
		t.Fatalf("split Redis address %q: %v", opt.Addr, err)
	}
	if dialHost != host+"." {
		t.Fatalf("Redis dial host = %q, want %q", dialHost, host+".")
	}
	if opt.TLSConfig == nil {
		t.Fatal("expected TLS config for SSL Redis client")
	}
	if opt.TLSConfig.ServerName != host {
		t.Fatalf("TLS ServerName = %q, want bare host %q", opt.TLSConfig.ServerName, host)
	}
	if strings.HasSuffix(opt.TLSConfig.ServerName, ".") {
		t.Fatalf("TLS ServerName must not end in a dot: %q", opt.TLSConfig.ServerName)
	}
}

// QueuePush must surface a failed LPUSH as an error rather than reporting
// success. go-redis stores execution errors on the returned *IntCmd, so a bare
// (cmd, nil) return would make a connection/context failure look like a
// successful enqueue — and the recover queue is the sole durable store of a
// captured ticket's identifiers, so a silent drop can drive a wrongful refund.
func TestQueuePushPropagatesCommandError(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer func() { _ = rdb.Close() }()
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
