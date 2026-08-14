package fund

import (
	"net/http"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/routes"
)

func TestHandlerState(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{
		Snapshot: appstate.FundSnapshot{
			AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
			ObservedAt:             time.Date(2026, time.August, 13, 16, 20, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "31.18",
			NAVUSD:                 "492986650.00",
			Assets:                 []appstate.FundAsset{},
			Categories:             []appstate.FundCategory{},
		},
		DailyValues: []appstate.FundDailyValue{},
	}}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	handler := NewHandler(NewService(nil, nil, manager))
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

func TestHandlerStateUnavailable(t *testing.T) {
	t.Parallel()

	handler := NewHandler(NewService(nil, nil, appstate.NewManager()))
	status, err, content := handler.State(routes.Request{})
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", status, http.StatusServiceUnavailable)
	}

	body, ok := content.(map[string]any)
	if !ok || body["error"] != "fund state is not available" {
		t.Fatalf("content = %#v", content)
	}
}
