package fund

import "github.com/jackc/pgx/v5/pgxpool"

type Module struct {
	Repository *Repository
	Service    *Service
}

func NewModule(db *pgxpool.Pool, source ManagementCompanySource) *Module {
	repository := NewRepository(db)
	service := NewService(repository, source)

	return &Module{
		Repository: repository,
		Service:    service,
	}
}
