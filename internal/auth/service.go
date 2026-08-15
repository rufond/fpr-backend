package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/rufond/fpr-backend/internal/routes"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type Service struct {
	login        string
	passwordHash []byte

	mu       sync.RWMutex
	sessions map[string]struct{}
}

func NewService(login string, passwordHash string) *Service {
	return &Service{
		login:        strings.TrimSpace(login),
		passwordHash: []byte(strings.TrimSpace(passwordHash)),
		sessions:     make(map[string]struct{}),
	}
}

func (s *Service) Login(login string, password string) (string, error) {
	requestedLoginHash := sha256.Sum256([]byte(strings.TrimSpace(login)))
	configuredLoginHash := sha256.Sum256([]byte(s.login))
	loginMatches := subtle.ConstantTimeCompare(requestedLoginHash[:], configuredLoginHash[:]) == 1
	passwordMatches := bcrypt.CompareHashAndPassword(s.passwordHash, []byte(password)) == nil

	if !loginMatches || !passwordMatches {
		return "", ErrInvalidCredentials
	}

	token := uuid.NewString()

	s.mu.Lock()
	s.sessions[token] = struct{}{}
	s.mu.Unlock()

	return token, nil
}

func (s *Service) Logout(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *Service) ResolveUser(token string) *routes.User {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}

	s.mu.RLock()
	_, exists := s.sessions[token]
	s.mu.RUnlock()
	if !exists {
		return nil
	}

	return &routes.User{
		Login: s.login,
		Token: token,
	}
}
