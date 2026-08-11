package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

func TestCaptureRepositoryReusesGenerationAndRejectsStoppedRestart(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	createCaptureRepositoryTestTables(t, db)
	task := createTaskRepositoryTestTask(t, db, 7, "cdc")
	repo := NewCaptureRepository(db)
	identity := CaptureIdentity{
		TaskID: task.ID, TenantID: task.TenantID, SourceType: models.CaptureSourcePostgreSQL, SourceIdentity: "addp://engine/12/path/public/orders?type=table",
		SourceConnectionFingerprint: "fingerprint",
		SourceEngineID:              12, SourceDatabase: "business", SourceSchema: "public", SourceTable: "orders",
	}
	first, err := repo.BeginGeneration(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.BeginGeneration(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Generation != 1 {
		t.Fatalf("generation not reused: first=%+v second=%+v", first, second)
	}
	if first.PostgreSQL == nil || first.MySQL != nil || first.PostgreSQL.SlotName == "" || first.PostgreSQL.PublicationName == "" {
		t.Fatalf("PostgreSQL provider facts = %#v/%#v", first.PostgreSQL, first.MySQL)
	}
	identity.SourceSpatialInfo = models.JSONMap(datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4326, 2)))
	if _, err := repo.BeginGeneration(context.Background(), identity); err == nil || err.Error() != "capture source identity changed after generation creation" {
		t.Fatalf("BeginGeneration() spatial identity error = %v", err)
	}
	if err := repo.ForceUpdate(context.Background(), first.ID, map[string]interface{}{"status": models.CaptureStatusCleanupFailed}); err != nil {
		t.Fatal(err)
	}
	if stopped, err := repo.HasStopInitiatedGeneration(context.Background(), task.ID, task.TenantID); err != nil || !stopped {
		t.Fatalf("HasStopInitiatedGeneration() = %v, %v, want true, nil", stopped, err)
	}
	if _, err := repo.BeginGeneration(context.Background(), identity); !errors.Is(err, ErrCaptureTerminal) {
		t.Fatalf("BeginGeneration() cleanup_failed error = %v, want ErrCaptureTerminal", err)
	}
	if err := repo.ForceUpdate(context.Background(), first.ID, map[string]interface{}{"status": models.CaptureStatusStopped}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginGeneration(context.Background(), identity); !errors.Is(err, ErrCaptureTerminal) {
		t.Fatalf("BeginGeneration() error = %v, want ErrCaptureTerminal", err)
	}

	spatialTask := createTaskRepositoryTestTask(t, db, 7, "cdc")
	spatialIdentity := CaptureIdentity{
		TaskID: spatialTask.ID, TenantID: spatialTask.TenantID, SourceType: models.CaptureSourcePostgreSQL, SourceIdentity: "addp://engine/12/path/public/roads?type=table",
		SourceConnectionFingerprint: "fingerprint",
		SourceEngineID:              12, SourceDatabase: "business", SourceSchema: "public", SourceTable: "roads",
		SourceSpatialInfo: models.JSONMap(datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4549, 2))),
	}
	spatialFirst, err := repo.BeginGeneration(context.Background(), spatialIdentity)
	if err != nil {
		t.Fatal(err)
	}
	spatialSecond, err := repo.BeginGeneration(context.Background(), spatialIdentity)
	if err != nil || spatialSecond.ID != spatialFirst.ID {
		t.Fatalf("spatial generation was not reused after JSON round trip: first=%#v second=%#v err=%v", spatialFirst, spatialSecond, err)
	}
	spatialIdentity.SourceSpatialInfo["srid"] = 4326
	if _, err := repo.BeginGeneration(context.Background(), spatialIdentity); err == nil || err.Error() != "capture source identity changed after generation creation" {
		t.Fatalf("BeginGeneration() changed spatial identity error = %v", err)
	}
}

func createCaptureRepositoryTestTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`
		CREATE TABLE transfer.capture_resources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			generation INTEGER NOT NULL,
			connector_name TEXT NOT NULL UNIQUE,
			topic_name TEXT NOT NULL UNIQUE,
			consumer_group TEXT NOT NULL UNIQUE,
			source_type TEXT NOT NULL,
			source_identity TEXT NOT NULL,
			source_connection_fingerprint TEXT NOT NULL,
			source_engine_id INTEGER NOT NULL,
			source_database TEXT NOT NULL,
			source_schema TEXT NOT NULL,
			source_table TEXT NOT NULL,
			source_spatial_info TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL,
			connector_status TEXT,
			connector_error TEXT,
			topic_created BOOLEAN NOT NULL DEFAULT FALSE,
			connector_created BOOLEAN NOT NULL DEFAULT FALSE,
			resource_version INTEGER NOT NULL DEFAULT 1,
			schema_revision INTEGER NOT NULL DEFAULT 1,
			last_observed_at DATETIME,
			stopped_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE(task_id, generation)
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE transfer.postgresql_capture_resources (
		capture_resource_id INTEGER PRIMARY KEY, slot_name TEXT NOT NULL UNIQUE, publication_name TEXT NOT NULL UNIQUE,
		slot_owned BOOLEAN NOT NULL DEFAULT TRUE, publication_owned BOOLEAN NOT NULL DEFAULT TRUE
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE transfer.mysql_capture_resources (
		capture_resource_id INTEGER PRIMARY KEY, connector_server_id INTEGER NOT NULL UNIQUE,
		schema_history_topic_name TEXT NOT NULL UNIQUE, schema_history_topic_owned BOOLEAN NOT NULL DEFAULT TRUE
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE transfer.oracle_capture_resources (
		capture_resource_id INTEGER PRIMARY KEY,
		schema_history_topic_name TEXT NOT NULL UNIQUE, schema_history_topic_owned BOOLEAN NOT NULL DEFAULT TRUE,
		spatial_mirror_table_name TEXT NOT NULL DEFAULT '', spatial_row_trigger_name TEXT NOT NULL DEFAULT '',
		spatial_ddl_guard_name TEXT NOT NULL DEFAULT '',
		spatial_artifacts_owned BOOLEAN NOT NULL DEFAULT FALSE
	)`).Error; err != nil {
		t.Fatal(err)
	}
}

