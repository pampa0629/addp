package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openLineageCollectorPostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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
	return tx
}

func TestLineageCollectorRetriesUnresolvedTargetAgainstPostgres(t *testing.T) {
	tx := openLineageCollectorPostgresDB(t)
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
	// All 601 rows share created_at, including the unresolved final row. The
	// first 600 observations must not keep the delayed target behind page one.
	earlyTarget := models.MetaItem{TenantID: source.TenantID, EngineID: 2, NodeID: 1, ItemType: "object", Name: "early.tif", FullName: "raster-target/early.tif", Fingerprint: runID + "-early"}
	if err := tx.Create(&earlyTarget).Error; err != nil {
		t.Fatal(err)
	}
	var earlier []commonExecution.TaskExecution
	for i := 0; i < 600; i++ {
		row := execution
		row.ExecutionID = fmt.Sprintf("%s-%03d", runID, i)
		row.Metadata = commonModels.JSONMap{"lineage_facts": commonExecution.LineageFacts{
			SchemaVersion: commonExecution.LineageFactsSchemaVersion,
			Inputs:        []commonExecution.LineageResourceRef{{Port: "input", ItemID: &source.ID}},
			Outputs:       []commonExecution.LineageResourceRef{{Port: "output", ItemID: &earlyTarget.ID, WriteMode: "create"}},
		}}
		earlier = append(earlier, row)
	}
	if err := tx.CreateInBatches(&earlier, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewLineageService(tx, lineageTestEngineCatalog{})
	first, err := svc.CollectExecution(context.Background(), source.TenantID, runID)
	if err != nil || first.Observed != 0 {
		t.Fatalf("unresolved collection = %+v, err = %v", first, err)
	}
	if collected, err := svc.CollectPendingExecutions(context.Background(), 500); err != nil || collected != 600 {
		t.Fatalf("multi-page collection = %d, err = %v, want 600", collected, err)
	}
	if collected, err := svc.CollectPendingExecutions(context.Background(), 500); err != nil || collected != 0 {
		t.Fatalf("unresolved replay = %d, err = %v", collected, err)
	}
	// A later owner scan creates the DataItem; it does not call lineage collect.
	target := models.MetaItem{TenantID: source.TenantID, EngineID: 2, NodeID: 1, ItemType: "object", Name: "result.cog.tif", FullName: "raster-target/result.cog.tif", Fingerprint: runID + "-target"}
	if err := tx.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	collected, err := svc.CollectPendingExecutions(context.Background(), 500)
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
	if collected, err := svc.CollectPendingExecutions(context.Background(), 500); err != nil || collected != 0 {
		t.Fatalf("repeat collection = %d, err = %v", collected, err)
	}
	for _, check := range []struct {
		model interface{}
		count int64
	}{
		{&models.LineageObservation{}, 601}, {&models.LineageItemRelation{}, 2},
	} {
		var count int64
		if err := tx.Model(check.model).Where("tenant_id = ?", source.TenantID).Count(&count).Error; err != nil || count != check.count {
			t.Fatalf("%T count = %d, err = %v, want %d", check.model, count, err, check.count)
		}
	}
}

func TestLineageCollectorContinuesAfterInvalidFactsAgainstPostgres(t *testing.T) {
	tx := openLineageCollectorPostgresDB(t)
	source := createLineageItem(t, tx, 700000007, "source", uuid.NewString())
	target := createLineageItem(t, tx, source.TenantID, "target", uuid.NewString())
	badID, goodID := uuid.NewString(), uuid.NewString()
	insertLineageExecution(t, tx, badID, source.TenantID, source.ID, target.ID, "create")
	if err := tx.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", badID).Updates(map[string]interface{}{
		"created_at": time.Unix(1, 0), "metadata": commonModels.JSONMap{"lineage_facts": map[string]interface{}{"schema_version": "invalid"}},
	}).Error; err != nil {
		t.Fatal(err)
	}
	insertLineageExecution(t, tx, goodID, source.TenantID, source.ID, target.ID, "create")
	svc := NewLineageService(tx, lineageTestEngineCatalog{})
	for i, want := range []int{1, 0} {
		collected, err := svc.CollectPendingExecutions(context.Background(), 1)
		if collected != want || err == nil || !strings.Contains(err.Error(), badID) {
			t.Fatalf("round %d: collected = %d, err = %v, want %d and invalid execution error", i, collected, err, want)
		}
	}
}

