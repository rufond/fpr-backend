package prices

import (
	"errors"
	"net/http"
	"strings"

	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/routes"
	govalidate "github.com/xloss/go-validate"
	"github.com/xloss/go-validate/rules"
)

type Handler struct {
	service   *Service
	publisher realtime.Publisher
}

func NewHandler(service *Service, publisher realtime.Publisher) *Handler {
	if publisher == nil {
		publisher = realtime.DiscardPublisher{}
	}

	return &Handler{
		service:   service,
		publisher: publisher,
	}
}

type SetPriceSourceRequest struct {
	InstrumentID   int64  `json:"instrument_id"`
	Provider       string `json:"provider"`
	ProviderSymbol string `json:"provider_symbol"`
	Enabled        bool   `json:"enabled"`
}

func (h *Handler) Sources(request routes.Request) (int, error, any) {
	result, err := h.service.AdminPriceSources(request.Context)
	if err != nil {
		return http.StatusInternalServerError, err, nil
	}

	return http.StatusOK, nil, result
}

func (h *Handler) SetSource(request routes.Request) (int, error, any) {
	if _, exists := request.Body["enabled"]; !exists {
		return http.StatusUnprocessableEntity, nil, map[string]any{
			"errors": map[string]string{"enabled": "field is required"},
		}
	}

	input, validationErrors := govalidate.Run[SetPriceSourceRequest](request.Body, map[string][]any{
		"instrument_id":   {rules.Required{}, rules.Integer{}, rules.Min{Min: 1}},
		"provider":        {rules.Required{}, rules.String{}},
		"provider_symbol": {rules.Required{}, rules.String{}},
	})
	if len(validationErrors) != 0 {
		return http.StatusUnprocessableEntity, nil, map[string]any{"errors": validationErrors}
	}

	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.ProviderSymbol = strings.ToUpper(strings.TrimSpace(input.ProviderSymbol))

	switch input.Provider {
	case ProviderMOEX, ProviderYahoo:
	case ProviderKASE:
		if input.Enabled {
			return http.StatusUnprocessableEntity, nil, map[string]any{
				"errors": map[string]string{
					"enabled": ErrPriceProviderNotImplemented.Error(),
				},
			}
		}
	default:
		return http.StatusUnprocessableEntity, nil, map[string]any{
			"errors": map[string]string{
				"provider": ErrUnsupportedPriceProvider.Error(),
			},
		}
	}

	result, err := h.service.SetPriceSource(request.Context, input.InstrumentID, input.Provider, input.ProviderSymbol, input.Enabled)
	if err != nil {
		if errors.Is(err, ErrPriceSourceInstrumentNotFound) {
			return http.StatusNotFound, nil, map[string]any{
				"errors": map[string]string{
					"instrument_id": err.Error(),
				},
			}
		}

		return http.StatusInternalServerError, err, nil
	}

	if result.Changed {
		h.publisher.Publish(realtime.Update{
			Scopes:        []string{realtime.ScopeInstrumentPrices},
			InstrumentIDs: []int64{result.InstrumentID},
		})
	}

	return http.StatusOK, nil, result
}
