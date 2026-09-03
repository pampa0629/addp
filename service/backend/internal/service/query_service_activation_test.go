package service

import (
	"errors"
	"testing"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdateServiceRejectsInvalidActiveRESTContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:query-service-activation?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS service").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE service.query_services (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		service_name TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL,
		description TEXT,
		keywords TEXT,
		config_type TEXT NOT NULL,
		engine_id INTEGER,
		runtime_engine_id INTEGER,
		schema_name TEXT,
		table_name TEXT,
		sql_query TEXT,
		named_parameters JSON NOT NULL DEFAULT '[]',
		data_config JSON NOT NULL,
		protocols JSON NOT NULL,
		public_access BOOLEAN,
		max_features INTEGER,
		status TEXT,
		error_message TEXT,
		created_by INTEGER NOT NULL,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatal(err)
	}

	queryService := models.QueryService{
		TenantID: 1, ServiceName: "invalid-contract", Title: "Invalid contract",
		ConfigType: "sql", SqlQuery: "SELECT 1", DataConfig: models.JSONB{},
		Protocols: models.JSONB{"rest_api": map[string]interface{}{
			"enabled": true, "formats": []interface{}{"json"},
		}},
		Status: "error", CreatedBy: 1,
	}
	if err := db.Create(&queryService).Error; err != nil {
		t.Fatal(err)
	}

	active := "active"
	svc := NewQueryServiceService(repository.NewQueryServiceRepository(db), nil, nil, "")
	if _, err := svc.UpdateService(queryService.ID, &models.UpdateQueryServiceRequest{Status: &active}); !errors.Is(err, ErrInvalidConsumerContract) {
		t.Fatalf("UpdateService() error = %v, want invalid consumer contract", err)
	}
	var stored models.QueryService
	if err := db.First(&stored, queryService.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "error" {
		t.Fatalf("stored status = %q, want error", stored.Status)
	}
}
