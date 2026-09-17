package service

import (
	"fmt"
	"os"
	"testing"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestQueryServiceMetricReferencesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SERVICE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SERVICE_POSTGRES_TEST_DSN is not set")
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
	for _, sql := range []string{"DROP SCHEMA IF EXISTS service CASCADE", "CREATE SCHEMA service"} {
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.AutoMigrate(tx); err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct {
		tenant                   uint
		implementation, revision int
		kind, status             string
	}{
		{1, 1, 6, "analytical", "active"}, {1, 1, 6, "analytical", "inactive"},
		{1, 1, 5, "analytical", "active"}, {1, 2, 6, "analytical", "active"},
		{2, 1, 6, "analytical", "active"}, {1, 1, 6, "sql", "active"},
	} {
		item := &models.QueryService{TenantID: c.tenant, ServiceName: fmt.Sprintf("reference_%d", i), Title: fmt.Sprintf("Reference %d", i), ConfigType: c.kind, Status: c.status,
			DataConfig: models.JSONB{"source_snapshot": map[string]interface{}{"metric_source": map[string]interface{}{"implementation_id": c.implementation, "revision_id": c.revision}}}}
		if c.kind == "sql" {
			engineID := uint(2)
			item.EngineID = &engineID
			item.SqlQuery = "SELECT 1 AS value"
		}
		if err := tx.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewQueryServiceRepository(tx)
	filter := models.QueryServiceListFilter{MetricSource: &models.MetricSourceRequest{ImplementationID: 1, RevisionID: 6}}
	first, total, err := repo.List(1, 0, 1, filter)
	if err != nil || total != 2 || len(first) != 1 || first[0].ServiceName != "reference_1" {
		t.Fatalf("first page: %#v %d %v", first, total, err)
	}
	second, total, err := repo.List(1, 1, 1, filter)
	if err != nil || total != 2 || len(second) != 1 || second[0].ServiceName != "reference_0" {
		t.Fatalf("second page: %#v %d %v", second, total, err)
	}
	filter.Search = "Reference 1"
	found, total, err := repo.List(1, 0, 20, filter)
	if err != nil || total != 1 || len(found) != 1 || found[0].Status != "inactive" {
		t.Fatalf("combined search: %#v %d %v", found, total, err)
	}
	filter.Search = ""
	filter.MetricSource.RevisionID = 99
	_, total, err = repo.List(1, 0, 20, filter)
	if err != nil || total != 0 {
		t.Fatalf("empty: %d %v", total, err)
	}
	_, total, err = repo.List(1, 0, 20, models.QueryServiceListFilter{Search: "Reference"})
	if err != nil || total != 5 {
		t.Fatalf("ordinary search: %d %v", total, err)
	}
}
