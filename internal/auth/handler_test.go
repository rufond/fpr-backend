package auth

import (
	"net/http"
	"testing"

	"github.com/rufond/fpr-backend/internal/routes"
)

func TestHandlerLogin(t *testing.T) {
	t.Parallel()

	handler := NewHandler(newTestService(t))
	status, err, content := handler.Login(routes.Request{Body: map[string]any{
		"login":    "admin",
		"password": "secret",
	}})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("Login() status = %d, want %d", status, http.StatusOK)
	}

	result, ok := content.(LoginResult)
	if !ok || result.Token == "" {
		t.Fatalf("Login() content = %#v", content)
	}
}

func TestHandlerLoginValidation(t *testing.T) {
	t.Parallel()

	handler := NewHandler(newTestService(t))
	status, err, _ := handler.Login(routes.Request{Body: map[string]any{}})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("Login() status = %d, want %d", status, http.StatusUnprocessableEntity)
	}
}

func TestHandlerLoginInvalidCredentials(t *testing.T) {
	t.Parallel()

	handler := NewHandler(newTestService(t))
	status, err, content := handler.Login(routes.Request{Body: map[string]any{
		"login":    "admin",
		"password": "wrong",
	}})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("Login() status = %d, want %d", status, http.StatusUnauthorized)
	}

	body, ok := content.(map[string]any)
	if !ok || body["error"] != "invalid credentials" {
		t.Fatalf("Login() content = %#v", content)
	}
}
