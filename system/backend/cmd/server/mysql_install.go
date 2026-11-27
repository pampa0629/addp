package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/schema"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/migration"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Installation uses a deployment connection scoped to the selected database.
// Stdout is reserved for the SQL a DBA reviews and executes separately.
func prepareDatabase(target string, output io.Writer, requireCompleteHistory bool) error {
	if target != "common" && target != "system" {
		return fmt.Errorf("expected database owner common or system")
	}
	provider := commonConfig.GetEnv("ADDP_INFRA_DATABASE_PROVIDER", "postgres")
	if provider == "mysql" {
		if requireCompleteHistory {
			return errors.New("--require-complete-history applies to PostgreSQL System migration checksums")
		}
		return installMySQL(target, output)
	}
	if provider != "postgres" {
		return fmt.Errorf("unsupported platform database provider %q", provider)
	}
	cfg := config.LoadPlatformDatabase()
	const prefix = "ADDP_DATABASE_DEPLOY"
	cfg.User, cfg.Password, cfg.Schema = os.Getenv(prefix+"_USER"), os.Getenv(prefix+"_PASSWORD"), target
	if cfg.User == "" || cfg.Password == "" {
		return fmt.Errorf("missing %s credentials", prefix)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if target == "system" {
		dsn, err := bootstrap.PostgreSQLDSN(cfg)
		if err != nil {
			return err
		}
		runner := migration.NewRunner(dsn)
		runner.RequireCompleteHistory = requireCompleteHistory
		return runner.Run(ctx)
	}
	db, err := bootstrap.OpenDatabase(cfg, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	return schema.InitializeCommon(db.WithContext(ctx))
}

func installMySQL(target string, output io.Writer) error {
	db, err := platformmysql.OpenInstallation(target)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if target == "common" {
		return schema.InitializeCommon(db.WithContext(ctx))
	}
	var statements []string
	for _, install := range []func(*gorm.DB) error{
		migration.MigrateMySQLIAMFoundation,
		migration.MigrateMySQLAccessRequestLocks,
		migration.MigrateMySQLAccessTargetLocks,
		migration.MigrateMySQLModuleRegistry,
		migration.MigrateMySQLEngineCatalog,
		migration.MigrateMySQLExecutionAuthorization,
		migration.MigrateMySQLSourceGrantCatalog,
		migration.MigrateMySQLSearchAuthorization,
		migration.MigrateMySQLEngineDelegations,
		migration.MigrateMySQLCleanupRequests,
	} {
		err := install(db.WithContext(ctx))
		if err == nil {
			continue
		}
		var pending *schema.PendingMySQLTriggers
		if !errors.As(err, &pending) {
			return err
		}
		statements = append(statements, pending.Statements...)
	}
	if len(statements) == 0 {
		runtime, err := bootstrap.OpenPlatformRuntime(database.Config{}, "information_schema", &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			return err
		}
		pool, err := runtime.DB()
		if err != nil {
			return err
		}
		defer pool.Close()
		return migration.PrepareMySQLSourceGrant(db.WithContext(ctx), runtime.WithContext(ctx))
	}
	pending := &schema.PendingMySQLTriggers{Statements: statements}
	if err := pending.WriteSQL(output, target); err != nil {
		return err
	}
	return pending
}
