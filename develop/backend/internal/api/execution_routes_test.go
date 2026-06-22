package api

import (
	"encoding/json"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/gin-gonic/gin"
)

func TestExecutionRoutesUseExecutionIDWildcard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	executions := router.Group("/api/v1/develop/executions")

	executions.GET("/:execution_id", func(c *gin.Context) {})
	executions.GET("/:execution_id/logs", func(c *gin.Context) {})
	executions.POST("/:execution_id/retry", func(c *gin.Context) {})
}

func TestProviderDevTaskListResponseUsesStandardItemsShape(t *testing.T) {
	now := time.Now()
	body, err := json.Marshal(models.ListProviderDevTasksResponse{
		Items: []models.ProviderDevTask{{
			ID:        1,
			TenantID:  7,
			Name:      "query task",
			TaskType:  commonExecution.TaskTypeQuery,
			Enabled:   true,
			CreatedAt: now,
			UpdatedAt: now,
			Status:    "active",
		}},
		Total:    1,
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("marshal ListProviderDevTasksResponse: %v", err)
	}

	assertStandardTaskProviderListShape(t, body)
}

func TestProviderExecuteDevResponseUsesStandardExecutionShape(t *testing.T) {
	body, err := json.Marshal(providerExecuteDevResponse{
		ExecutionID: "develop-exec-1",
		Status:      commonExecution.ExecutionStatusRunning,
	})
	if err != nil {
		t.Fatalf("marshal providerExecuteDevResponse: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	if resp["execution_id"] != "develop-exec-1" || resp["status"] != commonExecution.ExecutionStatusRunning {
		t.Fatalf("response = %#v, want execution_id and status", resp)
	}
	for _, legacyField := range []string{"message", "data", "id"} {
		if _, ok := resp[legacyField]; ok {
			t.Fatalf("response contains non-standard field %q: %s", legacyField, body)
		}
	}
}

func assertStandardTaskProviderListShape(t *testing.T, body []byte) {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	for _, field := range []string{"items", "total", "page", "page_size"} {
		if _, ok := resp[field]; !ok {
			t.Fatalf("response missing %q: %s", field, body)
		}
	}
	for _, legacyField := range []string{"data", "status", "message", "total_pages", "tasks"} {
		if _, ok := resp[legacyField]; ok {
			t.Fatalf("response contains non-standard field %q: %s", legacyField, body)
		}
	}
}
