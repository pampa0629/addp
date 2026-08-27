package repository

import (
	"errors"
	"os"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/service/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestConsumerCatalogAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SERVICE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SERVICE_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS service CASCADE").Error; err != nil {
		t.Fatalf("drop Service schema: %v", err)
	}
	if err := tx.Exec("CREATE SCHEMA service").Error; err != nil {
		t.Fatalf("create Service schema: %v", err)
	}
	if err := AutoMigrate(tx); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	fixtures := []models.QueryService{
		consumerCatalogPostgresService("alpha", "Alpha", 7, "active", true, false),
		consumerCatalogPostgresService("beta", "Beta spatial", 7, "active", true, true),
		consumerCatalogPostgresService("inactive", "Inactive", 7, "inactive", true, false),
		consumerCatalogPostgresService("disabled", "REST disabled", 7, "active", false, false),
		consumerCatalogPostgresService("other-tenant", "Other tenant", 8, "active", true, false),
	}
	for index := range fixtures {
		if err := tx.Create(&fixtures[index]).Error; err != nil {
			t.Fatalf("create fixture %s: %v", fixtures[index].ServiceName, err)
		}
	}
	repo := NewQueryServiceRepository(tx)

	firstPage, total, err := repo.ListConsumerServices(models.ConsumerServiceListFilter{
		TenantID: 7, Offset: 0, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(firstPage) != 1 || firstPage[0].ServiceName != "alpha" {
		t.Fatalf("first page = %#v total=%d", firstPage, total)
	}
	spatial, total, err := repo.ListConsumerServices(models.ConsumerServiceListFilter{
		TenantID: 7, OutputKind: models.ConsumerOutputKindSpatialTabular, Offset: 0, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(spatial) != 1 || spatial[0].ServiceName != "beta" {
		t.Fatalf("spatial list = %#v total=%d", spatial, total)
	}
	searched, total, err := repo.ListConsumerServices(models.ConsumerServiceListFilter{
		TenantID: 7, Search: "spatial", Offset: 0, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(searched) != 1 || searched[0].ServiceName != "beta" {
		t.Fatalf("searched list = %#v total=%d", searched, total)
	}
	if _, err := repo.GetConsumerServiceByID(7, fixtures[2].ID); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("inactive detail error = %v, want not found", err)
	}
	if detail, err := repo.GetConsumerServiceByID(7, fixtures[1].ID); err != nil || detail.ServiceName != "beta" {
		t.Fatalf("consumer detail = %#v error=%v", detail, err)
	}
}

func consumerCatalogPostgresService(
	serviceName string,
	title string,
	tenantID uint,
	status string,
	restEnabled bool,
	spatial bool,
) models.QueryService {
	spatialPayload := map[string]interface{}{}
	if spatial {
		spatialPayload["primary_geometry_column"] = "location"
	}
	return models.QueryService{
		TenantID: tenantID, ServiceName: serviceName, Title: title, ConfigType: "sql",
		SqlQuery: "SELECT 1", Status: status, CreatedBy: 1, MaxFeatures: 1000,
		DataConfig: models.JSONB{
			models.QueryServiceSourceSnapshotKey: map[string]interface{}{"spatial": spatialPayload},
		},
		Protocols: models.JSONB{
			"rest_api": map[string]interface{}{"enabled": restEnabled, "formats": []string{"json", "csv"}},
		},
	}
}
