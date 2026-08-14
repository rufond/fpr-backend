package managementcompany

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/fund"
)

const (
	DefaultTimeout  = 30 * time.Second
	maxResponseSize = 16 << 20
	userAgent       = "Mozilla/5.0 (compatible; FPR/1.0; +https://github.com/rufond/fpr-backend)"
)

type Provider struct {
	url    string
	client *http.Client
}

func NewProvider(rawURL string, client *http.Client) *Provider {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}

	return &Provider{
		url:    strings.TrimSpace(rawURL),
		client: client,
	}
}

func (p *Provider) Fetch(ctx context.Context) ([]byte, error) {
	if p == nil || p.client == nil || p.url == "" {
		return nil, fmt.Errorf("management company provider is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return nil, fmt.Errorf("create management company request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch management company fund page: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, fmt.Errorf("fetch management company fund page: unexpected HTTP status %d", resp.StatusCode)
	}

	reader := io.LimitReader(resp.Body, maxResponseSize+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read management company fund page: %w", err)
	}
	if len(body) > maxResponseSize {
		return nil, fmt.Errorf("read management company fund page: response exceeds %d bytes", maxResponseSize)
	}

	return body, nil
}
func (p *Provider) FetchPage(ctx context.Context) (*fund.SourcePage, error) {
	body, err := p.Fetch(ctx)
	if err != nil {
		return nil, err
	}

	page, err := ParsePage(body)
	if err != nil {
		return nil, fmt.Errorf("parse management company fund page: %w", err)
	}

	return page, nil
}
