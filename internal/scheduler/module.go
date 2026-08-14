package scheduler

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rufond/fpr-backend/internal/realtime"
)

type Module struct {
	Handler    *Handler
	Manager    *Manager
	Repository *Repository
}

func NewModule(db *pgxpool.Pool, realtimePublisher realtime.Publisher) *Module {
	repository := NewRepository(db)
	manager := NewManager(repository, realtimePublisher)
	handler := NewHandler(manager)

	return &Module{
		Handler:    handler,
		Manager:    manager,
		Repository: repository,
	}
}
