package valuation

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rufond/fpr-backend/internal/appstate"
)

type Module struct {
	Repository *Repository
	Service    *Service
}

func NewModule(db *pgxpool.Pool, state *appstate.Manager, prices HistoricalPriceSource, fx HistoricalFXSource) *Module {
	repository := NewRepository(db)

	return &Module{
		Repository: repository,
		Service:    NewService(repository, state, prices, fx),
	}
}
