package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresCatalogMetricChangeFeedCapturesOwnerLifecycle(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	metric := models.MetricDefinition{
		TenantID: tenantID, Code: fmt.Sprintf("catalog_metric_%d", tenantID), ScopeType: "tenant_common",
		Tags: models.StringArray{}, CreatedBy: 1, Version: 1, LifecycleState: "active",
	}
	revision := models.MetricDefinitionRevision{MetricType: models.MetricTypeAtomic, Name: "Catalog metric", Definition: "Catalog metric", StatisticalCaliber: "All records", ChangeSummary: "Initial", CreatedBy: 1}
	if err := NewMetricRepository(db).Create(&metric, &revision, nil); err != nil {
		t.Fatalf("create metric: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM standard.metric_definitions WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM standard.catalog_resource_changes WHERE tenant_id = ?", tenantID).Error
	})
	repository := NewCatalogResourceRepository(db)
	changes, err := repository.ListChanges(context.Background(), tenantID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 || changes[len(changes)-1].SourceType != models.CatalogSourceTypeMetric || changes[len(changes)-1].Operation != "upsert" || changes[len(changes)-1].Snapshot["metric_type"] != "atomic" || changes[len(changes)-1].Snapshot["scope_type"] != "tenant_common" {
		t.Fatalf("initial changes = %#v", changes)
	}
	lastID := changes[len(changes)-1].ID
	replayTx := db.Begin()
	if replayTx.Error != nil {
		t.Fatal(replayTx.Error)
	}
	defer replayTx.Rollback()
	if err := replayTx.Exec("DELETE FROM standard.data_migrations WHERE version = ?", 2026092701).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(replayTx); err != nil {
		t.Fatalf("replay existing Metric snapshot: %v", err)
	}
	replayed, err := NewCatalogResourceRepository(replayTx).ListChanges(context.Background(), tenantID, lastID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 || replayed[0].Snapshot["scope_type"] != "tenant_common" || replayed[0].ID <= lastID {
		t.Fatalf("replayed metric changes = %#v", replayed)
	}
	if err := Migrate(replayTx); err != nil {
		t.Fatalf("repeat Metric snapshot migration: %v", err)
	}
	var replayCount int64
	if err := replayTx.Table("standard.catalog_resource_changes").Where("tenant_id = ? AND id > ?", tenantID, lastID).Count(&replayCount).Error; err != nil {
		t.Fatal(err)
	}
	if replayCount != 1 {
		t.Fatalf("replay count after repeat migration = %d", replayCount)
	}
	if err := replayTx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	effectiveFrom := time.Now().UTC()
	if err := db.Model(&revision).Updates(map[string]any{"name": "Catalog metric current", "status": models.RevisionStatusPublished, "effective_from": effectiveFrom}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&metric).Updates(map[string]any{"version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&metric).Error; err != nil {
		t.Fatal(err)
	}
	changes, err = repository.ListChanges(context.Background(), tenantID, lastID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Operation != "upsert" || changes[0].Snapshot["name"] != "Catalog metric current" || changes[1].Operation != "missing" {
		t.Fatalf("lifecycle changes = %#v", changes)
	}
}
