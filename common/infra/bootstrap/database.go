// Package bootstrap selects the platform infrastructure for a process at startup.
package bootstrap

import (
	"fmt"

	"github.com/addp/common/config"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/infra/database/postgres"
	"gorm.io/gorm"
)

func OpenDatabase(cfg database.Config, options *gorm.Config) (*gorm.DB, error) {
	switch provider := databaseProvider(); provider {
	case "postgres":
		dialector, err := postgres.Dialector(cfg)
		if err != nil {
			return nil, err
		}
		return gorm.Open(dialector, options)
	default:
		return nil, fmt.Errorf("unsupported platform database provider %q", provider)
	}
}

// PostgreSQLDSN supplies the selected connection settings to native migration tools.
func PostgreSQLDSN(cfg database.Config) (string, error) {
	provider := databaseProvider()
	if provider != "postgres" {
		return "", fmt.Errorf("unsupported platform database provider %q", provider)
	}
	return postgres.DSN(cfg)
}

func databaseProvider() string {
	return config.GetEnv("ADDP_INFRA_DATABASE_PROVIDER", "postgres")
}
