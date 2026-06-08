package ktmb

import (
	"context"
	"io"
	"net"
	"net/http"
	u "net/url"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// WarmConfig controls the warm-connection pool and DNS cache. PoolSize <= 0
// disables both (the client behaves as a plain pooled http.Client).
type WarmConfig struct {
	PoolSize     int
	IntervalMs   int
	DnsRefreshMs int
}

func (w WarmConfig) interval() time.Duration   { return msOr(w.IntervalMs, 30000) }
func (w WarmConfig) dnsRefresh() time.Duration { return msOr(w.DnsRefreshMs, 60000) }

func msOr(ms, def int) time.Duration {
	if ms <= 0 {
		ms = def
	}
	return time.Duration(ms) * time.Millisecond
}

// hostOf returns the bare hostname of a URL (no scheme/port).
func hostOf(rawURL string) string {
	parsed, err := u.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return parsed.Hostname()
}

// rootURL returns scheme://host/ for warming (a cheap request target).
func rootURL(rawURL string) string {
	parsed, err := u.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return parsed.Scheme + "://" + parsed.Host + "/"
}

// fqdn appends a trailing dot so the resolver treats the name as absolute and
// skips the search-domain list (the Kubernetes ndots:5 amplification).
func fqdn(host string) string {
	if strings.HasSuffix(host, ".") {
		return host
	}
	return host + "."
}

// dnsCache pre-resolves KTMB hosts in the background and dials the cached IP, so
// DNS never sits on the hot path. It degrades gracefully: any miss/error falls
// back to a normal dial (which resolves via the OS).
type dnsCache struct {
	resolver *net.Resolver
	dialer   *net.Dialer
	logger   *zerolog.Logger

	mu      sync.RWMutex
	entries map[string][]string // host -> IPs
	hosts   map[string]struct{} // hosts to keep refreshed
}

func newDNSCache(logger *zerolog.Logger) *dnsCache {
	return &dnsCache{
		resolver: net.DefaultResolver,
		dialer:   &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second},
		logger:   logger,
		entries:  map[string][]string{},
		hosts:    map[string]struct{}{},
	}
}

func (d *dnsCache) resolve(ctx context.Context, host string) ([]string, error) {
	return d.resolver.LookupHost(ctx, fqdn(host))
}

func (d *dnsCache) get(host string) ([]string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ips, ok := d.entries[host]
	return ips, ok && len(ips) > 0
}

func (d *dnsCache) set(host string, ips []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[host] = ips
	d.hosts[host] = struct{}{}
}

// prime resolves the given hosts up front so the cache is hot from boot.
func (d *dnsCache) prime(ctx context.Context, hosts []string) {
	for _, h := range hosts {
		if ips, err := d.resolve(ctx, h); err == nil {
			d.set(h, ips)
		} else {
			d.logger.Warn().Err(err).Str("host", h).Msg("DNS prime failed")
		}
	}
}

// dialContext dials the cached IP for the target host, falling back to a normal
// dial on any miss or failure.
func (d *dnsCache) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) != nil {
		return d.dialer.DialContext(ctx, network, addr)
	}

	ips, ok := d.get(host)
	if !ok {
		resolved, rerr := d.resolve(ctx, host)
		if rerr != nil {
			return d.dialer.DialContext(ctx, network, addr) // OS-resolved fallback
		}
		d.set(host, resolved)
		ips = resolved
	}

	var lastErr error
	for _, ip := range ips {
		conn, derr := d.dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}

	// Cached IPs all failed (rotation?). Re-resolve fresh and retry.
	if resolved, rerr := d.resolve(ctx, host); rerr == nil {
		d.set(host, resolved)
		for _, ip := range resolved {
			conn, derr := d.dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
			if derr == nil {
				return conn, nil
			}
			lastErr = derr
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return d.dialer.DialContext(ctx, network, addr)
}

func (d *dnsCache) refreshLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.mu.RLock()
			hosts := make([]string, 0, len(d.hosts))
			for h := range d.hosts {
				hosts = append(hosts, h)
			}
			d.mu.RUnlock()
			for _, h := range hosts {
				if ips, err := d.resolve(ctx, h); err == nil {
					d.set(h, ips) // keep last-good on failure
				} else {
					d.logger.Warn().Err(err).Str("host", h).Msg("DNS refresh failed; keeping last good")
				}
			}
		}
	}
}

// warmer keeps `size` connections per host hot by firing cheap HEAD requests on
// an interval. The requests reuse already-warm connections, just resetting their
// idle timers (client- and server-side).
type warmer struct {
	client   *http.Client
	urls     []string
	size     int
	interval time.Duration
	logger   *zerolog.Logger
}

func (w *warmer) loop(ctx context.Context) {
	w.warm(ctx) // warm immediately at startup
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.warm(ctx)
		}
	}
}

func (w *warmer) warm(ctx context.Context) {
	var wg sync.WaitGroup
	for _, url := range w.urls {
		for i := 0; i < w.size; i++ {
			wg.Add(1)
			go func(url string) {
				defer wg.Done()
				req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
				if err != nil {
					return
				}
				resp, err := w.client.Do(req)
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body) // drain so the conn returns to the pool
				_ = resp.Body.Close()
			}(url)
		}
	}
	wg.Wait()
}

// startWarmPool primes the DNS cache and launches the background refresh +
// warmer goroutines for the lifetime of ctx.
func startWarmPool(ctx context.Context, client *http.Client, dc *dnsCache, apiUrl, appUrl string, warm WarmConfig, logger *zerolog.Logger) {
	dc.prime(ctx, dedupe([]string{hostOf(apiUrl), hostOf(appUrl)}))
	go dc.refreshLoop(ctx, warm.dnsRefresh())

	w := &warmer{
		client:   client,
		urls:     dedupe([]string{rootURL(apiUrl), rootURL(appUrl)}),
		size:     warm.PoolSize,
		interval: warm.interval(),
		logger:   logger,
	}
	go w.loop(ctx)

	logger.Info().Int("warmPoolSize", warm.PoolSize).
		Dur("warmInterval", warm.interval()).Dur("dnsRefresh", warm.dnsRefresh()).
		Msg("KTMB warm pool + DNS cache enabled")
}

func dedupe(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
