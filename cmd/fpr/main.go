package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/rufond/fpr-backend/internal/app"
	"github.com/rufond/fpr-backend/internal/config"
	"github.com/rufond/fpr-backend/internal/deps"
	"github.com/rufond/fpr-backend/internal/migrate"
	"github.com/rufond/fpr-backend/internal/routes"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Panic().Err(err).Msg("load config")
	}

	d, err := deps.New(ctx, cfg)
	if err != nil {
		log.Panic().Err(err).Msg("init dependencies")
	}
	defer d.Close()

	if errMigrate := migrate.Run(cfg.DB.URL); errMigrate != nil {
		log.Panic().Err(errMigrate).Msg("run migration")
	}

	application := app.New(d)

	if errStart := application.Start(ctx); errStart != nil {
		log.Panic().Err(errStart).Msg("start application")
	}
	defer application.Stop()

	if errRun := routes.Run(ctx, cfg.HTTP.Addr, application.Routes(), application.HTTPRoutes()); errRun != nil {
		log.Panic().Err(errRun).Msg("run server")
	}
}
