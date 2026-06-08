package ktmb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"io"
	"math/rand"
	"net/http"
	u "net/url"
	"strings"
)

type HttpConfig struct {
	Url    string
	Header map[string]string
	logger *zerolog.Logger
	proxy  *string
}

type HttpClient[T any, Y any] struct {
	Url    string
	Header map[string]string
	logger *zerolog.Logger
	proxy  *string
}

func NewHttp[T any, Y any](c HttpConfig) HttpClient[T, Y] {
	k := HttpClient[T, Y]{
		Url:    c.Url,
		Header: c.Header,
		logger: c.logger,
		proxy:  c.proxy,
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

func (k HttpClient[T, Y]) client() (*http.Client, error) {
	if k.proxy == nil {
		return &http.Client{}, nil
	}

	proxies := strings.Split(*k.proxy, ";")
	randomIndex := rand.Intn(len(proxies))
	p := proxies[randomIndex]

	pUrl, err := u.Parse(p)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to parse URL")
		return nil, err
	}
	proxy := http.ProxyURL(pUrl)
	return &http.Client{
		Transport: &http.Transport{
			Proxy: proxy,
		},
	}, nil
}

func (k HttpClient[T, Y]) Send(method string, path string, headers ...map[string]string) (Y, error) {
	url := fmt.Sprintf("%s/%s", k.Url, path)

	var y Y

	body := strings.NewReader("")

	req, err := http.NewRequest(method, url, body)
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

	client, err := k.client()
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to create client")
		return y, err
	}

	res, err := client.Do(req)
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
			return y, fmt.Errorf("status code %d", res.StatusCode)
		} else {
			return y, fmt.Errorf("status code %d, body %s", res.StatusCode, string(resp))
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
	url := fmt.Sprintf("%s/%s", k.Url, path)

	var y Y

	marshal, err := json.Marshal(payload)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to marshal payload")
		return y, err
	}

	body := bytes.NewReader(marshal)
	req, err := http.NewRequest(method, url, body)
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

	client, err := k.client()
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to create client")
		return y, err
	}

	res, err := client.Do(req)
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
		return y, fmt.Errorf("status code %d, body %s", res.StatusCode, string(resp))
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

	client, err := k.client()
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to create client")
		return nil, err
	}

	res, err := client.Do(req)
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
		return nil, fmt.Errorf("status code %d, body %s", res.StatusCode, string(bin))
	}

	return bin, nil
}
