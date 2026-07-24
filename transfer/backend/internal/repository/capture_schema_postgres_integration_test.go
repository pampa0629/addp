package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresCaptureProviderSchemaCleanBreak(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	connInfo := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := "capture_split_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := quoteCaptureIdentifier(schema)
	if err := db.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE").Error })
	if err := db.Exec(`CREATE TABLE ` + quotedSchema + `.capture_resources (
		id BIGSERIAL PRIMARY KEY,
		task_id BIGINT NOT NULL,
		tenant_id BIGINT NOT NULL,
		generation BIGINT NOT NULL,
		slot_name VARCHAR(63) NOT NULL,
		publication_name VARCHAR(63) NOT NULL,
		slot_owned BOOLEAN NOT NULL DEFAULT TRUE,
		publication_owned BOOLEAN NOT NULL DEFAULT TRUE,
		CONSTRAINT uq_transfer_capture_slot UNIQUE (slot_name),
		CONSTRAINT uq_transfer_capture_publication UNIQUE (publication_name)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO ` + quotedSchema + `.capture_resources
		(task_id, tenant_id, generation, slot_name, publication_name, slot_owned, publication_owned)
		VALUES (9, 7, 1, 'slot_1', 'publication_1', TRUE, TRUE)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateCaptureProviderResources(db, schema); err != nil {
		t.Fatal(err)
	}

	var sourceType, slotName, publicationName string
	if err := db.Raw(`SELECT c.source_type, p.slot_name, p.publication_name
		FROM `+quotedSchema+`.capture_resources c
		JOIN `+quotedSchema+`.postgresql_capture_resources p ON p.capture_resource_id = c.id`).
		Row().Scan(&sourceType, &slotName, &publicationName); err != nil {
		t.Fatal(err)
	}
	if sourceType != "postgresql" || slotName != "slot_1" || publicationName != "publication_1" {
		t.Fatalf("migrated provider facts = %q/%q/%q", sourceType, slotName, publicationName)
	}
	var legacyColumns int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = ? AND table_name = 'capture_resources'
		AND column_name IN ('slot_name', 'publication_name', 'slot_owned', 'publication_owned')`, schema).
		Scan(&legacyColumns).Error; err != nil {
		t.Fatal(err)
	}
	if legacyColumns != 0 {
		t.Fatalf("legacy capture columns remaining = %d", legacyColumns)
	}
	if err := db.Exec(`INSERT INTO ` + quotedSchema + `.capture_resources (task_id, tenant_id, generation, source_type) VALUES (10, 7, 1, 'mysql')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO ` + quotedSchema + `.mysql_capture_resources (
		capture_resource_id, connector_server_id, schema_history_topic_name, schema_history_topic_owned
	)
		SELECT id, 12345, '__addp_cdc_schema.test', TRUE FROM ` + quotedSchema + `.capture_resources WHERE source_type = 'mysql'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DELETE FROM ` + quotedSchema + `.capture_resources`).Error; err != nil {
		t.Fatal(err)
	}
	var childCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM ` + quotedSchema + `.postgresql_capture_resources`).Scan(&childCount).Error; err != nil {
		t.Fatal(err)
	}
	if childCount != 0 {
		t.Fatalf("PostgreSQL provider facts remaining after generation delete = %d", childCount)
	}
	if err := db.Raw(`SELECT COUNT(*) FROM ` + quotedSchema + `.mysql_capture_resources`).Scan(&childCount).Error; err != nil {
		t.Fatal(err)
	}
	if childCount != 0 {
		t.Fatalf("MySQL provider facts remaining after generation delete = %d", childCount)
	}
}
