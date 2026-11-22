package repository

import (
	"embed"
	"fmt"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

//go:embed mysql_scan_catalog/*.sql
var mysqlScanCatalogDDL embed.FS

// MigrateMySQLScanCatalog installs scan facts, independently of the complete Meta revision.
func MigrateMySQLScanCatalog(db *gorm.DB) error {
	var target string
	if err := db.Raw("SELECT DATABASE()").Scan(&target).Error; err != nil {
		return err
	}
	if target != "meta" {
		return fmt.Errorf("Meta scan catalog installation requires the meta database")
	}
	verified := false
	err := schema.MigrateMySQL(db, "meta_scan_catalog", 2, func(conn *gorm.DB) error {
		entries, err := mysqlScanCatalogDDL.ReadDir("mysql_scan_catalog")
		if err != nil {
			return err
		}
		for _, entry := range entries {
			ddl, err := mysqlScanCatalogDDL.ReadFile("mysql_scan_catalog/" + entry.Name())
			if err != nil {
				return err
			}
			if err := conn.Exec(string(ddl)).Error; err != nil {
				return fmt.Errorf("install %s: %w", entry.Name(), err)
			}
		}
		// Replace each v1 key atomically so an interrupted upgrade can resume at the next table.
		for _, table := range []string{"meta_node", "meta_item"} {
			oldKey := "fk_" + table + "_tenant_engine"
			var installed int
			if err := conn.Raw(`SELECT COUNT(*) FROM information_schema.referential_constraints
				WHERE constraint_schema='meta' AND table_name=? AND constraint_name=?`, table, oldKey).Scan(&installed).Error; err != nil {
				return err
			}
			if installed == 1 {
				ddl := fmt.Sprintf("ALTER TABLE meta.%s DROP FOREIGN KEY %s, ADD CONSTRAINT fk_%s_engine_restrict FOREIGN KEY (tenant_id,engine_id) REFERENCES `system`.engines (tenant_id,id) ON DELETE RESTRICT", table, oldKey, table)
				if err := conn.Exec(ddl).Error; err != nil {
					return fmt.Errorf("upgrade %s engine reference: %w", table, err)
				}
			}
		}
		if err := verifyScanEngineForeignKeys(conn); err != nil {
			return err
		}
		verified = true
		return nil
	})
	if err != nil || verified {
		return err
	}
	// A completed revision skips DDL on replay. Verify the installed keys before the DBA revokes REFERENCES.
	return verifyScanEngineForeignKeys(db)
}

func verifyScanEngineForeignKeys(db *gorm.DB) error {
	var keys int
	err := db.Raw(`SELECT COUNT(*) FROM (
        SELECT k.table_name FROM information_schema.key_column_usage k
        JOIN information_schema.referential_constraints r
          ON r.constraint_schema=k.constraint_schema AND r.constraint_name=k.constraint_name AND r.table_name=k.table_name
        WHERE k.constraint_schema='meta'
          AND (k.table_name,k.constraint_name) IN (('meta_node','fk_meta_node_engine_restrict'),('meta_item','fk_meta_item_engine_restrict'))
          AND k.referenced_table_schema='system' AND k.referenced_table_name='engines'
          AND r.delete_rule='RESTRICT' AND r.update_rule IN ('RESTRICT','NO ACTION')
        GROUP BY k.table_name
        HAVING COUNT(*)=2
           AND SUM(k.ordinal_position=1 AND k.column_name='tenant_id' AND k.referenced_column_name='tenant_id')=1
           AND SUM(k.ordinal_position=2 AND k.column_name='engine_id' AND k.referenced_column_name='id')=1
    ) verified_keys`).Scan(&keys).Error
	if err != nil {
		return err
	}
	if keys != 2 {
		return fmt.Errorf("Meta scan installation requires both tenant/engine foreign keys to system.engines")
	}
	return nil
}
