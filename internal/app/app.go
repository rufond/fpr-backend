package app

import (
	"context"
	"net/http"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/deps"
	"github.com/rufond/fpr-backend/internal/fund"
	"github.com/rufond/fpr-backend/internal/routes"
	"github.com/rufond/fpr-backend/internal/scheduler"
	"github.com/rufond/fpr-backend/internal/schedulerjobs"
)

type App struct {
	deps      *deps.Deps
	fund      *fund.Module
	scheduler *scheduler.Module
}

func New(d *deps.Deps) *App {
	stateManager := appstate.NewManager()
	fundModule := fund.NewModule(d.DB, d.ManagementCompany, stateManager)
	schedulerModule := scheduler.NewModule(d.DB, nil)

	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobManagementCompanySync,
		"Management company sync",
		"0 * * * *",
		schedulerjobs.ManagementCompanySync(fundModule.Service),
	)

	return &App{
		deps:      d,
		fund:      fundModule,
		scheduler: schedulerModule,
	}
}

func (a *App) Start(ctx context.Context) error {
	if err := a.fund.Start(ctx); err != nil {
		return err
	}

	a.scheduler.Manager.Start(ctx)

	return nil
}

func (a *App) Stop() {
	a.scheduler.Manager.Stop()
}

func (a *App) HTTPRoutes() []routes.HTTPRoute {
	return nil
}

func (a *App) Routes() []routes.Route {
	return []routes.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/fund/state",
			Handler: a.fund.Handler.State,
		},
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/fund/history",
			Handler: a.fund.Handler.History,
		},
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
