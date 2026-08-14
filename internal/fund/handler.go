package fund

import (
	"errors"
	"net/http"

	"github.com/rufond/fpr-backend/internal/routes"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) State(_ routes.Request) (int, error, any) {
	result, err := h.service.State()
	if err != nil {
		if errors.Is(err, ErrStateUnavailable) {
			return http.StatusServiceUnavailable, nil, map[string]any{
				"error": "fund state is not available",
			}
		}

		return http.StatusInternalServerError, err, nil
	}

	return http.StatusOK, nil, result
}
