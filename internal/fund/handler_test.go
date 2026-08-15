package fund

import (
	"net/http"
	"testing"

	"github.com/rufond/fpr-backend/internal/routes"
)

func TestHandlerState(t *testing.T) {
	t.Parallel()

	handler := NewHandler(NewService(nil, nil, testStateManager(t, 15)))
	status, err, content := handler.State(routes.Request{})
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if _, ok := content.(*StateResult); !ok {
		t.Fatalf("content = %T, want *StateResult", content)
	}
}

func TestHandlerHistory(t *testing.T) {
	t.Parallel()

	handler := NewHandler(NewService(nil, nil, testStateManager(t, 15)))
	status, err, content := handler.History(routes.Request{
		Body: map[string]any{"from": "2026-08-11"},
	})
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	result, ok := content.(*HistoryResult)
	if !ok {
		t.Fatalf("content = %T, want *HistoryResult", content)
	}
	if len(result.DailyValues) != 2 || result.DailyValues[0].AsOfDate != "2026-08-11" {
		t.Fatalf("daily values = %#v", result.DailyValues)
	}
	if len(result.UnitMarketPrices) != 2 || result.UnitMarketPrices[0].AsOfDate != "2026-08-11" {
		t.Fatalf("unit market prices = %#v", result.UnitMarketPrices)
	}
}

func TestHandlerHistoryWithoutFrom(t *testing.T) {
	t.Parallel()

	handler := NewHandler(NewService(nil, nil, testStateManager(t, 15)))
	status, err, content := handler.History(routes.Request{Body: map[string]any{}})
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	result, ok := content.(*HistoryResult)
	if !ok {
		t.Fatalf("content = %T, want *HistoryResult", content)
	}
	if len(result.DailyValues) != 3 {
		t.Fatalf("len(daily values) = %d, want 3", len(result.DailyValues))
	}
	if len(result.UnitMarketPrices) != 3 {
		t.Fatalf("len(unit market prices) = %d, want 3", len(result.UnitMarketPrices))
	}
}

func TestHandlerHistoryRejectsInvalidFrom(t *testing.T) {
	t.Parallel()

	handler := NewHandler(NewService(nil, nil, testStateManager(t, 15)))
	status, err, content := handler.History(routes.Request{
		Body: map[string]any{"from": "11.08.2026"},
	})
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}

	body, ok := content.(map[string]any)
	if !ok {
		t.Fatalf("content = %T, want map[string]any", content)
	}

	errorsMap, ok := body["errors"].(map[string]string)
	if !ok || errorsMap["from"] != "invalid date, expected YYYY-MM-DD" {
		t.Fatalf("content = %#v", content)
	}
}

func TestHandlerHistoryRejectsInvalidFromType(t *testing.T) {
	t.Parallel()

	handler := NewHandler(NewService(nil, nil, testStateManager(t, 15)))
	status, err, content := handler.History(routes.Request{
		Body: map[string]any{"from": 20260811},
	})
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}

	body, ok := content.(map[string]any)
	if !ok || body["errors"] == nil {
		t.Fatalf("content = %#v", content)
	}
}

func TestHandlerStateUnavailable(t *testing.T) {
	t.Parallel()

	handler := NewHandler(NewService(nil, nil, nil))
	status, err, _ := handler.State(routes.Request{})
	if err == nil {
		t.Fatal("State() error = nil, want configuration error")
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
	}
}
