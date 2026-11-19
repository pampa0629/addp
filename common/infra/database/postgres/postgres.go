// Package postgres binds platform connection settings to the GORM PostgreSQL driver.
package postgres

import (
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/config"
	"github.com/addp/common/infra/database"
	driver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Dialector builds a pgx keyword DSN. GORM also reads the unquoted IANA
// timezone to configure its timestamp codec on every connection in the pool.
func Dialector(cfg database.Config) (gorm.Dialector, error) {
	dsn, err := DSN(cfg)
	if err != nil {
		return nil, err
	}
	return driver.Open(dsn), nil
}

// DSN builds connection settings for GORM and native PostgreSQL migration tools.
func DSN(cfg database.Config) (string, error) {
	zone := config.GetTimezone()
	if _, err := time.LoadLocation(zone); err != nil {
		return "", fmt.Errorf("platform database timezone: %w", err)
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
	}
	dsn := fmt.Sprintf("timezone=%s host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		zone, quote(cfg.Host), quote(cfg.Port), quote(cfg.User), quote(cfg.Password), quote(cfg.DBName), quote(cfg.SSLMode))
	if cfg.Schema != "" {
		dsn += " search_path=" + quote(cfg.Schema)
	}
	if cfg.ConnectTimeout > 0 {
		dsn += fmt.Sprintf(" connect_timeout=%d", int64(cfg.ConnectTimeout/time.Second))
	}
	return dsn, nil
}

func quote(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(value) + "'"
}
