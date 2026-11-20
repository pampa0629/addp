package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/system/internal/migration"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Only this deployment subcommand reads the installation credentials.
// Stdout is reserved for the SQL a DBA reviews and executes separately.
func installMySQLIAM(output io.Writer) error {
	cfg := database.Config{
		Host: os.Getenv("ADDP_MYSQL_HOST"), Port: os.Getenv("ADDP_MYSQL_PORT"),
		User: os.Getenv("ADDP_MYSQL_INSTALL_USER"), Password: os.Getenv("ADDP_MYSQL_INSTALL_PASSWORD"),
		DBName: "system", SSLMode: os.Getenv("ADDP_MYSQL_SSLMODE"), ConnectTimeout: 5 * time.Second,
	}
	if os.Getenv("ADDP_INFRA_DATABASE_PROVIDER") != "mysql" || cfg.Host == "" || cfg.Port == "" || cfg.User == "" || cfg.Password == "" {
		return errors.New("MySQL IAM installation requires the mysql provider, explicit host, port and installation credentials")
	}
	dialect, err := platformmysql.Dialector(cfg)
	if err != nil {
		return err
	}
	db, err := gorm.Open(dialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("open MySQL IAM installation connection: %w", err)
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err = migration.MigrateMySQLIAMFoundation(db.WithContext(ctx))
	var pending *migration.PendingMySQLIAMTriggers
	if !errors.As(err, &pending) {
		return err
	}
	if _, err := fmt.Fprintln(output, "USE `system`;\nDELIMITER $$"); err != nil {
		return err
	}
	for _, statement := range pending.Statements {
		if _, err := fmt.Fprintln(output, statement+"$$"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(output, "DELIMITER ;"); err != nil {
		return err
	}
	return pending
}
