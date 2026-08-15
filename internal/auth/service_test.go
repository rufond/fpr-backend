package auth

import (
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestServiceLoginAndResolveUser(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	token, errLogin := service.Login("admin", "secret")
	if errLogin != nil {
		t.Fatalf("Login() error = %v", errLogin)
	}
	if token == "" {
		t.Fatal("Login() token is empty")
	}

	user := service.ResolveUser(token)
	if user == nil || user.Login != "admin" || user.Token != token {
		t.Fatalf("ResolveUser() = %#v", user)
	}
}

func TestServiceLoginRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()

	service := newTestService(t)

	for _, test := range []struct {
		name     string
		login    string
		password string
	}{
		{name: "login", login: "other", password: "secret"},
		{name: "password", login: "admin", password: "wrong"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := service.Login(test.login, test.password)
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestServiceLogoutInvalidatesOnlyRequestedSession(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	first, errFirst := service.Login("admin", "secret")
	if errFirst != nil {
		t.Fatalf("first Login() error = %v", errFirst)
	}
	second, errSecond := service.Login("admin", "secret")
	if errSecond != nil {
		t.Fatalf("second Login() error = %v", errSecond)
	}

	service.Logout(first)

	if service.ResolveUser(first) != nil {
		t.Fatal("logged out session is still valid")
	}
	if service.ResolveUser(second) == nil {
		t.Fatal("unrelated session was invalidated")
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}

	return NewService("admin", string(hash))
}
