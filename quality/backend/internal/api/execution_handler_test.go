package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQualityExecutionFilterIsScopedToBusinessQualityTaskTypes(t *testing.T) {
	filter := qualityExecutionFilter(7, 2, 50)
	if filter.TenantID != 7 || filter.Module != commonExecution.ModuleQuality || filter.Page != 2 || filter.PageSize != 50 {
		t.Fatalf("quality execution filter = %#v", filter)
	}
	wantTaskTypes := []string{commonExecution.TaskTypeQualityPlan}
	if len(filter.TaskTypes) != len(wantTaskTypes) || filter.TaskTypes[0] != wantTaskTypes[0] {
		t.Fatalf("quality execution task types = %#v, want %#v", filter.TaskTypes, wantTaskTypes)
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
	insertExecutionHandlerRow(t, db, 1, 7, "quality-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, commonExecution.ExecutionStatusSuccess)
	insertExecutionHandlerRow(t, db, 2, 8, "quality-8", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, commonExecution.ExecutionStatusSuccess)
	insertExecutionHandlerRow(t, db, 3, 7, "other-7", commonExecution.ModuleSystem, "cleanup", commonExecution.ExecutionStatusSuccess)
	insertExecutionHandlerRow(t, db, 4, 7, "gate-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, commonExecution.ExecutionStatusSuccess)
	insertExecutionHandlerRow(t, db, 5, 7, "cleanup-7", commonExecution.ModuleQuality, commonExecution.TaskTypeCleanupExecutor, commonExecution.ExecutionStatusSuccess)
	handler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))
	router := gin.New()
	router.GET("/executions", withExecutionHandlerAuth(7, 11), handler.List)

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

func TestExecutionGetExposesPlanOutputs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newExecutionHandlerTestDB(t)
	insertExecutionHandlerRow(t, db, 1, 7, "gate-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, commonExecution.ExecutionStatusSuccess)
	if err := db.Exec(`UPDATE common.task_executions SET metadata = ? WHERE execution_id = ?`,
		`{"outputs":{"passed":true}}`, "gate-7").Error; err != nil {
		t.Fatalf("set gate outputs: %v", err)
	}

	handler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))
	router := gin.New()
	router.GET("/executions/:execution_id", withExecutionHandlerAuth(7, 11), handler.Get)
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
	if body.Outputs["passed"] != true {
		t.Fatalf("outputs = %#v", body.Outputs)
	}
}

func TestExecutionGetRejectsCrossTenantAndNonQualityExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newExecutionHandlerTestDB(t)
	insertExecutionHandlerRow(t, db, 1, 7, "quality-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, commonExecution.ExecutionStatusSuccess)
	insertExecutionHandlerRow(t, db, 2, 7, "system-7", commonExecution.ModuleSystem, "cleanup", commonExecution.ExecutionStatusSuccess)
	handler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))

	crossTenantRouter := gin.New()
	crossTenantRouter.GET("/executions/:execution_id", withExecutionHandlerAuth(8, 22), handler.Get)
	crossTenantResponse := httptest.NewRecorder()
	crossTenantRouter.ServeHTTP(crossTenantResponse, httptest.NewRequest(http.MethodGet, "/executions/quality-7", nil))
	if crossTenantResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant status = %d, want %d, body=%s", crossTenantResponse.Code, http.StatusNotFound, crossTenantResponse.Body.String())
	}

	nonQualityRouter := gin.New()
	nonQualityRouter.GET("/executions/:execution_id", withExecutionHandlerAuth(7, 11), handler.Get)
	nonQualityResponse := httptest.NewRecorder()
	nonQualityRouter.ServeHTTP(nonQualityResponse, httptest.NewRequest(http.MethodGet, "/executions/system-7", nil))
	if nonQualityResponse.Code != http.StatusNotFound {
		t.Fatalf("non-quality status = %d, want %d, body=%s", nonQualityResponse.Code, http.StatusNotFound, nonQualityResponse.Body.String())
	}
}

