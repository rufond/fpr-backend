package diagnostics

import (
	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type Module struct {
	Handler *Handler
	Service *Service
}

func NewModule(
	state *appstate.Manager,
	schedulerManager *scheduler.Manager,
	schedulerRepository *scheduler.Repository,
	priceService *prices.Service,
) *Module {
	service := NewService(state, schedulerManager, schedulerRepository, priceService)

	return &Module{
		Handler: NewHandler(service),
		Service: service,
	}
}
