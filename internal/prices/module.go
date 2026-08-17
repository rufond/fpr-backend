package prices

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/realtime"
)

type Module struct {
	Repository *Repository
	Service    *Service
	Handler    *Handler
}

func NewModule(db *pgxpool.Pool, source Source, yahoo YahooSource, state *appstate.Manager, publisher realtime.Publisher) *Module {
	repository := NewRepository(db)
	service := NewService(repository, source, yahoo, state)

	return &Module{
		Repository: repository,
		Service:    service,
		Handler:    NewHandler(service, publisher),
	}
}
