package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresReclaimMaterializationStagingIsAtomicAndIdempotent(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schemaName := "model_reclaim_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := db.Exec("CREATE SCHEMA " + quoteIdentifier(schemaName)).Error; err != nil {
		t.Fatalf("create physical test schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA " + quoteIdentifier(schemaName) + " CASCADE").Error })

	fingerprint := strings.Repeat("a", 64)
	batches := []models.MaterializationBatch{
		{ID: uuid.NewString(), LogicalTableID: 1, SchemaFingerprint: fingerprint, StagingName: "first_staging"},
		{ID: uuid.NewString(), LogicalTableID: 2, SchemaFingerprint: fingerprint, StagingName: "second_staging"},
	}
	for index := range batches {
		if err := db.Exec("CREATE TABLE " + qualifiedIdentifier(schemaName, batches[index].StagingName) + " (value BIGINT NOT NULL)").Error; err != nil {
			t.Fatalf("create staging %d: %v", index, err)
		}
		marker := materializationMarker(batches[index].LogicalTableID, fingerprint, batches[index].ID)
		if index == 1 {
			marker = "invalid-owner-marker"
		}
		if err := db.Exec("COMMENT ON TABLE " + qualifiedIdentifier(schemaName, batches[index].StagingName) + " IS " + quoteSQLLiteral(marker)).Error; err != nil {
			t.Fatalf("comment staging %d: %v", index, err)
		}
	}

	err = db.WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		return reclaimMaterializationStaging(tx, schemaName, batches)
	})
	if err == nil {
		t.Fatal("reclaim with invalid second marker succeeded")
	}
	assertMaterializationTableExists(t, db, schemaName, batches[0].StagingName, true)
	assertMaterializationTableExists(t, db, schemaName, batches[1].StagingName, true)

	expectedSecondMarker := materializationMarker(batches[1].LogicalTableID, fingerprint, batches[1].ID)
	if err := db.Exec("COMMENT ON TABLE " + qualifiedIdentifier(schemaName, batches[1].StagingName) + " IS " + quoteSQLLiteral(expectedSecondMarker)).Error; err != nil {
		t.Fatalf("repair second marker: %v", err)
	}
	if err := db.WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		return reclaimMaterializationStaging(tx, schemaName, batches)
	}); err != nil {
		t.Fatalf("reclaim staging: %v", err)
	}
	assertMaterializationTableExists(t, db, schemaName, batches[0].StagingName, false)
	assertMaterializationTableExists(t, db, schemaName, batches[1].StagingName, false)

	if err := db.WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		return reclaimMaterializationStaging(tx, schemaName, batches)
	}); err != nil {
		t.Fatalf("idempotent reclaim staging: %v", err)
	}
}
