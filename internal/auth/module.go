package auth

type Module struct {
	Handler *Handler
	Service *Service
}

func NewModule(login string, passwordHash string) *Module {
	service := NewService(login, passwordHash)

	return &Module{
		Handler: NewHandler(service),
		Service: service,
	}
}
