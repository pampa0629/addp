package repository

import (
	_ "embed"
	"strings"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

//go:embed tables.mysql.sql
var mysqlTables string

//go:embed guards.mysql.sql
var mysqlGuards string

func PrepareDatabase(db *gorm.DB) error {
	if db.Dialector.Name() != "mysql" {
		return Migrate(db)
	}
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	return schema.MigrateMySQL(db, "ontology", SchemaVersion, func(conn *gorm.DB) error {
		for _, stmt := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
			if err := conn.Exec(stmt).Error; err != nil {
				return err
			}
		}
		// Heads and projections refer to each other. Install the three back references after all tables exist.
		for _, fk := range []struct{ table, name, definition string }{
			{"ontologies", "active_projection", `CONSTRAINT active_projection FOREIGN KEY (tenant_id, ontology_id, active_revision, active_generation) REFERENCES projections(tenant_id, ontology_id, revision, generation)`},
			{"platform_capabilities", "platform_active_reference", `CONSTRAINT platform_active_reference FOREIGN KEY (capability, active_revision, active_generation) REFERENCES platform_projections(capability, revision, generation)`},
			{"platform_capabilities", "platform_publish_reference", `CONSTRAINT platform_publish_reference FOREIGN KEY (capability, publish_generation) REFERENCES platform_projections(capability, generation)`},
		} {
			var count int64
			if err := conn.Raw("SELECT COUNT(*) FROM information_schema.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE() AND TABLE_NAME=? AND CONSTRAINT_NAME=?", fk.table, fk.name).Scan(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if err := conn.Exec("ALTER TABLE " + fk.table + " ADD " + fk.definition).Error; err != nil {
					return err
				}
			}
		}
		return schema.CheckMySQLTriggers(conn, strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlGuards), "$$"), "$$\n\n"))
	})
}
