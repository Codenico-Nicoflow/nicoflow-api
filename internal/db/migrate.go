package db

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/migrations"
)

// Migrate applies pending up-migrations from the embedded SQL files. Idempotent,
// safe on every boot; never runs down. Errors on a dirty DB rather than guessing.
func Migrate(dsn string) error {
	sourceDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("db.Migrate: open embedded source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, dsn)
	if err != nil {
		return fmt.Errorf("db.Migrate: init migrator: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			log.Warn().Err(srcErr).Msg("migrate: error closing source")
		}
		if dbErr != nil {
			log.Warn().Err(dbErr).Msg("migrate: error closing db")
		}
	}()

	before, dirty, vErr := m.Version()
	if vErr != nil && !errors.Is(vErr, migrate.ErrNilVersion) {
		return fmt.Errorf("db.Migrate: read version: %w", vErr)
	}
	if dirty {
		return fmt.Errorf("db.Migrate: database is dirty at version %d — resolve manually with `migrate force`", before)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db.Migrate: apply up: %w", err)
	}

	after, _, vErr := m.Version()
	if vErr != nil {
		return fmt.Errorf("db.Migrate: read final version: %w", vErr)
	}

	if before == after {
		log.Info().Uint("version", after).Msg("migrations up to date")
	} else {
		log.Info().Uint("from", before).Uint("to", after).Msg("migrations applied")
	}
	return nil
}
