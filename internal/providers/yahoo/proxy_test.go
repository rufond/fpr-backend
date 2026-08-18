package yahoo

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	http "github.com/bogdanfinn/fhttp"
)

type scriptedHTTPClient struct {
	responses []*http.Response
	errors    []error
	requests  []*http.Request
	closed    int
}

func (c *scriptedHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, req)

	var response *http.Response
	if len(c.responses) > 0 {
		response = c.responses[0]
		c.responses = c.responses[1:]
	}

	var err error
	if len(c.errors) > 0 {
		err = c.errors[0]
		c.errors = c.errors[1:]
	}

	return response, err
}

func (c *scriptedHTTPClient) CloseIdleConnections() {
	c.closed++
}

func TestProxyListIsLoadedOnceAndSelectedProxyStaysForRun(t *testing.T) {
	t.Parallel()

	listRequests := 0
	server := httptest.NewServer(stdhttp.HandlerFunc(func(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
		listRequests++
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`["socks5://proxy-one.example:1080","socks5://proxy-two.example:1080"]`))
	}))
	defer server.Close()

	proxyClient := &scriptedHTTPClient{responses: []*http.Response{
		jsonResponse(`{"quotes":[{"symbol":"FRHC"}]}`),
		jsonResponse(`{"quotes":[]}`),
	}}
	factoryCalls := make([]string, 0)
	provider := &Provider{
		batchSize:       20,
		requestDelay:    0,
		proxyMode:       ProxyModeList,
		proxyListURL:    server.URL,
		proxyListClient: server.Client(),
		clientFactory: func(proxyURL string) (httpClient, error) {
			factoryCalls = append(factoryCalls, proxyURL)
			return proxyClient, nil
		},
	}

	result, err := provider.ResolveSymbols(context.Background(), []string{"US3563901046", "KZ1C00001122"})
	if err != nil {
		t.Fatalf("ResolveSymbols() error = %v", err)
	}
	if result.SymbolsByISIN["US3563901046"] != "FRHC" || len(result.MissingISINs) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if listRequests != 1 {
		t.Fatalf("proxy list requests = %d, want 1", listRequests)
	}
	if len(factoryCalls) != 1 || factoryCalls[0] != "socks5://proxy-one.example:1080" {
		t.Fatalf("factory calls = %#v", factoryCalls)
	}
	if len(proxyClient.requests) != 2 {
		t.Fatalf("proxy requests = %d, want 2", len(proxyClient.requests))
	}
	if proxyClient.closed != 1 {
		t.Fatalf("proxy client closed = %d, want 1", proxyClient.closed)
	}
}

func TestProxyListFailsOverAndKeepsNextWorkingProxy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(stdhttp.HandlerFunc(func(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
		_, _ = writer.Write([]byte(`["http://user:secret@proxy-one.example:3128","socks5://proxy-two.example:1080"]`))
	}))
	defer server.Close()

	first := &scriptedHTTPClient{errors: []error{errors.New("dial failed")}}
	second := &scriptedHTTPClient{responses: []*http.Response{
		jsonResponse(`{"quotes":[{"symbol":"FRHC"}]}`),
		jsonResponse(`{"quotes":[]}`),
	}}

	factoryCalls := make([]string, 0)
	provider := &Provider{
		batchSize:       20,
		proxyMode:       ProxyModeList,
		proxyListURL:    server.URL,
		proxyListClient: server.Client(),
		clientFactory: func(proxyURL string) (httpClient, error) {
			factoryCalls = append(factoryCalls, proxyURL)
			if len(factoryCalls) == 1 {
				return first, nil
			}

			return second, nil
		},
	}

	_, err := provider.ResolveSymbols(context.Background(), []string{"US3563901046", "KZ1C00001122"})
	if err != nil {
		t.Fatalf("ResolveSymbols() error = %v", err)
	}
	if len(factoryCalls) != 2 {
		t.Fatalf("factory calls = %#v", factoryCalls)
	}
	if len(first.requests) != 1 || first.closed != 1 {
		t.Fatalf("first proxy requests=%d closed=%d", len(first.requests), first.closed)
	}
	if len(second.requests) != 2 {
		t.Fatalf("second proxy requests = %d, want 2", len(second.requests))
	}
}

