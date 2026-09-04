package service

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresMaterializationGroupPhysicalPublishIsAtomicAndIdempotent(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schemaName := "model_group_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := db.Exec("CREATE SCHEMA " + quoteIdentifier(schemaName)).Error; err != nil {
		t.Fatalf("create physical test schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA " + quoteIdentifier(schemaName) + " CASCADE").Error })

	fingerprint := strings.Repeat("a", 64)
	candidates := make([]materializationPublishCandidate, 0, 2)
	for index, targetName := range []string{"person_metric", "person_pair_metric"} {
		logicalTableID := int64(index + 1)
		batchID := uuid.NewString()
		stagingName := targetName + "__staging"
		expectedMarker := materializationMarker(logicalTableID, fingerprint, batchID)
		oldMarker := materializationMarker(logicalTableID, strings.Repeat("b", 64), uuid.NewString())
		for _, statement := range []string{
			"CREATE TABLE " + qualifiedIdentifier(schemaName, targetName) + " (value BIGINT NOT NULL)",
			"INSERT INTO " + qualifiedIdentifier(schemaName, targetName) + " (value) VALUES (" + strconv.Itoa(index+1) + ")",
			"COMMENT ON TABLE " + qualifiedIdentifier(schemaName, targetName) + " IS " + quoteSQLLiteral(oldMarker),
			"CREATE TABLE " + qualifiedIdentifier(schemaName, stagingName) + " (value BIGINT NOT NULL)",
			"INSERT INTO " + qualifiedIdentifier(schemaName, stagingName) + " (value) VALUES (" + strconv.Itoa(index+3) + ")",
		} {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatalf("prepare physical table %d: %v", index, err)
			}
		}
		stagingMarker := expectedMarker
		if index == 1 {
			stagingMarker = "invalid-owner-marker"
		}
		if err := db.Exec("COMMENT ON TABLE " + qualifiedIdentifier(schemaName, stagingName) + " IS " + quoteSQLLiteral(stagingMarker)).Error; err != nil {
			t.Fatalf("comment staging table %d: %v", index, err)
		}
		candidates = append(candidates, materializationPublishCandidate{
			batch: models.MaterializationBatch{
				ID: batchID, LogicalTableID: logicalTableID, SchemaFingerprint: fingerprint,
				TargetName: targetName, StagingName: stagingName, ExpectedTargetMarker: &oldMarker,
			},
			schemaName: schemaName, expectedMarker: expectedMarker,
			backupName: materializationTemporaryName(targetName, "backup", batchID),
		})
	}

	if err := publishMaterializationCandidates(context.Background(), db, candidates); err == nil {
		t.Fatal("publish with an invalid second staging marker succeeded")
	}
	assertMaterializationTableValue(t, db, schemaName, "person_metric", 1)
	assertMaterializationTableValue(t, db, schemaName, "person_pair_metric", 2)
	assertMaterializationTableExists(t, db, schemaName, "person_metric__staging", true)
	assertMaterializationTableExists(t, db, schemaName, "person_pair_metric__staging", true)

	second := candidates[1]
	if err := db.Exec("COMMENT ON TABLE " + qualifiedIdentifier(schemaName, second.batch.StagingName) + " IS " + quoteSQLLiteral(second.expectedMarker)).Error; err != nil {
		t.Fatalf("repair second staging marker: %v", err)
	}
	if err := publishMaterializationCandidates(context.Background(), db, candidates); err != nil {
		t.Fatalf("publish materialization group: %v", err)
	}
	assertMaterializationTableValue(t, db, schemaName, "person_metric", 3)
	assertMaterializationTableValue(t, db, schemaName, "person_pair_metric", 4)
	assertMaterializationTableExists(t, db, schemaName, "person_metric__staging", false)
	assertMaterializationTableExists(t, db, schemaName, "person_pair_metric__staging", false)

	if err := publishMaterializationCandidates(context.Background(), db, candidates); err != nil {
		t.Fatalf("idempotent materialization group publish: %v", err)
	}
}

func TestPostgresMaterializationPublishRejectsTargetChangedAfterPrepare(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schemaName := "model_cas_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := db.Exec("CREATE SCHEMA " + quoteIdentifier(schemaName)).Error; err != nil {
		t.Fatalf("create physical test schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA " + quoteIdentifier(schemaName) + " CASCADE").Error })

	batchID := uuid.NewString()
	oldMarker := materializationMarker(7, strings.Repeat("a", 64), uuid.NewString())
	changedMarker := materializationMarker(7, strings.Repeat("b", 64), uuid.NewString())
	newMarker := materializationMarker(7, strings.Repeat("c", 64), batchID)
	for _, statement := range []string{
		"CREATE TABLE " + qualifiedIdentifier(schemaName, "metric") + " (value BIGINT NOT NULL)",
		"COMMENT ON TABLE " + qualifiedIdentifier(schemaName, "metric") + " IS " + quoteSQLLiteral(changedMarker),
		"CREATE TABLE " + qualifiedIdentifier(schemaName, "metric__staging") + " (value BIGINT NOT NULL)",
		"COMMENT ON TABLE " + qualifiedIdentifier(schemaName, "metric__staging") + " IS " + quoteSQLLiteral(newMarker),
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare physical table: %v", err)
		}
	}
	candidates := []materializationPublishCandidate{{
		batch: models.MaterializationBatch{
			ID: batchID, LogicalTableID: 7, SchemaFingerprint: strings.Repeat("c", 64),
			TargetName: "metric", StagingName: "metric__staging", ExpectedTargetMarker: &oldMarker,
		},
		schemaName: schemaName, expectedMarker: newMarker,
		backupName: materializationTemporaryName("metric", "backup", batchID),
	}}
	if err := publishMaterializationCandidates(context.Background(), db, candidates); err == nil {
		t.Fatal("publish accepted a target changed after prepare")
	}
	assertMaterializationTableExists(t, db, schemaName, "metric", true)
	assertMaterializationTableExists(t, db, schemaName, "metric__staging", true)
}

func assertMaterializationTableValue(t *testing.T, db *gorm.DB, schemaName, tableName string, expected int64) {
	t.Helper()
	var value int64
	if err := db.Raw("SELECT value FROM " + qualifiedIdentifier(schemaName, tableName)).Scan(&value).Error; err != nil {
		t.Fatalf("read %s: %v", tableName, err)
	}
	if value != expected {
		t.Fatalf("%s value = %d, want %d", tableName, value, expected)
	}
}

func assertMaterializationTableExists(t *testing.T, db *gorm.DB, schemaName, tableName string, expected bool) {
	t.Helper()
	var relation sql.NullString
	if err := db.Raw("SELECT to_regclass(?)::text", schemaName+"."+tableName).Scan(&relation).Error; err != nil {
		t.Fatalf("inspect %s: %v", tableName, err)
	}
	if relation.Valid != expected {
		t.Fatalf("%s existence = %v, want %v", tableName, relation.Valid, expected)
	}
}