func TestLineageCollectorDefersNewExecutionsAgainstPostgres(t *testing.T) {
	tx := openLineageCollectorPostgresDB(t)
	source := createLineageItem(t, tx, 700000007, "source", uuid.NewString())
	target := createLineageItem(t, tx, source.TenantID, "target", uuid.NewString())
	lateID, firstID, nextID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	insertLineageExecution(t, tx, lateID, source.TenantID, source.ID, target.ID, "create")
	if err := tx.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", lateID).Updates(map[string]interface{}{
		"created_at": time.Unix(1, 0), "status": commonExecution.ExecutionStatusRunning,
	}).Error; err != nil {
		t.Fatal(err)
	}
	insertLineageExecution(t, tx, firstID, source.TenantID, source.ID, target.ID, "create")
	// Insert during the first observation, after the collector freezes its ID
	// ceiling. The new row must be picked up by the next round only.
	inserted := false
	const callback = "test:lineage-new-execution"
	if err := tx.Callback().Create().After("gorm:create").Register(callback, func(db *gorm.DB) {
		if _, ok := db.Statement.Dest.(*models.LineageObservation); ok && !inserted {
			inserted = true
			insertLineageExecution(t, tx, nextID, source.TenantID, source.ID, target.ID, "create")
			if err := tx.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", nextID).Update("created_at", time.Unix(2, 0)).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", lateID).Update("status", commonExecution.ExecutionStatusSuccess).Error; err != nil {
				t.Fatal(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Callback().Create().Remove(callback) })
	svc := NewLineageService(tx, lineageTestEngineCatalog{})
	for i, want := range []int{1, 2, 0} {
		if collected, err := svc.CollectPendingExecutions(context.Background(), 1); err != nil || collected != want {
			t.Fatalf("round %d: collected = %d, err = %v, want %d", i, collected, err, want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.CollectPendingExecutions(ctx, 1); err == nil {
		t.Fatal("cancelled collector must return an error")
	}
}

func TestLineageCollectorPreservesClosedFieldRelationsAgainstPostgres(t *testing.T) {
	tx := openLineageCollectorPostgresDB(t)
	schema := fieldTestSchema(t, "x", "y")
	source := fieldTestItem(t, tx, 700000007, "source", schema)
	target := fieldTestItem(t, tx, source.TenantID, "target", schema)
	oldID, newID := uuid.NewString(), uuid.NewString()
	at := time.Now().UTC().Add(-time.Hour)
	fieldTestExecution(t, tx, oldID, source, target, schema, schema, "replace", at,
		commonExecution.LineageFieldMapping{SourceField: "x", TargetField: "x", Transformation: "direct"})
	fieldTestExecution(t, tx, newID, source, target, schema, schema, "replace", at.Add(time.Minute),
		commonExecution.LineageFieldMapping{SourceField: "y", TargetField: "x", Transformation: "derived"})
	svc := NewLineageService(tx, lineageTestEngineCatalog{})
	for _, id := range []string{oldID, newID} {
		if _, err := svc.CollectExecution(context.Background(), source.TenantID, id); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if collected, err := svc.CollectPendingExecutions(context.Background(), 1); err != nil || collected != 0 {
			t.Fatalf("replay = %d, err = %v", collected, err)
		}
	}
	// Repeated immediate notifications share the same idempotent collector;
	// skipping completed executions in the periodic query does not replace it.
	if result, err := svc.CollectExecution(context.Background(), source.TenantID, oldID); err != nil || result.Observed != 0 {
		t.Fatalf("old notification replay = %+v, err = %v", result, err)
	}
	var relations []models.LineageItemRelation
	if err := tx.Where("tenant_id = ? AND granularity = 'field'", source.TenantID).Order("id").Find(&relations).Error; err != nil {
		t.Fatal(err)
	}
	if len(relations) != 2 || relations[0].Status != "closed" || relations[0].SourceFieldName != "x" || relations[1].Status != "active" || relations[1].SourceFieldName != "y" {
		t.Fatalf("replay changed field projections: %+v", relations)
	}
	var count int64
	if err := tx.Model(&models.LineageObservation{}).Where("tenant_id = ?", source.TenantID).Count(&count).Error; err != nil || count != 4 {
		t.Fatalf("observation count = %d, err = %v, want 4", count, err)
	}
}
