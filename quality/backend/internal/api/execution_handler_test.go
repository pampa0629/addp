package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQualityExecutionFilterIsScopedToQualityModule(t *testing.T) {
	filter := qualityExecutionFilter(7, 2, 50)
	if filter.TenantID != 7 || filter.Module != commonExecution.ModuleQuality || filter.TaskType != "" || filter.Page != 2 || filter.PageSize != 50 {
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
	insertExecutionHandlerRow(t, db, 4, 7, "gate-7", commonExecution.ModuleQuality, commonExecution.TaskTypeMaterializationGate, commonExecution.ExecutionStatusSuccess)
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
	if body.Total != 2 || len(body.Data) != 2 || body.Data[0].ExecutionID != "gate-7" || body.Data[1].ExecutionID != "quality-7" {
		t.Fatalf("filtered executions = %#v", body)
	}
	if body.Page != 1 || body.PageSize != 100 || body.TotalPages != 1 {
		t.Fatalf("normalized execution pagination = %#v", body)
	}
}

func TestExecutionGetExposesMaterializationGateOutputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newExecutionHandlerTestDB(t)
	insertExecutionHandlerRow(t, db, 1, 7, "gate-7", commonExecution.ModuleQuality, commonExecution.TaskTypeMaterializationGate, commonExecution.ExecutionStatusSuccess)
	if err := db.Exec(`UPDATE common.task_executions SET metadata = ? WHERE execution_id = ?`,
		`{"outputs":{"materialization_group_id":5,"materialization_group_version":3}}`, "gate-7").Error; err != nil {
		t.Fatalf("set gate outputs: %v", err)
	}

	handler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))
	router := gin.New()
	router.GET("/executions/:execution_id", withIssueHandlerAuth(7, 11), handler.Get)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/executions/gate-7", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Outputs map[string]interface{} `json:"outputs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Outputs["materialization_group_id"] != float64(5) || body.Outputs["materialization_group_version"] != float64(3) {
		t.Fatalf("outputs = %#v", body.Outputs)
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
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
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

func TestIsQualityExecution(t *testing.T) {
	valid := &commonExecution.TaskExecution{Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityCheck}
	if !isQualityExecution(valid) {
		t.Fatal("check execution was rejected")
	}
	for _, item := range []*commonExecution.TaskExecution{
		nil,
		{Module: commonExecution.ModuleQuality, TaskType: "cleanup_executor"},
		{Module: commonExecution.ModuleSystem, TaskType: commonExecution.TaskTypeQualityCheck},
	} {
		if isQualityExecution(item) {
			t.Fatalf("non-quality check execution accepted: %#v", item)
		}
	}
	if !isQualityExecution(&commonExecution.TaskExecution{Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeMaterializationGate}) {
		t.Fatal("materialization gate execution was rejected")
	}
}
