package migration

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const SchemaVersion = 1

//go:embed tables.mysql.sql
var mysqlTables string

func Prepare(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		return schema.MigrateMySQL(db, "model", SchemaVersion, prepareMySQL)
	}
	return schema.Migrate(db, "model", SchemaVersion, Run)
}

func prepareMySQL(db *gorm.DB) error {
	if err := cleanup.PrepareMySQLResultTable(db, "model"); err != nil {
		return err
	}
	for _, statement := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	triggers := []string{cleanup.MySQLResultUpdateGuard}
	for _, group := range []struct{ parent, child, key, kind string }{
		{"entities", "entity_attributes", "entity_id", "entity"},
		{"logical_tables", "logical_fields", "table_id", "logical_table"},
	} {
		// The owner's approval and the child's frozen Standard revision form one invariant.
		for _, event := range []string{"INSERT", "UPDATE"} {
			parentGuard := fmt.Sprintf("NEW.status='approved' AND EXISTS (SELECT 1 FROM %s WHERE %s=NEW.id AND element_id IS NOT NULL AND element_revision_id IS NULL)", group.child, group.key)
			childGuard := fmt.Sprintf("NEW.element_id IS NOT NULL AND NEW.element_revision_id IS NULL AND EXISTS (SELECT 1 FROM %s WHERE id=NEW.%s AND status='approved')", group.parent, group.key)
			for _, guard := range []struct{ table, condition string }{{group.parent, parentGuard}, {group.child, childGuard}} {
				triggers = append(triggers, fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS freeze_%s_%s\nBEFORE %s ON %s FOR EACH ROW\nBEGIN IF %s THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='approved model must freeze its data element revision'; END IF; END", guard.table, strings.ToLower(event), event, guard.table, guard.condition))
			}
		}
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			record, operation := "NEW", "upsert"
			if event == "DELETE" {
				record, operation = "OLD", "missing"
			}
			fields := fmt.Sprintf("'name',%[1]s.name,'code',%[1]s.code,'object_kind','%[2]s','model_status',%[1]s.status,'domain_id',CAST(%[1]s.domain_id AS CHAR)", record, group.kind)
			if group.kind == "logical_table" {
				fields += fmt.Sprintf(",'table_type',%[1]s.table_type,'layer',%[1]s.layer", record)
			}
			triggers = append(triggers, fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS catalog_%s_%s\nAFTER %s ON %s FOR EACH ROW\nINSERT INTO catalog_resource_changes (tenant_id,source_type,source_identity,operation,resource_version,snapshot,observed_at) VALUES (%s.tenant_id,'%s',%s.id,'%s',%s.version,JSON_MERGE_PATCH(JSON_OBJECT(),JSON_OBJECT(%s)),CURRENT_TIMESTAMP(6))", group.parent, strings.ToLower(event), event, group.parent, record, group.kind, record, operation, record, fields))
		}
	}
	return schema.CheckMySQLTriggers(db, triggers)
}
