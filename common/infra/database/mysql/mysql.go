// Package mysql assembles a MySQL 8 platform database session.
package mysql

import (
	"fmt"
	"net"
	"time"

	"github.com/addp/common/config"
	"github.com/addp/common/infra/database"
	driver "github.com/go-sql-driver/mysql"
	dialect "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Dialector opens the selected physical database. Namespace allocation belongs
// to deployment and the migration owner.
func Dialector(cfg database.Config) (gorm.Dialector, error) {
	if cfg.DBName == "" || cfg.Schema != "" {
		return nil, fmt.Errorf("platform MySQL requires a physical database and no PostgreSQL schema")
	}
	tlsMode := ""
	switch cfg.SSLMode {
	case "", "disable":
	case "verify-full":
		tlsMode = "true"
	default:
		return nil, fmt.Errorf("unsupported platform MySQL TLS mode %q", cfg.SSLMode)
	}
	zone := config.GetTimezone()
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("platform database timezone: %w", err)
	}
	connection := driver.NewConfig()
	connection.User = cfg.User
	connection.Passwd = cfg.Password
	connection.Net = "tcp"
	connection.Addr = net.JoinHostPort(cfg.Host, cfg.Port)
	connection.DBName = cfg.DBName
	connection.TLSConfig = tlsMode
	connection.Timeout = cfg.ConnectTimeout
	connection.ParseTime = true
	connection.Loc = location
	connection.Params = map[string]string{
		"charset":   "utf8mb4",
		"time_zone": "'" + zone + "'",
	}
	return dialect.New(dialect.Config{DSNConfig: connection}), nil
}
