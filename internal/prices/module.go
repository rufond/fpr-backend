package prices

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rufond/fpr-backend/internal/appstate"
)

type Module struct {
	Repository *Repository
	Service    *Service
}

func NewModule(db *pgxpool.Pool, source Source, yahoo YahooSource, state *appstate.Manager) *Module {
	repository := NewRepository(db)
	service := NewService(repository, source, yahoo, state)

	return &Module{
		Repository: repository,
		Service:    service,
	}
}
