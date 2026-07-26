package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdateInternalUpdatesTenantEngineThroughService(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate engine: %v", err)
	}

	tenantID := uint(1)
	repo := repository.NewEngineRepository(db)
	engine := &models.Engine{
		Name:       "Business Spark",
		EngineType: "spark",
		TenantID:   &tenantID,
		IsActive:   true,
		ConnectionInfo: models.ConnectionInfo{
			"host":        "host.docker.internal",
			"port":        11000,
			"master_port": 7077,
		},
	}
	if err := repo.Create(engine); err != nil {
		t.Fatalf("create engine: %v", err)
	}

	router := gin.New()
	handler := NewEngineHandler(service.NewEngineService(repo, nil, nil))
	router.PUT("/api/v1/internal/engines/:id", handler.UpdateInternal)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/internal/engines/1",
		bytes.NewBufferString(`{"tenant_id":1,"connection_info":{"host":"localhost"}}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", response.Code, response.Body.String())
	}

	updated, err := repo.GetByID(engine.ID)
	if err != nil {
		t.Fatalf("get updated engine: %v", err)
	}
	if updated.ConnectionInfo["host"] != "localhost" {
		t.Fatalf("updated host = %#v, want localhost", updated.ConnectionInfo["host"])
	}
	if updated.ConnectionInfo["master_port"] != float64(7077) && updated.ConnectionInfo["master_port"] != 7077 {
		t.Fatalf("master_port was not preserved: %#v", updated.ConnectionInfo["master_port"])
	}
}
