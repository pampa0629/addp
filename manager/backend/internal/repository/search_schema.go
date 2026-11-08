package repository

import (
	"crypto/sha256"
	"fmt"

	"github.com/addp/manager/migrations"
	"gorm.io/gorm"
)

// MigrateSearchSchema applies the owner transition once, atomically with its
// checksum. It never stores per-query candidates or clears an existing index.
func MigrateSearchSchema(db *gorm.DB) error {
	const name = "047_search_continuation.sql"
	sql, err := migrations.SearchFS.ReadFile(name)
	if err != nil {
		return err
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(sql))
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('addp.manager.search_schema',0))").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE TABLE IF NOT EXISTS manager.search_schema_migrations(name text PRIMARY KEY,checksum text NOT NULL)").Error; err != nil {
			return err
		}
		var applied string
		if err := tx.Raw("SELECT checksum FROM manager.search_schema_migrations WHERE name=?", name).Scan(&applied).Error; err != nil {
			return err
		}
		if applied != "" {
			if applied != checksum {
				return fmt.Errorf("applied Manager search migration checksum differs: %s", name)
			}
			return nil
		}
		if err := tx.Exec(string(sql)).Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO manager.search_schema_migrations(name,checksum) VALUES(?,?)", name, checksum).Error
	})
}
