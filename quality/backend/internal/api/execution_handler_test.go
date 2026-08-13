package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQualityExecutionFilterIsScopedToCheckExecutions(t *testing.T) {
	filter := qualityExecutionFilter(7, 2, 50)
	if filter.TenantID != 7 || filter.Module != commonExecution.ModuleQuality || filter.TaskType != commonExecution.TaskTypeQualityCheck || filter.Page != 2 || filter.PageSize != 50 {
		t.Fatalf("quality execution filter = %#v", filter)
	}
}

func TestQualityExecutionStatusAcceptsOnlyContractValues(t *testing.T) {
	for _, value := range []string{"", "pending", "running", "success", "failed", "timeout", "cancelled"} {
		got, err := qualityExecutionStatus(value)
		if err != nil || got != value {
			t.Fatalf("qualityExecutionStatus(%q) = %q, %v", value, got, err)
		}
	}
	if _, err := qualityExecutionStatus("queued"); err == nil {
		t.Fatal("qualityExecutionStatus accepted unsupported status")
	}
}

func TestExecutionListRejectsUnsupportedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/executions", NewExecutionHandler(nil).List)
	req := httptest.NewRequest(http.MethodGet, "/executions?status=queued", strings.NewReader(""))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestExecutionListUsesQualityAndTenantFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newExecutionHandlerTestDB(t)
	insertExecutionHandlerRow(t, db, 1, 7, "quality-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, commonExecution.ExecutionStatusSuccess)
	insertExecutionHandlerRow(t, db, 2, 8, "quality-8", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, commonExecution.ExecutionStatusSuccess)
	insertExecutionHandlerRow(t, db, 3, 7, "other-7", commonExecution.ModuleSystem, "cleanup", commonExecution.ExecutionStatusSuccess)
	handler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))
	router := gin.New()
	router.GET("/executions", withIssueHandlerAuth(7, 11), handler.List)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/executions?page=0&page_size=999&status=success", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var body qualityExecutionListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 1 || len(body.Data) != 1 || body.Data[0].ExecutionID != "quality-7" {
		t.Fatalf("filtered executions = %#v", body)
	}
	if body.Page != 1 || body.PageSize != 100 || body.TotalPages != 1 {
		t.Fatalf("normalized execution pagination = %#v", body)
	}
}

func TestExecutionGetRejectsCrossTenantAndNonQualityExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newExecutionHandlerTestDB(t)
	insertExecutionHandlerRow(t, db, 1, 7, "quality-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, commonExecution.ExecutionStatusSuccess)
	insertExecutionHandlerRow(t, db, 2, 7, "system-7", commonExecution.ModuleSystem, "cleanup", commonExecution.ExecutionStatusSuccess)
	handler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))

	crossTenantRouter := gin.New()
	crossTenantRouter.GET("/executions/:execution_id", withIssueHandlerAuth(8, 22), handler.Get)
	crossTenantResponse := httptest.NewRecorder()
	crossTenantRouter.ServeHTTP(crossTenantResponse, httptest.NewRequest(http.MethodGet, "/executions/quality-7", nil))
	if crossTenantResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant status = %d, want %d, body=%s", crossTenantResponse.Code, http.StatusNotFound, crossTenantResponse.Body.String())
	}

	nonQualityRouter := gin.New()
	nonQualityRouter.GET("/executions/:execution_id", withIssueHandlerAuth(7, 11), handler.Get)
	nonQualityResponse := httptest.NewRecorder()
	nonQualityRouter.ServeHTTP(nonQualityResponse, httptest.NewRequest(http.MethodGet, "/executions/system-7", nil))
	if nonQualityResponse.Code != http.StatusNotFound {
		t.Fatalf("non-quality status = %d, want %d, body=%s", nonQualityResponse.Code, http.StatusNotFound, nonQualityResponse.Body.String())
	}
}

func newExecutionHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
		id INTEGER PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		execution_id TEXT NOT NULL,
		module TEXT NOT NULL,
		task_type TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		source_task_id TEXT,
		source_task_name TEXT,
		parent_execution_id TEXT,
		status TEXT NOT NULL,
		progress INTEGER,
		current_step TEXT,
		trigger_type TEXT NOT NULL,
		triggered_by INTEGER,
		actor_principal_id INTEGER,
		actor_tenant_membership_id INTEGER,
		issued_authorization_version INTEGER,
		execution_authorization_id INTEGER,
		authorization_effects TEXT,
		authorization_expires_at DATETIME,
		execution_config JSON,
		error_details JSON,
		metadata JSON,
		execution_time_ms INTEGER,
		rows_affected INTEGER,
		records_read INTEGER,
		records_written INTEGER,
		bytes_read INTEGER,
		bytes_written INTEGER,
		lease_owner TEXT,
		lease_expires_at DATETIME,
		attempt INTEGER NOT NULL DEFAULT 0,
		max_attempts INTEGER NOT NULL DEFAULT 3,
		started_at DATETIME,
		completed_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create task executions table: %v", err)
	}
	return db
}

func insertExecutionHandlerRow(t *testing.T, db *gorm.DB, id, tenantID int, executionID, module, taskType, status string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO common.task_executions (
		id, tenant_id, execution_id, module, task_type, source, status, progress, trigger_type, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		id, tenantID, executionID, module, taskType, module, status, 100, commonExecution.TriggerTypeManual).Error; err != nil {
		t.Fatalf("insert task execution: %v", err)
	}
}

func TestIsQualityCheckExecution(t *testing.T) {
	valid := &commonExecution.TaskExecution{Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityCheck}
	if !isQualityCheckExecution(valid) {
		t.Fatal("check execution was rejected")
	}
	for _, item := range []*commonExecution.TaskExecution{
		nil,
		{Module: commonExecution.ModuleQuality, TaskType: "cleanup_executor"},
		{Module: commonExecution.ModuleSystem, TaskType: commonExecution.TaskTypeQualityCheck},
	} {
		if isQualityCheckExecution(item) {
			t.Fatalf("non-quality check execution accepted: %#v", item)
		}
	}
}