func TestProxyFailoverUsesNextProxyForBlockedStatus(t *testing.T) {
	t.Parallel()

	first := &scriptedHTTPClient{responses: []*http.Response{{
		StatusCode: http.StatusTooManyRequests,
		Body:       io.NopCloser(strings.NewReader("rate limited")),
	}}}
	second := &scriptedHTTPClient{responses: []*http.Response{jsonResponse(`{
		"spark":{"result":[{"symbol":"FRHC","response":[{"meta":{"currency":"USD","regularMarketTime":1785771000,"regularMarketPrice":126.32}}]}],"error":null}
	}`)}}

	client := &failoverHTTPClient{
		proxyURLs: []string{"http://proxy-one.example:3128", "http://proxy-two.example:3128"},
		factory: func(proxyURL string) (httpClient, error) {
			if proxyURL == "http://proxy-one.example:3128" {
				return first, nil
			}

			return second, nil
		},
	}
	provider := newProvider(client, 20, 0)

	result, err := provider.fetch(context.Background(), []string{"FRHC"})
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if result.Quotes["FRHC"].Price != "126.32" {
		t.Fatalf("result = %#v", result)
	}
	if len(first.requests) != 1 || len(second.requests) != 1 {
		t.Fatalf("requests first=%d second=%d", len(first.requests), len(second.requests))
	}
}

func TestProxyFailureDoesNotExposeConfiguredURLs(t *testing.T) {
	t.Parallel()

	client := &failoverHTTPClient{
		proxyURLs: []string{
			"http://user:very-secret@proxy-one.example:3128",
			"socks5://user:another-secret@proxy-two.example:1080",
		},
		factory: func(string) (httpClient, error) {
			return nil, errors.New("create proxy client failed")
		},
	}

	req, errRequest := http.NewRequest(http.MethodGet, "https://query1.finance.yahoo.com/test", nil)
	if errRequest != nil {
		t.Fatalf("NewRequest() error = %v", errRequest)
	}

	_, err := client.Do(req)
	if err == nil {
		t.Fatal("Do() error = nil, want error")
	}
	if strings.Contains(err.Error(), "very-secret") || strings.Contains(err.Error(), "another-secret") || strings.Contains(err.Error(), "proxy-one") {
		t.Fatalf("error exposes proxy URL: %v", err)
	}
}

func TestLoadProxyListFiltersInvalidAndDuplicateItems(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(stdhttp.HandlerFunc(func(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
		_, _ = writer.Write([]byte(`[
			" socks5://proxy-one.example:1080 ",
			"broken",
			"socks5://proxy-one.example:1080",
			"http://proxy-two.example:3128",
			""
		]`))
	}))
	defer server.Close()

	provider := &Provider{
		proxyListURL:    server.URL,
		proxyListClient: server.Client(),
	}

	values, err := provider.loadProxyList(context.Background())
	if err != nil {
		t.Fatalf("loadProxyList() error = %v", err)
	}
	if len(values) != 2 || values[0] != "socks5://proxy-one.example:1080" || values[1] != "http://proxy-two.example:3128" {
		t.Fatalf("proxy list = %#v", values)
	}
}

func TestEmptyYahooOperationsDoNotLoadProxyList(t *testing.T) {
	t.Parallel()

	listRequests := 0
	server := httptest.NewServer(stdhttp.HandlerFunc(func(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
		listRequests++
		_, _ = writer.Write([]byte(`["http://proxy.example:3128"]`))
	}))
	defer server.Close()

	provider := &Provider{
		proxyMode:       ProxyModeList,
		proxyListURL:    server.URL,
		proxyListClient: server.Client(),
		clientFactory: func(string) (httpClient, error) {
			t.Fatal("client factory must not be called")
			return nil, nil
		},
	}

	if _, err := provider.FetchPrices(context.Background(), nil); err != nil {
		t.Fatalf("FetchPrices() error = %v", err)
	}
	if _, err := provider.ResolveSymbols(context.Background(), nil); err != nil {
		t.Fatalf("ResolveSymbols() error = %v", err)
	}
	if _, err := provider.FetchDailyPrices(context.Background(), nil, time.Time{}); err != nil {
		t.Fatalf("FetchDailyPrices() error = %v", err)
	}
	if listRequests != 0 {
		t.Fatalf("proxy list requests = %d, want 0", listRequests)
	}
}
