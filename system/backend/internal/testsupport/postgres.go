package testsupport

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// RequireDisposablePostgresDSN blocks destructive integration tests unless
// their configured database name explicitly identifies a disposable test DB.
func RequireDisposablePostgresDSN(t testing.TB, dsn string) {
	t.Helper()
	if err := ValidateDisposablePostgresDSN(dsn); err != nil {
		t.Fatal(err)
	}
}

func ValidateDisposablePostgresDSN(dsn string) error {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse destructive PostgreSQL test DSN: %w", err)
	}

	database := strings.TrimSpace(config.Database)
	if !isDisposableDatabaseName(database) {
		return fmt.Errorf(
			"refusing destructive PostgreSQL test against database %q: database name must contain a separate test or disposable segment",
			database,
		)
	}
	return nil
}

func isDisposableDatabaseName(name string) bool {
	segments := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(name)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, segment := range segments {
		if segment == "test" || segment == "disposable" {
			return true
		}
	}
	return false
}
