package schema

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const mysqlMigrationLock = "SHA2(CONCAT('addp.schema/', DATABASE(), '/', ?), 256)"

// MigrateMySQL coordinates a retryable owner migration in the selected physical
// database. Apply must be able to resume after any committed DDL statement.
func MigrateMySQL(db *gorm.DB, owner string, version int64, apply func(*gorm.DB) error) error {
	return MigrateMySQLWithData(db, owner, version, apply, nil)
}

// MigrateMySQLWithData commits the data migration and completion revision
// together after retryable DDL. The callback receives the last completed version.
func MigrateMySQLWithData(db *gorm.DB, owner string, version int64, apply func(*gorm.DB) error, data func(*gorm.DB, int64) error) error {
	if db == nil || db.Dialector.Name() != "mysql" || !validMySQLSchemaOwner(owner) || version < 1 || apply == nil {
		return fmt.Errorf("invalid MySQL schema migration contract: %q version %d", owner, version)
	}
	return db.Connection(func(connection *gorm.DB) (migrationErr error) {
		conn := connection.Session(&gorm.Session{NewDB: true})
		var acquired int
		if err := conn.Raw("SELECT GET_LOCK("+mysqlMigrationLock+", 30)", owner).Scan(&acquired).Error; err != nil {
			return fmt.Errorf("acquire MySQL schema migration lock for %s: %w", owner, err)
		}
		if acquired != 1 {
			return fmt.Errorf("timed out acquiring MySQL schema migration lock for %s", owner)
		}
		defer func() {
			// The startup context can expire during a migration. Release using the
			// same connection, independently of the canceled startup context.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var released int
			err := conn.WithContext(ctx).Raw("SELECT RELEASE_LOCK("+mysqlMigrationLock+")", owner).Scan(&released).Error
			if err == nil && released == 1 {
				return
			}
			// Do not return a connection that might still hold the lock to the pool.
			_ = connection.Statement.ConnPool.(*sql.Conn).Raw(func(any) error { return driver.ErrBadConn })
			if err == nil {
				err = errors.New("migration lock was not held")
			}
			migrationErr = errors.Join(migrationErr, fmt.Errorf("release MySQL schema migration lock for %s: %w", owner, err))
		}()
		if err := conn.Exec(`CREATE TABLE IF NOT EXISTS startup_schema_revision (
			owner VARCHAR(64) NOT NULL PRIMARY KEY,
			version BIGINT NOT NULL,
			pending_version BIGINT NOT NULL
		) ENGINE=InnoDB`).Error; err != nil {
			return err
		}
		current, err := readMySQLSchemaRevision(conn, owner)
		if err != nil {
			return err
		}
		if current.PendingVersion != 0 && current.PendingVersion != version {
			return fmt.Errorf("schema %s has unfinished MySQL migration to version %d; process requires %d", owner, current.PendingVersion, version)
		}
		if current.Version > version {
			return fmt.Errorf("schema %s version %d is newer than this process requires (%d)", owner, current.Version, version)
		}
		if current.Version == version && current.PendingVersion == 0 {
			return nil
		}
		if err := conn.Exec(`INSERT INTO startup_schema_revision (owner, version, pending_version) VALUES (?, 0, ?)
			ON DUPLICATE KEY UPDATE pending_version = ?`, owner, version, version).Error; err != nil {
			return err
		}
		if err := apply(conn.Session(&gorm.Session{NewDB: true})); err != nil {
			return fmt.Errorf("migrate MySQL schema %s to %d: %w", owner, version, err)
		}
		return conn.Transaction(func(tx *gorm.DB) error {
			if data != nil {
				if err := data(tx, current.Version); err != nil {
					return fmt.Errorf("migrate MySQL data for %s to %d: %w", owner, version, err)
				}
			}
			return tx.Exec("UPDATE startup_schema_revision SET version = ?, pending_version = 0 WHERE owner = ?", version, owner).Error
		})
	})
}

// RequireMySQL verifies the owner revision without writing to the selected database.
func RequireMySQL(db *gorm.DB, owner string, version int64) error {
	if db == nil || db.Dialector.Name() != "mysql" || !validMySQLSchemaOwner(owner) || version < 1 {
		return fmt.Errorf("invalid MySQL schema requirement: %q", owner)
	}
	revision, err := readMySQLSchemaRevision(db, owner)
	if err != nil {
		return fmt.Errorf("schema %s is not initialized; start its Backend first: %w", owner, err)
	}
	if revision.PendingVersion != 0 {
		return fmt.Errorf("schema %s has unfinished MySQL migration to version %d", owner, revision.PendingVersion)
	}
	if revision.Version != version {
		return fmt.Errorf("schema %s version mismatch: database=%d process=%d; complete Backend migration before starting Worker", owner, revision.Version, version)
	}
	return nil
}

type mysqlSchemaRevision struct {
	Version        int64
	PendingVersion int64
}

func readMySQLSchemaRevision(db *gorm.DB, owner string) (mysqlSchemaRevision, error) {
	var revision mysqlSchemaRevision
	err := db.Raw("SELECT version, pending_version FROM startup_schema_revision WHERE owner = ?", owner).Scan(&revision).Error
	return revision, err
}

func validMySQLSchemaOwner(owner string) bool {
	return len(owner) <= 64 && namePattern.MatchString(owner)
}
