package mysql

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/addp/common/infra/database"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenInstallation opens the physical database selected by the module's offline installer.
func OpenInstallation(target string) (*gorm.DB, error) {
	cfg := database.Config{
		Host: os.Getenv("ADDP_MYSQL_HOST"), Port: os.Getenv("ADDP_MYSQL_PORT"),
		User: os.Getenv("ADDP_MYSQL_INSTALL_USER"), Password: os.Getenv("ADDP_MYSQL_INSTALL_PASSWORD"),
		DBName: target, SSLMode: os.Getenv("ADDP_MYSQL_SSLMODE"), ConnectTimeout: 5 * time.Second,
	}
	if os.Getenv("ADDP_INFRA_DATABASE_PROVIDER") != "mysql" || cfg.Host == "" || cfg.Port == "" || cfg.User == "" || cfg.Password == "" {
		return nil, errors.New("MySQL installation requires the mysql provider, explicit host, port and installation credentials")
	}
	dialect, err := Dialector(cfg)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("open MySQL %s installation connection: %w", target, err)
	}
	return db, nil
}
