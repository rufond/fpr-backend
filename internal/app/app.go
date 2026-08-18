package app

import (
	"context"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/auth"
	"github.com/rufond/fpr-backend/internal/deps"
	"github.com/rufond/fpr-backend/internal/fund"
	"github.com/rufond/fpr-backend/internal/fx"
	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/routes"
	"github.com/rufond/fpr-backend/internal/scheduler"
	"github.com/rufond/fpr-backend/internal/schedulerjobs"
	"github.com/rufond/fpr-backend/internal/valuation"
)

type App struct {
	UserResolver routes.UserResolver

	auth      *auth.Module
	fund      *fund.Module
	fx        *fx.Module
	prices    *prices.Module
	realtime  *realtime.Hub
	scheduler *scheduler.Module
	valuation *valuation.Module
}

func New(d *deps.Deps) *App {
	realtimeHub := realtime.NewHub()
	stateManager := appstate.NewManager()
	authModule := auth.NewModule(d.Config.Admin.Login, d.Config.Admin.PasswordHash)
	fundModule := fund.NewModule(d.DB, d.ManagementCompany, stateManager)
	priceModule := prices.NewModule(d.DB, d.MOEX, d.Yahoo, stateManager, realtimeHub)
	fxModule := fx.NewModule(d.DB, d.MOEX, stateManager)
	valuationModule := valuation.NewModule(d.DB, stateManager, priceModule.Service, fxModule.Service)
	priceModule.Handler.SetLiveValuationRefresher(valuationModule.Service)
	schedulerModule := scheduler.NewModule(d.DB, realtimeHub)

	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobManagementCompanySync,
		"Management company sync",
		"0 * * * *",
		schedulerjobs.ManagementCompanySync(fundModule.Service, valuationModule.Service, realtimeHub),
	)
	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobMOEXFundUnitSync,
		"MOEX fund unit sync",
		"* * * * *",
		schedulerjobs.MOEXFundUnitSync(priceModule.Service, valuationModule.Service, realtimeHub),
	)
	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobMOEXUSDRUBSync,
		"MOEX USD/RUB sync",
		"* * * * *",
		schedulerjobs.MOEXUSDRUBSync(fxModule.Service, valuationModule.Service, realtimeHub),
	)
	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobMOEXSourcesDiscovery,
		"MOEX source discovery",
		"4 * * * *",
		schedulerjobs.MOEXSourcesDiscovery(priceModule.Service),
	)
	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobYahooSourcesDiscovery,
		"Yahoo source discovery",
		"5 * * * *",
		schedulerjobs.YahooSourcesDiscovery(priceModule.Service),
	)
	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobMOEXSecurityPricesSync,
		"MOEX security prices sync",
		"* * * * *",
		schedulerjobs.MOEXSecurityPricesSync(priceModule.Service, valuationModule.Service, realtimeHub),
	)
	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobYahooPricesSync,
		"Yahoo prices sync",
		"* * * * *",
		schedulerjobs.YahooPricesSync(priceModule.Service, valuationModule.Service, realtimeHub),
	)
	schedulerModule.Manager.MustAdd(
		schedulerjobs.JobMOEXFundUnitHistorySync,
		"MOEX fund unit history sync",
		"10 * * * *",
		schedulerjobs.MOEXFundUnitHistorySync(priceModule.Service, realtimeHub),
	)

	return &App{
		UserResolver: authModule.Service.ResolveUser,
		auth:         authModule,
		fund:         fundModule,
		fx:           fxModule,
		prices:       priceModule,
		realtime:     realtimeHub,
		scheduler:    schedulerModule,
		valuation:    valuationModule,
	}
}

func (a *App) Start(ctx context.Context) error {
	if err := a.fund.Start(ctx); err != nil {
		return err
	}
	if err := a.prices.Service.Start(ctx); err != nil {
		return err
	}
	if err := a.fx.Service.Start(ctx); err != nil {
		return err
	}
	if err := a.valuation.Service.Start(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Error().Err(err).Msg("start live valuation")
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
			Method:  http.MethodPost,
			Path:    "/api/v1/auth/login",
			Handler: a.auth.Handler.Login,
		},
		{
			Method:       http.MethodPost,
			Path:         "/api/v1/auth/logout",
			AuthRequired: true,
			Handler:      a.auth.Handler.Logout,
		},
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
			Method:  http.MethodPost,
			Path:    "/api/v1/fund/market-history",
			Handler: a.fund.Handler.MarketHistory,
		},
		{
			Method:       http.MethodGet,
			Path:         "/api/v1/admin/price-sources",
			AuthRequired: true,
			Handler:      a.prices.Handler.Sources,
		},
		{
			Method:       http.MethodPost,
			Path:         "/api/v1/admin/price-sources/set",
			AuthRequired: true,
			Handler:      a.prices.Handler.SetSource,
		},
		{
			Method:       http.MethodGet,
			Path:         "/api/v1/admin/scheduler/jobs",
			AuthRequired: true,
			Handler:      a.scheduler.Handler.Jobs,
		},
		{
			Method:       http.MethodPost,
			Path:         "/api/v1/admin/scheduler/run",
			AuthRequired: true,
			Handler:      a.scheduler.Handler.RunJob,
		},
		{
			Method: http.MethodGet,
			Path:   "/healthz",
			Handler: func(_ routes.Request) (int, error, any) {
				return http.StatusOK, nil, map[string]any{"status": "ok"}
			},
		},
	}
}
