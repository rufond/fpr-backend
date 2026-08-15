package managementcompany

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rufond/fpr-backend/internal/providers"
)

func TestProviderFetch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("User-Agent"); got != providers.UserAgent {
			t.Fatalf("User-Agent = %q, want %q", got, providers.UserAgent)
		}
		if got := r.Header.Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("Cache-Control = %q, want no-cache", got)
		}
		_, _ = io.WriteString(w, "<html>ok</html>")
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	body, err := provider.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := string(body); got != "<html>ok</html>" {
		t.Fatalf("body = %q", got)
	}
}

func TestProviderFetchRejectsUnexpectedStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	_, err := provider.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP status 502") {
		t.Fatalf("Fetch() error = %v, want status error", err)
	}
}

func TestProviderFetchRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxResponseSize+1))
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	_, err := provider.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("Fetch() error = %v, want response size error", err)
	}
}
