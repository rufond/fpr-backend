package app

import (
	"context"
	"net/http"

	"github.com/rufond/fpr-backend/internal/deps"
	"github.com/rufond/fpr-backend/internal/routes"
)

type App struct {
	deps *deps.Deps
}

func New(d *deps.Deps) *App {
	return &App{deps: d}
}

func (a *App) Start(_ context.Context) error {
	return nil
}

func (a *App) Stop() {
}

func (a *App) HTTPRoutes() []routes.HTTPRoute {
	return nil
}

func (a *App) Routes() []routes.Route {
	return []routes.Route{
		{
			Method:  http.MethodGet,
			Path:    "/healthz",
			Handler: health,
		},
	}
}

func health(_ routes.Request) (int, error, any) {
	return http.StatusOK, nil, map[string]any{
		"status": "ok",
	}
}
