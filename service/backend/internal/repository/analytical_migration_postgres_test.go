package repository

import (
	"github.com/addp/service/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
)

func TestAnalyticalPublicationMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SERVICE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires Service PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS service CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("CREATE SCHEMA service").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.QueryService{}); err != nil {
		t.Fatal(err)
	}
	engine := uint(2)
	legacy := &models.QueryService{TenantID: 7, ServiceName: "legacy_metric", Title: "Legacy", ConfigType: "sql", EngineID: &engine, SqlQuery: "SELECT :subject_id", NamedParameters: []models.QueryServiceNamedParameter{{Name: "subject_id", Type: "string", Required: true}}, Status: "active", DataConfig: models.JSONB{"stable_key": []string{"subject_id"}, models.QueryServiceSourceSnapshotKey: map[string]interface{}{"metric_source": map[string]interface{}{"implementation_id": 3, "revision_id": 7}, "table": map[string]interface{}{"fields": []interface{}{}}}}}
	if err := tx.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := AutoMigrate(tx); err != nil {
			t.Fatal(err)
		}
	}
	var stored models.QueryService
	if err := tx.First(&stored, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ConfigType != "analytical" || stored.Status != "inactive" || stored.EngineID != nil || stored.SqlQuery != "" || len(stored.NamedParameters) != 0 {
		t.Fatalf("old SQL remains executable: %+v", stored)
	}
	snapshot := stored.SourceSnapshot()
	if snapshot == nil || snapshot.MetricSource == nil || snapshot.MetricSource.RevisionID != 7 || snapshot.Table != nil || len(stored.GetStableKey()) != 0 {
		t.Fatal("migration lost owner reference or retained duplicate output")
	}
	if err := tx.Transaction(func(inner *gorm.DB) error { return inner.Model(&stored).Update("sql_query", "SELECT 1").Error }); err == nil {
		t.Fatal("analytical publication accepted SQL override")
	}
}
