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
	switch provider := config.GetEnv("ADDP_INFRA_DATABASE_PROVIDER", "postgres"); provider {
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
