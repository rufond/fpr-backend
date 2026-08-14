package fund

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rufond/fpr-backend/internal/appstate"
)

type Module struct {
	Handler    *Handler
	Repository *Repository
	Service    *Service
}

func NewModule(db *pgxpool.Pool, source ManagementCompanySource, state *appstate.Manager) *Module {
	repository := NewRepository(db)
	service := NewService(repository, source, state)
	handler := NewHandler(service)

	return &Module{
		Handler:    handler,
		Repository: repository,
		Service:    service,
	}
}
