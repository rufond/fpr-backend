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
