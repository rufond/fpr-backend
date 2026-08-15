package deps

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/rufond/fpr-backend/internal/config"
	"github.com/rufond/fpr-backend/internal/providers/managementcompany"
	"github.com/rufond/fpr-backend/internal/providers/moex"
)

type Deps struct {
	Config *config.Config
	DB     *pgxpool.Pool
	Logger zerolog.Logger

	ManagementCompany *managementcompany.Provider
	MOEX              *moex.Provider
}

func New(ctx context.Context, cfg *config.Config) (*Deps, error) {
	logger := newLogger(cfg)

	db, err := newDB(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return &Deps{
		Config:            cfg,
		DB:                db,
		Logger:            logger,
		ManagementCompany: managementcompany.NewProvider(cfg.ManagementCompany.FundURL, nil),
		MOEX:              moex.NewProvider("", nil),
	}, nil
}

func (d *Deps) Close() {
	if d == nil {
		return
	}
	if d.DB != nil {
		d.DB.Close()
	}
}

func newLogger(cfg *config.Config) zerolog.Logger {
	writer := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339Nano,
	}

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	logger := log.Output(writer)
	if cfg.Debug {
		logger = logger.With().Caller().Logger()
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Logger = logger

	return logger
}

func newDB(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.DB.URL)
	if err != nil {
		return nil, fmt.Errorf("pg connection: %w", err)
	}

	if errPool := pool.Ping(ctx); errPool != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", errPool)
	}

	return pool, nil
}
