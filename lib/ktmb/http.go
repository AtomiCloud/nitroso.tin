package ktmb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"io"
	"math/rand"
	"net/http"
	u "net/url"
	"strings"
	"time"
)

type HttpConfig struct {
	Url    string
	Header map[string]string
	logger *zerolog.Logger
	client *http.Client
}

// HttpStatusError is a non-2xx KTMB response, carrying the status code and
// raw body so callers can classify semantic rejections (4xx with a known
// message) separately from transport/infrastructure noise (5xx, gateway
// pages). It formats identically to the previous flat fmt.Errorf string so
// existing log/message matching is unaffected.
type HttpStatusError struct {
	StatusCode int
	Body       string
}

func (e *HttpStatusError) Error() string {
	return fmt.Sprintf("status code %d, body %s", e.StatusCode, e.Body)
}

type HttpClient[T any, Y any] struct {
	Url    string
	Header map[string]string
	logger *zerolog.Logger
	client *http.Client
}

func NewHttp[T any, Y any](c HttpConfig) HttpClient[T, Y] {
	k := HttpClient[T, Y]{
		Url:    c.Url,
		Header: c.Header,
		logger: c.logger,
		client: c.client,
	}
	return k
}

// applyRealIP generates a random public IPv4 address and sets it as the
// X-Real-IP header on every request.
func (k HttpClient[T, Y]) applyRealIP(req *http.Request) {
	req.Header.Set("X-Real-IP", randomPublicIP())
}

// randomPublicIP returns a random, routable IPv4 address. It avoids private,
// loopback, link-local, multicast and other reserved ranges so the generated
// address looks like a plausible public client IP.
func randomPublicIP() string {
	for {
		a := rand.Intn(256)
		b := rand.Intn(256)
		c := rand.Intn(256)
		d := rand.Intn(256)

		switch {
		case a == 0: // "this" network
		case a == 10: // 10.0.0.0/8 private
		case a == 127: // loopback
		case a == 100 && b >= 64 && b <= 127: // 100.64.0.0/10 CGNAT
		case a == 169 && b == 254: // link-local
		case a == 172 && b >= 16 && b <= 31: // 172.16.0.0/12 private
		case a == 192 && b == 168: // 192.168.0.0/16 private
		case a == 192 && b == 0 && c == 2: // TEST-NET-1
		case a == 198 && (b == 18 || b == 19): // benchmarking
		case a == 198 && b == 51 && c == 100: // TEST-NET-2
		case a == 203 && b == 0 && c == 113: // TEST-NET-3
		case a >= 224: // multicast + reserved
		default:
			return fmt.Sprintf("%d.%d.%d.%d", a, b, c, d)
		}
	}
}

