package scheduler

import (
	"errors"
	"net/http"
	"strings"

	govalidate "github.com/xloss/go-validate"
	"github.com/xloss/go-validate/rules"

	"github.com/rufond/fpr-backend/internal/routes"
)

type Handler struct {
	manager *Manager
}

func NewHandler(manager *Manager) *Handler {
	return &Handler{
		manager: manager,
	}
}

func (h *Handler) Jobs(_ routes.Request) (int, error, any) {
	return http.StatusOK, nil, map[string]any{
		"items": h.manager.Jobs(),
	}
}

func (h *Handler) RunJob(req routes.Request) (int, error, any) {
	r, validationErrors := govalidate.Run[RunJobRequest](req.Body, map[string][]any{
		"key": {rules.Required{}, rules.String{}},
	})

	if len(validationErrors) != 0 {
		return http.StatusUnprocessableEntity, nil, map[string]any{"errors": validationErrors}
	}

	r.Key = strings.TrimSpace(r.Key)

	result, err := h.manager.RunNow(req.Context, r.Key)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			return http.StatusNotFound, nil, map[string]any{
				"errors": map[string]string{
					"key": "scheduler job not found",
				},
			}
		}

		if errors.Is(err, ErrJobAlreadyRunning) {
			return http.StatusConflict, nil, map[string]any{
				"errors": map[string]string{
					"key": "scheduler job is already running",
				},
			}
		}

		return http.StatusInternalServerError, err, nil
	}

	return http.StatusOK, nil, result
}
