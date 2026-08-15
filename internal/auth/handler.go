package auth

import (
	"errors"
	"net/http"

	govalidate "github.com/xloss/go-validate"
	"github.com/xloss/go-validate/rules"

	"github.com/rufond/fpr-backend/internal/routes"
)

type LoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type LoginResult struct {
	Token string `json:"token"`
}

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Login(req routes.Request) (int, error, any) {
	input, validationErrors := govalidate.Run[LoginRequest](req.Body, map[string][]any{
		"login":    {rules.Required{}, rules.String{}},
		"password": {rules.Required{}, rules.String{}},
	})
	if len(validationErrors) != 0 {
		return http.StatusUnprocessableEntity, nil, map[string]any{"errors": validationErrors}
	}

	token, err := h.service.Login(input.Login, input.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return http.StatusUnauthorized, nil, map[string]any{"error": "invalid credentials"}
		}

		return http.StatusInternalServerError, err, nil
	}

	return http.StatusOK, nil, LoginResult{Token: token}
}

func (h *Handler) Logout(req routes.Request) (int, error, any) {
	h.service.Logout(req.User.Token)
	return http.StatusOK, nil, struct{}{}
}
