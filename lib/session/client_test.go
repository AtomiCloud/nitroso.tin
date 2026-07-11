package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func TestConcurrentCacheMissPerformsOneKTMBLogin(t *testing.T) {
	var loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		loginCalls.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":true,"messages":[],"data":{"userData":"shared-token"}}`))
	}))
	defer server.Close()

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	mainRedis := otelredis.OtelRedis{Client: rdb}
	logger := zerolog.Nop()
	k := ktmb.New(server.URL, server.URL, "signature", &logger, nil, ktmb.WarmConfig{})
	encr := encryptor.NewSymEncryptor[ktmb.LoginRes]("0123456789abcdef0123456789abcdef", &logger)
	first := New(&k, &mainRedis, &logger, "login-session", encr)
	second := New(&k, &mainRedis, &logger, "login-session", encr)

	results := make(chan string, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, client := range []*Session{&first, &second} {
		wg.Add(1)
		go func(client *Session) {
			defer wg.Done()
			token, err := client.Login(context.Background(), "funded@example.com", "password")
			results <- token
			errs <- err
		}(client)
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for token := range results {
		if token != "shared-token" {
			t.Fatalf("token = %q", token)
		}
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("KTMB login calls = %d, want 1", loginCalls.Load())
	}
	if mini.Exists("login-session:login-lock") {
		t.Fatal("login lock was not released")
	}
}
