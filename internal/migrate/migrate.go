package migrate

import (
	"errors"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/rs/zerolog/log"
)

func Run(url string) error {
	log.Info().Msg("start migration")

	m, errNew := migrate.New("file://database/migrations", url)
	if errNew != nil {
		return fmt.Errorf("error init migration: %w", errNew)
	}

	version, dirty, errGetVersion := m.Version()
	if errGetVersion != nil && !errors.Is(errGetVersion, migrate.ErrNilVersion) {
		return fmt.Errorf("error get version: %w", errGetVersion)
	}
	if dirty {
		log.Warn().Msgf("previous version %v migration is dirty", version)
	}

	if os.Getenv("DB_FRESH") == "true" {
		errDown := m.Down()
		if errDown != nil && !errors.Is(errDown, migrate.ErrNoChange) {
			return fmt.Errorf("error down migrate: %w", errDown)
		}
		if errDown != nil {
			log.Info().Err(errDown).Msg("down")
		}
	}

	errUp := m.Up()
	if errUp != nil && !errors.Is(errUp, migrate.ErrNoChange) {
		errForce := m.Force(int(version))
		if errForce != nil {
			return fmt.Errorf("error force: %w", errForce)
		}

		return fmt.Errorf("error up migrate: %w", errUp)
	}
	if errUp != nil {
		log.Info().Err(errUp).Msg("up")
	}

	errCloseSource, errCloseDB := m.Close()

	if errCloseSource != nil {
		return fmt.Errorf("error close source: %w", errCloseSource)
	}
	if errCloseDB != nil {
		return fmt.Errorf("error close db: %w", errCloseDB)
	}

	log.Info().Msg("finish migration")

	return nil
}
