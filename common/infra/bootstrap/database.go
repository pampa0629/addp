// Package bootstrap selects the platform infrastructure for a process at startup.
package bootstrap

import (
	"fmt"
	"os"
	"time"

	"github.com/addp/common/config"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
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

// OpenPlatformRuntime uses one runtime identity for the dedicated platform instance.
func OpenPlatformRuntime(pg database.Config, owner string, options *gorm.Config) (*gorm.DB, error) {
	pg.User, pg.Password = os.Getenv("ADDP_DATABASE_RUNTIME_USER"), os.Getenv("ADDP_DATABASE_RUNTIME_PASSWORD")
	if pg.User == "" || pg.Password == "" {
		return nil, fmt.Errorf("platform database runtime credentials are required")
	}
	return OpenRuntimeDatabase(pg, owner, "ADDP_DATABASE_RUNTIME", options)
}

// OpenRuntimeDatabase is for consumers with a MySQL startup contract. The
// physical database and credential prefix belong to the calling process.
func OpenRuntimeDatabase(pg database.Config, mysqlDatabase, credentialPrefix string, options *gorm.Config) (*gorm.DB, error) {
	if databaseProvider() != "mysql" {
		return OpenDatabase(pg, options)
	}
	cfg := database.Config{
		Host:           os.Getenv("ADDP_MYSQL_HOST"),
		Port:           os.Getenv("ADDP_MYSQL_PORT"),
		DBName:         mysqlDatabase,
		User:           os.Getenv(credentialPrefix + "_USER"),
		Password:       os.Getenv(credentialPrefix + "_PASSWORD"),
		SSLMode:        os.Getenv("ADDP_MYSQL_SSLMODE"),
		ConnectTimeout: 5 * time.Second,
	}
	if cfg.Host == "" || cfg.Port == "" || cfg.User == "" || cfg.Password == "" {
		return nil, fmt.Errorf("MySQL %s startup requires explicit host, port and %s credentials", mysqlDatabase, credentialPrefix)
	}
	driver, err := platformmysql.Dialector(cfg)
	if err != nil {
		return nil, err
	}
	return gorm.Open(driver, options)
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
