package service

import (
	"context"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestLineageCollectorRetriesUnresolvedTargetAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	// The owner gate rolls back Meta fixtures and the canonical execution model.
	if err := tx.Exec("DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta; CREATE SCHEMA IF NOT EXISTS common").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&commonExecution.TaskExecution{}, &models.MetaItem{}, &models.LineageItemRelation{}, &models.LineageObservation{}); err != nil {
		t.Fatal(err)
	}
	runID := uuid.NewString()
	source := models.MetaItem{TenantID: 700000007, EngineID: 1, NodeID: 1, ItemType: "object", Name: "source.tif", FullName: "raster-source/source.tif", Fingerprint: runID + "-source"}
	if err := tx.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	targetLocator := "addp://engine/2/path/raster-target/result.cog.tif?type=object"
	execution := commonExecution.TaskExecution{TenantID: int(source.TenantID), ExecutionID: runID, Module: "develop", TaskType: "workflow", Source: "develop", Status: "success", TriggerType: "manual", CreatedAt: time.Unix(1, 0), Metadata: commonModels.JSONMap{
		"lineage_facts": commonExecution.LineageFacts{SchemaVersion: commonExecution.LineageFactsSchemaVersion,
			Inputs:     []commonExecution.LineageResourceRef{{Port: "input", ItemID: &source.ID}},
			Outputs:    []commonExecution.LineageResourceRef{{Port: "output", Locator: targetLocator, WriteMode: "create"}},
			Operations: []commonExecution.LineageOperation{{Kind: "derive", InputPorts: []string{"input"}, OutputPorts: []string{"output"}}}},
	}}
	if err := tx.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewLineageService(tx, lineageTestEngineCatalog{})
	first, err := svc.CollectExecution(context.Background(), source.TenantID, runID)
	if err != nil || first.Observed != 0 {
		t.Fatalf("unresolved collection = %+v, err = %v", first, err)
	}
	// A later owner scan creates the DataItem; it does not call lineage collect.
	target := models.MetaItem{TenantID: source.TenantID, EngineID: 2, NodeID: 1, ItemType: "object", Name: "result.cog.tif", FullName: "raster-target/result.cog.tif", Fingerprint: runID + "-target"}
	if err := tx.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	collected, err := svc.CollectPendingExecutions(context.Background(), 1)
	if err != nil || collected != 1 {
		t.Fatalf("pending collection = %d, err = %v, want one resolved relation", collected, err)
	}
	var relation models.LineageItemRelation
	if err := tx.Where("tenant_id = ? AND source_item_id = ? AND target_item_id = ?", source.TenantID, source.ID, target.ID).First(&relation).Error; err != nil {
		t.Fatal(err)
	}
	if relation.Status != "active" || relation.RelationKind != "derive" {
		t.Fatalf("relation = %+v", relation)
	}
	if collected, err := svc.CollectPendingExecutions(context.Background(), 1); err != nil || collected != 0 {
		t.Fatalf("repeat collection = %d, err = %v", collected, err)
	}
}
