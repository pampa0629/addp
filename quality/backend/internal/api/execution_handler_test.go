package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/gin-gonic/gin"
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
