package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewHandlerAuthRequiredRejectsBeforeHandler(t *testing.T) {
	t.Parallel()

	handler := NewHandler([]Route{
		{
			Method:       http.MethodPost,
			Path:         "/private",
			AuthRequired: true,
			Handler: func(Request) (int, error, any) {
				t.Fatal("protected handler was called without authorization")
				return 0, nil, nil
			},
		},
	}, nil, func(string) *User { return nil })

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/private", strings.NewReader(`{"broken"`)))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNewHandlerAuthRequiredPassesKnownUser(t *testing.T) {
	t.Parallel()

	handler := NewHandler([]Route{
		{
			Method:       http.MethodPost,
			Path:         "/private",
			AuthRequired: true,
			Handler: func(request Request) (int, error, any) {
				if request.User == nil || request.User.Login != "admin" || request.User.Token != "valid-token" {
					t.Fatalf("request.User = %#v", request.User)
				}
				return http.StatusOK, nil, map[string]any{"ok": true}
			},
		},
	}, nil, func(token string) *User {
		if token == "valid-token" {
			return &User{Login: "admin", Token: token}
		}
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/private", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer valid-token")
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		body, _ := io.ReadAll(response.Result().Body)
		t.Fatalf("status = %d, body = %s", response.Code, body)
	}
}

func TestNewHandlerPublicRouteReceivesKnownUser(t *testing.T) {
	t.Parallel()

	handler := NewHandler([]Route{
		{
			Method: http.MethodGet,
			Path:   "/public",
			Handler: func(request Request) (int, error, any) {
				if request.User == nil || request.User.Login != "admin" {
					t.Fatalf("request.User = %#v", request.User)
				}
				return http.StatusOK, nil, struct{}{}
			},
		},
	}, nil, func(token string) *User {
		return &User{Login: "admin", Token: token}
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/public", nil)
	request.Header.Set("Authorization", "bearer token")
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestNewHandlerRejectsOversizedJSON(t *testing.T) {
	t.Parallel()

	handler := NewHandler([]Route{
		{
			Method: http.MethodPost,
			Path:   "/data",
			Handler: func(Request) (int, error, any) {
				t.Fatal("handler was called with an oversized request")
				return 0, nil, nil
			},
		},
	}, nil, nil)

	response := httptest.NewRecorder()
	body := `{"value":"` + strings.Repeat("x", maxRequestBodyBytes) + `"}`
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/data", strings.NewReader(body)))

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestNewHandlerRawBodyRequiresAuthAndHonorsItsLimit(t *testing.T) {
	t.Parallel()
	const input = "{\"date\":1}\n{\"date\":2}\n"
	handler := NewHandler([]Route{{
		Method: http.MethodPost, Path: "/import", AuthRequired: true, MaxRawBodyBytes: int64(len(input)),
		Handler: func(request Request) (int, error, any) {
			if string(request.RawBody) != input || request.Body != nil {
				t.Fatalf("expected the original NDJSON body, got %q", request.RawBody)
			}
			return http.StatusOK, nil, nil
		},
	}}, nil, func(token string) *User {
		if token == "valid-token" {
			return &User{Login: "admin"}
		}
		return nil
	})

	for _, test := range []struct {
		name, token, body string
		status            int
	}{
		{"anonymous", "", input, http.StatusUnauthorized},
		{"authorized", "valid-token", input, http.StatusOK},
		{"oversized", "valid-token", input + " ", http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/import", strings.NewReader(test.body))
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}
