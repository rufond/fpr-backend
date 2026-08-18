package diagnostics

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/routes"
)

func TestHandlerList(t *testing.T) {
	state := initializedMinimalState(t)
	service := NewService(
		state,
		schedulerJobsStub{},
		schedulerRunsStub{},
		priceSourcesStub{err: errors.New("database unavailable")},
	)
	handler := NewHandler(service)

	status, err, body := handler.List(routes.Request{Context: context.Background()})
	if status != http.StatusInternalServerError {
		t.Fatalf("status=%d, want %d", status, http.StatusInternalServerError)
	}
	if err == nil {
		t.Fatal("expected handler error")
	}
	if body != nil {
		t.Fatalf("body=%v, want nil", body)
	}
}

func TestHandlerListSuccess(t *testing.T) {
	state := initializedMinimalState(t)
	service := NewService(
		state,
		schedulerJobsStub{},
		schedulerRunsStub{},
		priceSourcesStub{result: emptyPriceSourcesResult()},
	)
	handler := NewHandler(service)

	status, err, body := handler.List(routes.Request{Context: context.Background()})
	if err != nil {
		t.Fatalf("list diagnostics: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d, want %d", status, http.StatusOK)
	}

	result, ok := body.(*Result)
	if !ok {
		t.Fatalf("body type=%T, want *Result", body)
	}
	if result.Total != 1 || result.Items[0].Type != "fund_unit_price_missing" {
		t.Fatalf("unexpected diagnostics result: %+v", result)
	}
}

func emptyPriceSourcesResult() *prices.AdminPriceSourcesResult {
	return &prices.AdminPriceSourcesResult{Items: []prices.AdminPriceSourceInstrument{}}
}
