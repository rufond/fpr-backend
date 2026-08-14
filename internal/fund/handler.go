package fund

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/routes"
	govalidate "github.com/xloss/go-validate"
	"github.com/xloss/go-validate/rules"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type HistoryRequest struct {
	From string `json:"from"`
}

func (h *Handler) State(_ routes.Request) (int, error, any) {
	result, err := h.service.State()
	if err != nil {
		return stateError(err)
	}

	return http.StatusOK, nil, result
}

func (h *Handler) History(request routes.Request) (int, error, any) {
	r, validationErrors := govalidate.Run[HistoryRequest](request.Body, map[string][]any{
		"from": {rules.String{}},
	})
	if len(validationErrors) != 0 {
		return http.StatusUnprocessableEntity, nil, map[string]any{"errors": validationErrors}
	}

	var from *time.Time

	fromText := strings.TrimSpace(r.From)
	if fromText != "" {
		fromDate, errParse := time.Parse("2006-01-02", fromText)
		if errParse != nil {
			return http.StatusUnprocessableEntity, nil, map[string]any{
				"errors": map[string]string{
					"from": "invalid date, expected YYYY-MM-DD",
				},
			}
		}

		from = &fromDate
	}

	result, err := h.service.History(from)
	if err != nil {
		return stateError(err)
	}

	return http.StatusOK, nil, result
}

func stateError(err error) (int, error, any) {
	if errors.Is(err, ErrStateUnavailable) {
		return http.StatusServiceUnavailable, nil, map[string]any{
			"error": "fund state is not available",
		}
	}

	return http.StatusInternalServerError, err, nil
}
