package repository

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/schema"
	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

const SchemaVersion = 1

//go:embed tables.mysql.sql
var mysqlTables string

func PrepareDatabase(db *gorm.DB, validate func(*models.QueryService) error) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		if _, err := projectionstore.Migrate(db, "service", "service", nil); err != nil {
			return err
		}
		return schema.MigrateMySQL(db, "service", SchemaVersion, prepareMySQL)
	}
	return schema.Migrate(db, "service", SchemaVersion, func(tx *gorm.DB) error {
		if err := AutoMigrate(tx); err != nil {
			return err
		}
		if _, err := projectionstore.Migrate(tx, "service", "service", nil); err != nil {
			return err
		}
		_, err := NewQueryServiceRepository(tx).MigrateInvalidQueryServiceConsumerContracts(validate)
		return err
	})
}

func prepareMySQL(db *gorm.DB) error {
	if err := cleanup.PrepareMySQLResultTable(db, "service"); err != nil {
		return err
	}
	for _, stmt := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	triggers := []string{cleanup.MySQLResultUpdateGuard}
	for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
		row, op := "NEW", "upsert"
		if event == "DELETE" {
			row, op = "OLD", "missing"
		}
		triggers = append(triggers, fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS catalog_query_service_%s
AFTER %s ON query_services FOR EACH ROW
INSERT INTO catalog_resource_changes(tenant_id,source_type,source_identity,operation,snapshot,observed_at)
VALUES (%[3]s.tenant_id,'query_service',%[3]s.id,'%[4]s',JSON_MERGE_PATCH(JSON_OBJECT(),JSON_OBJECT(
'name',%[3]s.title,'code',%[3]s.service_name,'object_kind','query_service','service_status',%[3]s.status,
'config_type',%[3]s.config_type,'access_mode',IF(%[3]s.public_access,'public','private'),
'engine_id',CAST(%[3]s.engine_id AS CHAR),'runtime_engine_id',CAST(%[3]s.runtime_engine_id AS CHAR))),CURRENT_TIMESTAMP(6))`, strings.ToLower(event), event, row, op))
	}
	return schema.CheckMySQLTriggers(db, triggers)
}
