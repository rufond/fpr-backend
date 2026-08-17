package prices

import (
	"context"
	"net/http"
	"testing"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/routes"
)

type testPricePublisher struct {
	updates []realtime.Update
}

func (p *testPricePublisher) Publish(update realtime.Update) {
	p.updates = append(p.updates, update)
}

func TestHandlerSetSourceNormalizesInputAndPublishesInvalidation(t *testing.T) {
	t.Parallel()

	instrumentID := int64(51)
	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{
		Fund: &appstate.FundState{Snapshot: appstate.FundSnapshot{Assets: []appstate.FundAsset{
			{InstrumentID: &instrumentID},
		}}},
		Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{
		setStored: storedPriceSource{
			ID:             15,
			InstrumentID:   instrumentID,
			Provider:       ProviderMOEX,
			ProviderSymbol: "SPBE",
			Enabled:        true,
		},
		setChanged: true,
		setState:   &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}},
	}
	publisher := &testPricePublisher{}
	handler := NewHandler(NewService(repository, nil, nil, manager), publisher)

	status, err, content := handler.SetSource(routes.Request{
		Context: context.Background(),
		Body: map[string]any{
			"instrument_id":   instrumentID,
			"provider":        " MOEX ",
			"provider_symbol": " spbe ",
			"enabled":         true,
		},
	})
	if err != nil {
		t.Fatalf("SetSource() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("SetSource() status = %d, want %d", status, http.StatusOK)
	}
	if repository.setArgs.provider != ProviderMOEX || repository.setArgs.providerSymbol != "SPBE" || !repository.setArgs.enabled {
		t.Fatalf("set args = %#v", repository.setArgs)
	}
	if _, ok := content.(*SetPriceSourceResult); !ok {
		t.Fatalf("SetSource() content = %#v", content)
	}
	if len(publisher.updates) != 1 || len(publisher.updates[0].InstrumentIDs) != 1 || publisher.updates[0].InstrumentIDs[0] != instrumentID {
		t.Fatalf("updates = %#v", publisher.updates)
	}
}

func TestHandlerSetSourceRejectsEnabledProviderWithoutImplementation(t *testing.T) {
	t.Parallel()

	handler := NewHandler(NewService(&fakePriceRepository{}, nil, nil, appstate.NewManager()), nil)
	status, err, _ := handler.SetSource(routes.Request{
		Context: context.Background(),
		Body: map[string]any{
			"instrument_id":   int64(51),
			"provider":        "kase",
			"provider_symbol": "KMGZ",
			"enabled":         true,
		},
	})
	if err != nil {
		t.Fatalf("SetSource() error = %v", err)
	}
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("SetSource() status = %d, want %d", status, http.StatusUnprocessableEntity)
	}
}

func TestHandlerSetSourceAllowsDisabledProviderWithoutImplementation(t *testing.T) {
	t.Parallel()

	instrumentID := int64(51)
	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{
		Fund: &appstate.FundState{Snapshot: appstate.FundSnapshot{Assets: []appstate.FundAsset{
			{InstrumentID: &instrumentID},
		}}},
		Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{
		setStored: storedPriceSource{
			ID:             16,
			InstrumentID:   instrumentID,
			Provider:       ProviderKASE,
			ProviderSymbol: "KMGZ",
			Enabled:        false,
		},
		setChanged: true,
		setState:   &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}},
	}
	handler := NewHandler(NewService(repository, nil, nil, manager), nil)

	status, err, _ := handler.SetSource(routes.Request{
		Context: context.Background(),
		Body: map[string]any{
			"instrument_id":   instrumentID,
			"provider":        "kase",
			"provider_symbol": "KMGZ",
			"enabled":         false,
		},
	})
	if err != nil {
		t.Fatalf("SetSource() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("SetSource() status = %d, want %d", status, http.StatusOK)
	}
	if repository.setArgs.provider != ProviderKASE || repository.setArgs.providerSymbol != "KMGZ" || repository.setArgs.enabled {
		t.Fatalf("set args = %#v", repository.setArgs)
	}
}

func TestHandlerSetSourceRequiresEnabled(t *testing.T) {
	t.Parallel()

	handler := NewHandler(NewService(&fakePriceRepository{}, nil, nil, appstate.NewManager()), nil)
	status, err, _ := handler.SetSource(routes.Request{
		Context: context.Background(),
		Body: map[string]any{
			"instrument_id":   int64(51),
			"provider":        "moex",
			"provider_symbol": "SPBE",
		},
	})
	if err != nil {
		t.Fatalf("SetSource() error = %v", err)
	}
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("SetSource() status = %d, want %d", status, http.StatusUnprocessableEntity)
	}
}
