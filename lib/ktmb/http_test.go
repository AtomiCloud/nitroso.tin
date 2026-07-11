package ktmb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestHTTPClientHasTotalTimeout(t *testing.T) {
	client := newHTTPClient(nil, nil, 0)
	if client.Timeout != 60*time.Second {
		t.Fatalf("HTTP timeout = %s, want 60s", client.Timeout)
	}
}

func TestSendWithContextReturnsTypedHTTPStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"messages":["rate limit"]}`))
	}))
	defer server.Close()
	logger := zerolog.Nop()
	client := NewHttp[map[string]string, map[string]string](HttpConfig{Url: server.URL, Header: map[string]string{}, logger: &logger, client: server.Client()})
	_, err := client.SendWithContext(context.Background(), http.MethodPost, "reserve", map[string]string{"x": "y"})
	var statusErr *HttpStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusTooManyRequests || statusErr.Body == "" {
		t.Fatalf("err = %#v, want typed 429 with body", err)
	}
}

func TestSendWithContextHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	logger := zerolog.Nop()
	client := NewHttp[map[string]string, map[string]string](HttpConfig{Url: server.URL, Header: map[string]string{}, logger: &logger, client: server.Client()})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := client.SendWithContext(ctx, http.MethodPost, "reserve", map[string]string{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context deadline", err)
	}
}