func TestQualityProfessionalExecutionProjectionKeepsDomainFactsOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newExecutionHandlerTestDB(t)
	insertExecutionHandlerRow(t, db, 1, 7, "professional", "quality", "quality_plan", "failed")
	if err := db.Exec(`UPDATE common.task_executions SET execution_config = ?, metadata = ?, error_details = ?, execution_authorization_id = ? WHERE execution_id = ?`,
		`{"task_version":3,"owner_domain_id":null,"target_key":"target","table_bindings":[{"alias":"target","locator":"addp://engine/1/path/public/a?type=table","private":"private-binding"}],"private":"private-config"}`,
		`{"passed":false,"quality_score":0,"rules":[{"rule_key":"rule","revision_no":2,"total_count":2,"observed":{"row_count":2,"private":"private-observation"},"private":"private-rule"}],"outputs":{"passed":false},"private":"private-metadata"}`,
		`{"code":"quality.plan.rule_failed","message":"private-error"}`, 17, "professional").Error; err != nil {
		t.Fatal(err)
	}
	handler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))
	router := gin.New()
	router.GET("/executions", withExecutionHandlerAuth(7, 11), handler.List)
	router.GET("/executions/:execution_id", withExecutionHandlerAuth(7, 11), handler.Get)
	router.GET("/provider/:execution_id", withExecutionHandlerAuth(7, 11), handler.ProviderGet)
	for _, path := range []string{"/executions", "/executions/professional", "/provider/professional"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "private-") || strings.Contains(response.Body.String(), "execution_authorization_id") {
			t.Fatalf("%s leaked private facts: %s", path, response.Body.String())
		}
		if path == "/executions/professional" {
			var body qualityExecutionResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.ExecutionConfig.TaskVersion != 3 || string(body.ExecutionConfig.OwnerDomainID) != "null" || len(body.ExecutionConfig.TableBindings) != 1 ||
				len(body.Metadata.Rules) != 1 || body.Metadata.Rules[0].TotalCount != 2 || body.Outputs.Passed == nil || *body.Outputs.Passed {
				t.Fatalf("professional domain projection lost facts: %s", response.Body.String())
			}
		} else if strings.Contains(response.Body.String(), "execution_config") || strings.Contains(response.Body.String(), "row_count") {
			t.Fatalf("%s exposed professional results: %s", path, response.Body.String())
		}
	}
}

func TestQualityExecutionProjectionPreservesUnrecordedDomain(t *testing.T) {
	item := &commonExecution.TaskExecution{}
	response, err := newQualityExecutionResponse(item)
	if err != nil || len(response.ExecutionConfig.OwnerDomainID) != 0 || response.Metadata.Passed != nil {
		t.Fatalf("unrecorded facts were manufactured: %#v, %v", response, err)
	}
}

func withExecutionHandlerAuth(tenantID, principalID int) gin.HandlerFunc {
	return func(c *gin.Context) {
		facts := authtest.NewTenantUserAuthContext(strconv.Itoa(tenantID), strconv.Itoa(principalID), []string{"quality.plan.read", "monitor.execution.read"})
		if err := commonauth.SetAuthContextForGin(c, facts); err != nil {
			panic(err)
		}
		c.Next()
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
		id, tenant_id, execution_id, module, task_type, source, source_task_id, status, progress, trigger_type, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		id, tenantID, executionID, module, taskType, module, strconv.Itoa(id), status, 100, commonExecution.TriggerTypeManual).Error; err != nil {
		t.Fatalf("insert task execution: %v", err)
	}
}

func TestIsQualityExecution(t *testing.T) {
	valid := &commonExecution.TaskExecution{Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan}
	if !isQualityExecution(valid) {
		t.Fatal("check execution was rejected")
	}
	for _, item := range []*commonExecution.TaskExecution{
		nil,
		{Module: commonExecution.ModuleQuality, TaskType: "cleanup_executor"},
		{Module: commonExecution.ModuleQuality, TaskType: "check"},
		{Module: commonExecution.ModuleQuality, TaskType: "data_validation"},
		{Module: commonExecution.ModuleSystem, TaskType: commonExecution.TaskTypeQualityPlan},
	} {
		if isQualityExecution(item) {
			t.Fatalf("non-quality check execution accepted: %#v", item)
		}
	}
	if !isQualityExecution(&commonExecution.TaskExecution{Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan}) {
		t.Fatal("quality plan execution was rejected")
	}
}
