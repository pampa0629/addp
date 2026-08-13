package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCheckTaskListUsesTenantAndNormalizesPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newCheckTaskHandlerTestDB(t)
	createCheckTaskHandlerTask(t, db, models.CheckTask{TenantID: 7, Name: "tenant-7", EngineID: 2, SchemaName: "public", Table: "orders", CreatedBy: 1})
	createCheckTaskHandlerTask(t, db, models.CheckTask{TenantID: 8, Name: "tenant-8", EngineID: 2, SchemaName: "public", Table: "orders", CreatedBy: 1})
	handler := NewCheckTaskHandler(service.NewCheckTaskService(repository.NewCheckTaskRepository(db), nil), nil)
	router := gin.New()
	router.GET("/check-tasks", withIssueHandlerAuth(7, 11), handler.List)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/check-tasks?page=0&page_size=999", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var body qualityCheckTaskListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 1 || len(body.Data) != 1 || body.Data[0].TenantID != 7 || body.Data[0].Name != "tenant-7" {
		t.Fatalf("tenant-scoped response = %#v", body)
	}
	if body.Page != 1 || body.PageSize != 100 || body.TotalPages != 1 {
		t.Fatalf("normalized pagination = %#v", body)
	}
}

func TestCheckTaskGetAndRunRejectInvalidIDAndCrossTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newCheckTaskHandlerTestDB(t)
	task := createCheckTaskHandlerTask(t, db, models.CheckTask{TenantID: 7, Name: "tenant-7", EngineID: 2, SchemaName: "public", Table: "orders", CreatedBy: 1})
	handler := NewCheckTaskHandler(service.NewCheckTaskService(repository.NewCheckTaskRepository(db), nil), nil)

	getRouter := gin.New()
	getRouter.GET("/check-tasks/:id", withIssueHandlerAuth(8, 22), handler.Get)
	crossTenantResponse := httptest.NewRecorder()
	getRouter.ServeHTTP(crossTenantResponse, httptest.NewRequest(http.MethodGet, "/check-tasks/"+strconv.FormatInt(task.ID, 10), nil))
	if crossTenantResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant status = %d, want %d, body=%s", crossTenantResponse.Code, http.StatusNotFound, crossTenantResponse.Body.String())
	}

	runRouter := gin.New()
	runRouter.POST("/check-tasks/:id/run", withIssueHandlerAuth(7, 11), handler.Run)
	runResponse := httptest.NewRecorder()
	runRouter.ServeHTTP(runResponse, httptest.NewRequest(http.MethodPost, "/check-tasks/not-an-id/run", nil))
	if runResponse.Code != http.StatusBadRequest {
		t.Fatalf("run invalid id status = %d, want %d, body=%s", runResponse.Code, http.StatusBadRequest, runResponse.Body.String())
	}
}

func TestCheckTaskMutationsRejectUnknownFieldsAndInvalidIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	createRouter := gin.New()
	createRouter.POST("/check-tasks", NewCheckTaskHandler(nil, nil).Create)
	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/check-tasks", strings.NewReader(`{"name":"quality","engine_id":2,"schema_name":"public","table_name":"orders","enabled":true}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRouter.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusBadRequest || !strings.Contains(createResponse.Body.String(), "unknown field") {
		t.Fatalf("create unknown field status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}

	db := newCheckTaskHandlerTestDB(t)
	handler := NewCheckTaskHandler(service.NewCheckTaskService(repository.NewCheckTaskRepository(db), nil), nil)
	updateRouter := gin.New()
	updateRouter.PUT("/check-tasks/:id", withIssueHandlerAuth(7, 11), handler.Update)
	updateResponse := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/check-tasks/not-an-id", strings.NewReader(`{"name":"quality","engine_id":2,"schema_name":"public","table_name":"orders"}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRouter.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusBadRequest {
		t.Fatalf("update invalid id status = %d, want %d, body=%s", updateResponse.Code, http.StatusBadRequest, updateResponse.Body.String())
	}

	deleteRouter := gin.New()
	deleteRouter.DELETE("/check-tasks/:id", withIssueHandlerAuth(7, 11), handler.Delete)
	deleteResponse := httptest.NewRecorder()
	deleteRouter.ServeHTTP(deleteResponse, httptest.NewRequest(http.MethodDelete, "/check-tasks/not-an-id", nil))
	if deleteResponse.Code != http.StatusBadRequest {
		t.Fatalf("delete invalid id status = %d, want %d, body=%s", deleteResponse.Code, http.StatusBadRequest, deleteResponse.Body.String())
	}
}

func TestCheckTaskCreateRejectsMissingRequiredFieldsBeforePersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newCheckTaskHandlerTestDB(t)
	handler := NewCheckTaskHandler(service.NewCheckTaskService(repository.NewCheckTaskRepository(db), nil), nil)
	router := gin.New()
	router.POST("/check-tasks", withIssueHandlerAuth(7, 11), handler.Create)
	request := httptest.NewRequest(http.MethodPost, "/check-tasks", strings.NewReader(`{"name":"  ","engine_id":0,"schema_name":"","table_name":""}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "invalid_request") {
		t.Fatalf("body = %s, want invalid_request", response.Body.String())
	}
}

func newCheckTaskHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.check_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		engine_id INTEGER NOT NULL,
		schema_name TEXT NOT NULL,
		table_name TEXT NOT NULL,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		last_run_at DATETIME,
		last_execution_id TEXT,
		last_execution_status TEXT
	)`).Error; err != nil {
		t.Fatalf("create check tasks table: %v", err)
	}
	return db
}

func createCheckTaskHandlerTask(t *testing.T, db *gorm.DB, task models.CheckTask) models.CheckTask {
	t.Helper()
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create check task: %v", err)
	}
	return task
}
