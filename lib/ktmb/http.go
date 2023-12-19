package ktmb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"io"
	"net/http"
	"strings"
)

type HttpConfig struct {
	Url    string
	Header map[string]string
	logger *zerolog.Logger
}

type HttpClient[T any, Y any] struct {
	Url    string
	Header map[string]string
	logger *zerolog.Logger
}

func NewHttp[T any, Y any](c HttpConfig) HttpClient[T, Y] {
	k := HttpClient[T, Y]{
		Url:    c.Url,
		Header: c.Header,
		logger: c.logger,
	}
	return k
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

	res, err := http.DefaultClient.Do(req)
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

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to send request")
		return y, err
	}
	defer res.Body.Close()
	err = json.NewDecoder(res.Body).Decode(&y)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to decode response")
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

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to send request")
		return nil, err
	}
	defer res.Body.Close()
	bin, err := io.ReadAll(res.Body)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to decode response")
		return nil, err
	}
	return bin, nil
}
