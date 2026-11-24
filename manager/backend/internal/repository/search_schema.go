package repository

import (
	"crypto/sha256"
	"fmt"

	"github.com/addp/manager/migrations"
	"gorm.io/gorm"
)

// MigrateSearchSchema applies the search migrations in version order and records
// each checksum in the same transaction as its schema changes.
func MigrateSearchSchema(db *gorm.DB) error {
	if db.Dialector.Name() == "mysql" {
		if err := db.Exec(`CREATE TABLE IF NOT EXISTS manager.search_history_owners (
            tenant_id bigint unsigned NOT NULL, user_id bigint unsigned NOT NULL,
            PRIMARY KEY(tenant_id,user_id), CHECK(tenant_id>0 AND user_id>0)
        ) ENGINE=InnoDB`).Error; err != nil {
			return err
		}
		return db.Exec(`CREATE TABLE IF NOT EXISTS manager.search_histories (
            id bigint unsigned AUTO_INCREMENT PRIMARY KEY, tenant_id bigint unsigned NOT NULL,
            user_id bigint unsigned NOT NULL, query varchar(512) NOT NULL,
            created_at datetime(6), updated_at datetime(6),
            UNIQUE KEY idx_search_history_tenant_user_query (tenant_id,user_id,query),
            CHECK(tenant_id>0 AND user_id>0)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`).Error
	}
	return migrateSearchSchema(db, "047_search_continuation.sql", "049_scope_search_history_to_tenant.sql",
		"050_keyword_search.sql", "051_vector_search_revision.sql")
}

func migrateSearchSchema(db *gorm.DB, names ...string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('addp.manager.search_schema',0))").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE TABLE IF NOT EXISTS manager.search_schema_migrations(name text PRIMARY KEY,checksum text NOT NULL)").Error; err != nil {
			return err
		}
		for _, name := range names {
			sql, err := migrations.SearchFS.ReadFile(name)
			if err != nil {
				return err
			}
			checksum := fmt.Sprintf("%x", sha256.Sum256(sql))
			var applied string
			if err := tx.Raw("SELECT checksum FROM manager.search_schema_migrations WHERE name=?", name).Scan(&applied).Error; err != nil {
				return err
			}
			if applied == checksum {
				continue
			}
			if applied != "" {
				return fmt.Errorf("applied Manager search migration checksum differs: %s", name)
			}
			// A published 047 is immutable. Fresh installations prepare keyword
			// and vector materials independently with 050 and 051.
			if name == "047_search_continuation.sql" {
				continue
			}
			if name == "051_vector_search_revision.sql" {
				var installed bool
				if err := tx.Raw("SELECT to_regclass('manager.embeddings') IS NOT NULL").Scan(&installed).Error; err != nil {
					return err
				}
				if !installed {
					continue
				}
			}
			if err := tx.Exec(string(sql)).Error; err != nil {
				return fmt.Errorf("apply Manager search migration %s: %w", name, err)
			}
			if err := tx.Exec("INSERT INTO manager.search_schema_migrations(name,checksum) VALUES(?,?)", name, checksum).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