func TestCaptureRepositoryCreatesMySQLProviderFacts(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	createCaptureRepositoryTestTables(t, db)
	task := createTaskRepositoryTestTask(t, db, 7, "mysql-cdc")
	resource, err := NewCaptureRepository(db).BeginGeneration(context.Background(), CaptureIdentity{
		TaskID: task.ID, TenantID: task.TenantID, SourceType: models.CaptureSourceMySQL,
		SourceIdentity: "addp://engine/13/path/business/orders?type=table", SourceConnectionFingerprint: "fingerprint",
		SourceEngineID: 13, SourceDatabase: "business", SourceSchema: "business", SourceTable: "orders",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resource.MySQL == nil || resource.PostgreSQL != nil || resource.MySQL.ConnectorServerID != uint32(resource.ID) ||
		resource.MySQL.SchemaHistoryTopicName != "__addp_cdc_schema.7.1.1" || !resource.MySQL.SchemaHistoryTopicOwned {
		t.Fatalf("MySQL provider facts = %#v/%#v", resource.MySQL, resource.PostgreSQL)
	}
}

func TestCaptureRepositoryCreatesOracleProviderFacts(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	createCaptureRepositoryTestTables(t, db)
	task := createTaskRepositoryTestTask(t, db, 7, "oracle-cdc")
	resource, err := NewCaptureRepository(db).BeginGeneration(context.Background(), CaptureIdentity{
		TaskID: task.ID, TenantID: task.TenantID, SourceType: models.CaptureSourceOracle,
		SourceIdentity: "addp://engine/22/path/BUSINESS/CUSTOMERS?type=table", SourceConnectionFingerprint: "fingerprint",
		SourceEngineID: 22, SourceDatabase: "FREEPDB1", SourceSchema: "BUSINESS", SourceTable: "CUSTOMERS",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Oracle == nil || resource.PostgreSQL != nil || resource.MySQL != nil ||
		resource.Oracle.SchemaHistoryTopicName != "__addp_cdc_schema.7.1.1" || !resource.Oracle.SchemaHistoryTopicOwned {
		t.Fatalf("Oracle provider facts = %#v", resource.Oracle)
	}
}

func TestCaptureRepositoryCreatesOracleSpatialProviderFacts(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	createCaptureRepositoryTestTables(t, db)
	task := createTaskRepositoryTestTask(t, db, 7, "oracle-spatial-cdc")
	resource, err := NewCaptureRepository(db).BeginGeneration(context.Background(), CaptureIdentity{
		TaskID: task.ID, TenantID: task.TenantID, SourceType: models.CaptureSourceOracle,
		SourceIdentity: "addp://engine/22/path/BUSINESS/CUSTOMER_LOCATIONS?type=table", SourceConnectionFingerprint: "fingerprint",
		SourceEngineID: 22, SourceDatabase: "FREEPDB1", SourceSchema: "BUSINESS", SourceTable: "CUSTOMER_LOCATIONS",
		SourceSpatialInfo: models.JSONMap(datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("SHAPE", "Point", 4326, 2))),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Oracle == nil || !resource.Oracle.SpatialArtifactsOwned || resource.Oracle.SpatialMirrorTableName == "" ||
		resource.Oracle.SpatialRowTriggerName == "" || resource.Oracle.SpatialDDLGuardName == "" ||
		len(resource.Oracle.SpatialMirrorTableName) > 30 || len(resource.Oracle.SpatialRowTriggerName) > 30 || len(resource.Oracle.SpatialDDLGuardName) > 30 {
		t.Fatalf("Oracle Spatial provider facts = %#v", resource.Oracle)
	}
}

func TestCaptureResourceNamesAreStableAndSeparated(t *testing.T) {
	if got := captureTopicName(2, 3, 4); got != "__addp_cdc.2.3.4" {
		t.Fatalf("topic = %q", got)
	}
	if captureSlotName(2, 3, 4) == capturePublicationName(2, 3, 4) {
		t.Fatal("slot and publication names must differ")
	}
}