// newHTTPClient builds a single reusable HTTP client with connection pooling
// (keep-alive) tuned for the reserver's concurrent load. It is created once per
// Ktmb and shared across every request and goroutine (http.Client/Transport are
// safe for concurrent use), so TCP+TLS handshakes are amortized instead of paid
// on every call. When a proxy list is configured, a random proxy is picked per
// request via the transport's Proxy hook, so connections stay pooled per
// (proxy, host) while still spreading load across proxies.
func newHTTPClient(proxy *string, dc *dnsCache, maxIdlePerHost int) *http.Client {
	if maxIdlePerHost < 100 {
		maxIdlePerHost = 100
	}
	transport := &http.Transport{
		MaxIdleConns:          maxIdlePerHost * 2,
		MaxIdleConnsPerHost:   maxIdlePerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	if dc != nil {
		transport.DialContext = dc.dialContext // dial pre-resolved, cached IPs
	}
	if proxy != nil {
		proxies := strings.Split(*proxy, ";")
		transport.Proxy = func(*http.Request) (*u.URL, error) {
			return u.Parse(strings.TrimSpace(proxies[rand.Intn(len(proxies))]))
		}
	}
	// Bound the complete exchange, including body reads. Individual callers do
	// not currently attach contexts to requests, so without a client timeout a
	// stalled peer could keep a short-lived prober Job alive indefinitely.
	return &http.Client{Transport: transport, Timeout: 60 * time.Second}
}

func (k HttpClient[T, Y]) Send(method string, path string, headers ...map[string]string) (Y, error) {
	return k.SendContext(context.Background(), method, path, headers...)
}

func (k HttpClient[T, Y]) SendContext(ctx context.Context, method string, path string, headers ...map[string]string) (Y, error) {
	url := fmt.Sprintf("%s/%s", k.Url, path)

	var y Y

	body := strings.NewReader("")

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to create request")
		return y, err
	}

	for _, h := range headers {
		for hk, hv := range h {
			req.Header.Add(hk, hv)
		}
	}

	for hk, hv := range k.Header {
		req.Header.Add(hk, hv)
	}

	k.applyRealIP(req)

	res, err := k.client.Do(req)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to send request")
		return y, err
	}
	defer res.Body.Close()

	if res.StatusCode > 399 {
		k.logger.Error().Err(err).Msg("Failed to send request")
		resp, e := io.ReadAll(res.Body)
		if e != nil {
			k.logger.Error().Err(err).Msg("Failed to read response")
			return y, &HttpStatusError{StatusCode: res.StatusCode}
		} else {
			return y, &HttpStatusError{StatusCode: res.StatusCode, Body: string(resp)}
		}

	}

	resp, err := io.ReadAll(res.Body)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to read response")
		return y, err
	}

	err = json.Unmarshal(resp, &y)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to decode response")
		return y, err
	}
	return y, nil
}

func (k HttpClient[T, Y]) SendWith(method string, path string, payload T, headers ...map[string]string) (Y, error) {
	return k.SendWithContext(context.Background(), method, path, payload, headers...)
}

func (k HttpClient[T, Y]) SendWithContext(ctx context.Context, method string, path string, payload T, headers ...map[string]string) (Y, error) {
	url := fmt.Sprintf("%s/%s", k.Url, path)

	var y Y

	marshal, err := json.Marshal(payload)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to marshal payload")
		return y, err
	}

	body := bytes.NewReader(marshal)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to create request")
		return y, err
	}

	for _, h := range headers {
		for hk, hv := range h {
			req.Header.Add(hk, hv)
		}
	}

	for hk, hv := range k.Header {
		req.Header.Add(hk, hv)
	}

	k.applyRealIP(req)

	res, err := k.client.Do(req)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to send request")
		return y, err
	}
	defer res.Body.Close()

	resp, err := io.ReadAll(res.Body)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to read response body")
		return y, err
	}

	if res.StatusCode > 399 {
		k.logger.Error().Str("body", string(resp)).Int("status", res.StatusCode).Msg("HTTP request failed")
		return y, &HttpStatusError{StatusCode: res.StatusCode, Body: string(resp)}
	}

	err = json.Unmarshal(resp, &y)
	if err != nil {
		k.logger.Error().Err(err).Str("body", string(resp)).Msg("Failed to decode response")
		return y, err
	}
	return y, nil
}

func (k HttpClient[T, Y]) BinarySendWith(method string, path string, payload T, headers ...map[string]string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s", k.Url, path)

	marshal, err := json.Marshal(payload)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to marshal payload")
		return nil, err
	}

	body := bytes.NewReader(marshal)
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to create request")
		return nil, err
	}

	for _, h := range headers {
		for hk, hv := range h {
			req.Header.Add(hk, hv)
		}
	}

	for hk, hv := range k.Header {
		req.Header.Add(hk, hv)
	}

	k.applyRealIP(req)

	res, err := k.client.Do(req)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to send request")
		return nil, err
	}
	defer res.Body.Close()

	bin, err := io.ReadAll(res.Body)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to read response body")
		return nil, err
	}

	if res.StatusCode > 399 {
		k.logger.Error().Str("body", string(bin)).Int("status", res.StatusCode).Msg("HTTP request failed")
		return nil, &HttpStatusError{StatusCode: res.StatusCode, Body: string(bin)}
	}

	return bin, nil
}
