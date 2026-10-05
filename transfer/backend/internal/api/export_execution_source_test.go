package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/execution"
	"github.com/addp/common/middleware/auth"
	transferauthorization "github.com/addp/transfer/internal/authorization"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

func TestAdHocExecutionRejectsClientClaimedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTransferTaskHandlerTestDB(t)
	executions := service.NewExecutionService(db, execution.NewTaskExecutionRepository(db))
	tasks := service.NewTaskService(db, nil, nil)
	tasks.SetExecutionService(executions)
	handler := NewExecutionHandler(executions, tasks)
	system := authtest.NewTenantAuthContextServer(t, "7", transferauthorization.PermissionTransferExecutionCreate)
	defer system.Close()
	router := gin.New()
	router.Use(auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: system.URL}), auth.MustNewContextGuard("tenant"))
	router.POST("/executions", auth.MustNewPermissionGuard(transferauthorization.PermissionTransferExecutionCreate), handler.CreateExecution)
	body := `{"name":"machine export","config":{"runtime":{"boundary":"bounded"},"load":{"mode":"snapshot"},"source":{"locator":"addp://engine/1/path/public/orders?type=table","data_type":"table","representation":"native"},"target":{"parent_locator":"addp-infra://minio/manager/tenant_7/export/source?type=prefix","name":"orders.csv","data_type":"table","representation":"encoded","format":"csv","policy":{"apply_mode":"replace"}}}}`
	for _, field := range []string{`"user_id":9`, `"triggered_by":9`, `"actor_principal_id":9`, `"tenant_id":8`, `"callback_url":"http://127.0.0.1:1"`} {
		request := httptest.NewRequest("POST", "/executions", strings.NewReader(strings.TrimSuffix(body, "}")+","+field+"}"))
		request.Header.Set("Authorization", "Bearer "+authtest.AssetServiceToken)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("claimed identity status=%d", response.Code)
		}
	}
	request := httptest.NewRequest("POST", "/executions", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+authtest.UserToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatalf("human creation status=%d", response.Code)
	}
	request = httptest.NewRequest("POST", "/executions", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+authtest.AssetServiceToken)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 202 {
		t.Fatalf("machine creation status=%d", response.Code)
	}
	var rows []execution.TaskExecution
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TriggeredBy != nil {
		t.Fatal("machine execution acquired a human initiator")
	}
}
