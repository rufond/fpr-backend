package diagnostics

import (
	"net/http"

	"github.com/rufond/fpr-backend/internal/routes"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(request routes.Request) (int, error, any) {
	result, err := h.service.List(request.Context)
	if err != nil {
		return http.StatusInternalServerError, err, nil
	}

	return http.StatusOK, nil, result
}
