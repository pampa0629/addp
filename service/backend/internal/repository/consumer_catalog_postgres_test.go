package repository

import (
	"errors"
	"os"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
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

	checkConsumerCatalog(t, tx)
}

func TestConsumerCatalogAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_SERVICE_REPOSITORY") != "1" {
		t.Skip("requires a gate-owned Service database")
	}
	driver, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), DBName: "service",
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Exec(`CREATE TABLE service.query_services (
		id BIGINT PRIMARY KEY AUTO_INCREMENT, tenant_id BIGINT, service_name VARCHAR(255), title VARCHAR(255),
		description TEXT, config_type VARCHAR(50), engine_id BIGINT, sql_query TEXT, status VARCHAR(50),
		data_config JSON, protocols JSON, created_by BIGINT, created_at DATETIME, updated_at DATETIME
	)`).Error; err != nil {
		t.Fatal(err)
	}
	checkConsumerCatalog(t, db)

	if err := db.Exec(`INSERT INTO service.query_services (tenant_id, service_name, title, config_type, data_config) VALUES
		(7,'sales','Sales','analytical','{"source_snapshot":{"metric_source":{"implementation_id":9007199254740993,"revision_id":2}}}'),
		(7,'stale','Sales','analytical','{"source_snapshot":{"metric_source":{"implementation_id":9007199254740993,"revision_id":1}}}'),
		(8,'foreign','Sales','analytical','{"source_snapshot":{"metric_source":{"implementation_id":9007199254740993,"revision_id":2}}}')`).Error; err != nil {
		t.Fatal(err)
	}
	rows, total, err := NewQueryServiceRepository(db).List(7, 0, 10, models.QueryServiceListFilter{
		Search: "sAlEs", MetricSource: &models.MetricSourceRequest{ImplementationID: 9007199254740993, RevisionID: 2},
	})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ServiceName != "sales" {
		t.Fatalf("metric source list=%v total=%d err=%v", rows, total, err)
	}
}

func checkConsumerCatalog(t *testing.T, tx *gorm.DB) {
	t.Helper()
	fixtures := []models.QueryService{
		consumerCatalogService("alpha", "Alpha", 7, "active", true, false),
		consumerCatalogService("beta", "Beta spatial", 7, "active", true, true),
		consumerCatalogService("inactive", "Inactive", 7, "inactive", true, false),
		consumerCatalogService("disabled", "REST disabled", 7, "active", false, false),
		consumerCatalogService("other-tenant", "Other tenant", 8, "active", true, false),
	}
	wrongType := consumerCatalogService("string", "String flag", 7, "active", true, false)
	wrongType.Protocols = models.JSONB{"rest_api": map[string]any{"enabled": "true"}}
	fixtures = append(fixtures, wrongType)
	fixtures[0].DataConfig = models.JSONB{"source_snapshot": map[string]any{"spatial": map[string]any{"primary_geometry_column": nil}}}
	for index := range fixtures {
		if err := tx.Select("TenantID", "ServiceName", "Title", "ConfigType", "EngineID", "SqlQuery", "Status", "CreatedBy",
			"DataConfig", "Protocols", "CreatedAt", "UpdatedAt").Create(&fixtures[index]).Error; err != nil {
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
	tabular, total, err := repo.ListConsumerServices(models.ConsumerServiceListFilter{TenantID: 7, OutputKind: models.ConsumerOutputKindTabular, Limit: 20})
	if err != nil || total != 1 || len(tabular) != 1 || tabular[0].ServiceName != "alpha" {
		t.Fatalf("tabular list=%v total=%d error=%v", tabular, total, err)
	}
	searched, total, err := repo.ListConsumerServices(models.ConsumerServiceListFilter{
		TenantID: 7, Search: "sPaTiAl", Offset: 0, Limit: 20,
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

func consumerCatalogService(
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
	engine := uint(2)
	return models.QueryService{
		TenantID: tenantID, ServiceName: serviceName, Title: title, ConfigType: "sql", EngineID: &engine,
		SqlQuery: "SELECT 1", Status: status, CreatedBy: 1, MaxFeatures: 1000,
		DataConfig: models.JSONB{
			models.QueryServiceSourceSnapshotKey: map[string]interface{}{"spatial": spatialPayload},
		},
		Protocols: models.JSONB{
			"rest_api": map[string]interface{}{"enabled": restEnabled, "formats": []string{"json", "csv"}},
		},
	}
}
