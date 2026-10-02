package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/execution"
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	transferauthorization "github.com/addp/transfer/internal/authorization"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

func TestTransferEventsReuseOwnerScopeAndRejectUnownedAdHoc(t *testing.T) {
	db := newTransferTaskHandlerTestDB(t)
	actor, otherActor := 9, 10
	task := "deleted-task"
	for _, item := range []execution.TaskExecution{
		{TenantID: 7, ExecutionID: "mine", TriggeredBy: &actor},
		{TenantID: 7, ExecutionID: "other", TriggeredBy: &otherActor},
		{TenantID: 8, ExecutionID: "cross", TriggeredBy: &actor},
		{TenantID: 7, ExecutionID: "deleted", SourceTaskID: &task},
	} {
		item.Module = execution.ModuleTransfer
		item.TaskType = execution.TaskTypeSync
		item.Source = execution.ModuleTransfer
		item.Status = execution.ExecutionStatusSuccess
		item.TriggerType = execution.TriggerTypeManual
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		event := execution.Event{TenantID: item.TenantID, ExecutionID: item.ExecutionID, Module: item.Module, TaskType: item.TaskType, Attempt: 1, OccurredAt: time.Now().UTC(), Kind: "completed", Counters: models.JSONMap{}}
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	authServer := authtest.NewTenantAuthContextServer(t, "7", transferauthorization.PermissionTransferTaskRead)
	defer authServer.Close()
	router := gin.New()
	router.Use(auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: authServer.URL}), auth.MustNewContextGuard("tenant"))
	handler := NewExecutionHandler(service.NewExecutionService(db, execution.NewTaskExecutionRepository(db)), nil)
	router.GET("/executions/:execution_id/events", auth.MustNewPermissionGuard(transferauthorization.PermissionTransferTaskRead), handler.GetExecutionEvents)
	for _, check := range []struct {
		id, query string
		want      int
	}{
		{"mine", "", 200}, {"deleted", "", 200}, {"other", "", 404}, {"cross", "", 404}, {"mine", "?limit=101", 400}, {"mine", "?after=-1", 400},
	} {
		request := httptest.NewRequest(http.MethodGet, "/executions/"+check.id+"/events"+check.query, nil)
		request.Header.Set("Authorization", "Bearer "+authtest.UserToken)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != check.want {
			t.Fatalf("%s status=%d body=%s", check.id+check.query, response.Code, response.Body.String())
		}
	}
	// The event API is a User diagnostic path; service credentials are not promoted.
	request := httptest.NewRequest(http.MethodGet, "/executions/mine/events", nil)
	request.Header.Set("Authorization", "Bearer "+authtest.AssetServiceToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("service status=%d", response.Code)
	}
}
