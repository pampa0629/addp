package main

import (
	"context"
	"errors"
	"io"
	"time"

	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/schema"
	"github.com/addp/system/internal/migration"
	"gorm.io/gorm"
)

// Installation uses a deployment connection scoped to the selected database.
// Stdout is reserved for the SQL a DBA reviews and executes separately.
func installMySQL(command string, output io.Writer) error {
	target := "system"
	if command == "install-mysql-common" {
		target = "common"
	}
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
	if command == "verify-mysql-source-grant" {
		return migration.VerifyMySQLSourceGrantProcedure(db.WithContext(ctx))
	}
	var statements []string
	for _, install := range []func(*gorm.DB) error{
		migration.MigrateMySQLIAMFoundation,
		migration.MigrateMySQLAccessRequestLocks,
		migration.MigrateMySQLAccessTargetLocks,
		migration.MigrateMySQLModuleRegistry,
		migration.MigrateMySQLEngineCatalog,
		migration.MigrateMySQLSourceGrantCatalog,
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
		return nil
	}
	pending := &schema.PendingMySQLTriggers{Statements: statements}
	if err := pending.WriteSQL(output, target); err != nil {
		return err
	}
	return pending
}
