package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewHandlerAuthRequired(t *testing.T) {
	t.Parallel()

	handlerCalled := false
	handler := NewHandler([]Route{
		{
			Method:       http.MethodPost,
			Path:         "/private",
			AuthRequired: true,
			Handler: func(request Request) (int, error, any) {
				handlerCalled = true
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

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/private", strings.NewReader(`{"broken"`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}
	if handlerCalled {
		t.Fatal("protected handler was called without authorization")
	}

	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/private", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer valid-token")
	handler.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK {
		body, _ := io.ReadAll(authorized.Result().Body)
		t.Fatalf("authorized status = %d, body = %s", authorized.Code, body)
	}
	if !handlerCalled {
		t.Fatal("protected handler was not called with valid authorization")
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
