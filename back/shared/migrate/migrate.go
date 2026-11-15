package migrate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func Up(databaseURL, sourceDir string) error {
	src := "file://" + sourceDir
	// The pgx driver accepts several hosts with target_session_attrs, so
	// migrations always run on the current PostgreSQL leader.
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(databaseURL, scheme) {
			databaseURL = "pgx5://" + strings.TrimPrefix(databaseURL, scheme)
			break
		}
	}
	m, err := migrate.New(src, databaseURL)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
