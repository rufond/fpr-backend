package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"strings"

	http "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"

	"github.com/rufond/fpr-backend/internal/providers"
)

const (
	ProxyModeDisabled = "disabled"
	ProxyModeSingle   = "single"
	ProxyModeList     = "list"

	maxProxyListResponseSize = 256 << 10
	maxProxyListItems        = 1000
)

type ProxyConfig struct {
	Mode    string
	URL     string
	ListURL string
}

type proxyListHTTPClient interface {
	Do(req *stdhttp.Request) (*stdhttp.Response, error)
}

type httpClientFactory func(proxyURL string) (httpClient, error)

type failoverHTTPClient struct {
	proxyURLs []string
	factory   httpClientFactory

	index  int
	client httpClient
}

func newTLSClient(proxyURL string) (httpClient, error) {
	options := []tlsclient.HttpClientOption{
		tlsclient.WithTimeoutMilliseconds(int(defaultTimeout.Milliseconds())),
		tlsclient.WithClientProfile(profiles.Chrome_146),
		tlsclient.WithDisableHttp3(),
	}
	if proxyURL != "" {
		options = append(options, tlsclient.WithProxyUrl(proxyURL))
	}

	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (p *Provider) runProvider(ctx context.Context) (*Provider, func(), error) {
	if p.proxyMode != ProxyModeList {
		return p, func() {}, nil
	}

	proxyURLs, errList := p.loadProxyList(ctx)
	if errList != nil {
		return nil, nil, errList
	}

	client := &failoverHTTPClient{
		proxyURLs: proxyURLs,
		factory:   p.clientFactory,
	}

	run := *p
	run.client = client

	return &run, client.CloseIdleConnections, nil
}

func (p *Provider) loadProxyList(ctx context.Context) ([]string, error) {
	req, errRequest := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, p.proxyListURL, nil)
	if errRequest != nil {
		return nil, fmt.Errorf("create Yahoo proxy list request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", providers.UserAgent)

	resp, errDo := p.proxyListClient.Do(req)
	if errDo != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		return nil, fmt.Errorf("request Yahoo proxy list failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < stdhttp.StatusOK || resp.StatusCode >= stdhttp.StatusMultipleChoices {
		return nil, fmt.Errorf("Yahoo proxy list returned HTTP %d", resp.StatusCode)
	}

	body, errRead := io.ReadAll(io.LimitReader(resp.Body, maxProxyListResponseSize+1))
	if errRead != nil {
		return nil, fmt.Errorf("read Yahoo proxy list: %w", errRead)
	}
	if len(body) > maxProxyListResponseSize {
		return nil, fmt.Errorf("Yahoo proxy list response exceeds %d bytes", maxProxyListResponseSize)
	}

	var values []string
	if errDecode := json.Unmarshal(body, &values); errDecode != nil {
		return nil, fmt.Errorf("decode Yahoo proxy list: %w", errDecode)
	}

	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		parsed, errParse := url.Parse(value)
		if errParse != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) > maxProxyListItems {
			return nil, fmt.Errorf("Yahoo proxy list exceeds %d valid items", maxProxyListItems)
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("Yahoo proxy list contains no valid proxy URLs")
	}

	return result, nil
}

func (c *failoverHTTPClient) Do(req *http.Request) (*http.Response, error) {
	attempts := 0

	for c.index < len(c.proxyURLs) {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}

		if c.client == nil {
			client, errClient := c.factory(c.proxyURLs[c.index])
			if errClient != nil {
				c.index++
				attempts++

				continue
			}

			c.client = client
		}

		attempt := req.WithContext(req.Context())

		resp, errDo := c.client.Do(attempt)
		if errDo == nil && !proxyFailoverStatus(resp.StatusCode) {
			return resp, nil
		}

		if resp != nil {
			_ = resp.Body.Close()
		}

		c.client.CloseIdleConnections()
		c.client = nil

		c.index++
		attempts++
	}

	return nil, fmt.Errorf("Yahoo request failed through all configured proxies after %d attempts", attempts)
}

func (c *failoverHTTPClient) CloseIdleConnections() {
	if c == nil || c.client == nil {
		return
	}

	c.client.CloseIdleConnections()
}

func proxyFailoverStatus(status int) bool {
	switch status {
	case stdhttp.StatusForbidden,
		stdhttp.StatusProxyAuthRequired,
		stdhttp.StatusRequestTimeout,
		stdhttp.StatusTooManyRequests,
		stdhttp.StatusBadGateway,
		stdhttp.StatusServiceUnavailable,
		stdhttp.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
