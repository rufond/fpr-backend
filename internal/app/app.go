package app

import (
	"context"
	"net/http"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/deps"
	"github.com/rufond/fpr-backend/internal/fund"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/routes"
	"github.com/rufond/fpr-backend/internal/scheduler"
	"github.com/rufond/fpr-backend/internal/schedulerjobs"
)

type App struct {
	deps      *deps.Deps
	fund      *fund.Module
	realtime  *realtime.Hub
	scheduler *scheduler.Module
}

func New(d *deps.Deps) *App {
	realtimeHub := realtime.NewHub()
	stateManager := appstate.NewManager()
	fundModule := fund.NewModule(d.DB, d.ManagementCompany, stateManager)
	schedulerModule := scheduler.NewModule(d.DB, realtimeHub)

	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobManagementCompanySync,
		"Management company sync",
		"0 * * * *",
		schedulerjobs.ManagementCompanySync(fundModule.Service, realtimeHub),
	)

	return &App{
		deps:      d,
		fund:      fundModule,
		realtime:  realtimeHub,
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
	a.realtime.Close()
}

func (a *App) HTTPRoutes() []routes.HTTPRoute {
	return []routes.HTTPRoute{
		{
			Method:  http.MethodGet,
			Path:    "/api/v1/realtime",
			Handler: a.realtime,
		},
	}
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
