package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	transferauthorization "github.com/addp/transfer/internal/authorization"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

func TestTransferExecutionSourceModuleComesFromAuthenticatedServiceClient(t *testing.T) {
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer asset-service": {ClientID: "addp-asset", Permissions: []string{"transfer.execution.create"}},
	})
	defer authServer.Close()

	router := gin.New()
	router.Use(commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: authServer.URL}))
	router.GET("/source", func(c *gin.Context) {
		module, ok := transferExecutionSourceModule(c)
		if !ok {
			c.Status(http.StatusForbidden)
			return
		}
		c.String(http.StatusOK, module)
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/source", nil)
	request.Header.Set("Authorization", "Bearer asset-service")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "asset" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestOwnedExecutionResultIsScopedToAuthenticatedSourceModule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer develop-read": {ClientID: "addp-develop", Permissions: []string{transferauthorization.PermissionTransferExecutionRead}},
		"Bearer manager-read": {ClientID: "addp-manager", Permissions: []string{transferauthorization.PermissionTransferExecutionRead}},
		"Bearer create-only":  {ClientID: "addp-develop", Permissions: []string{transferauthorization.PermissionTransferExecutionCreate}},
	})
	defer authServer.Close()
	db := newTransferTaskHandlerTestDB(t)
	execution := commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "develop-export-execution", Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleDevelop,
		Status: commonExecution.ExecutionStatusSuccess, TriggerType: commonExecution.TriggerTypeManual,
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	executionService := service.NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))

	router := gin.New()
	router.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: authServer.URL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	router.GET(
		"/executions/:execution_id/result",
		commonAuth.MustNewPermissionGuard(transferauthorization.PermissionTransferExecutionRead),
		NewExecutionHandler(executionService, nil).GetOwnedExecutionResult,
	)

	for _, test := range []struct {
		name, token string
		want        int
	}{
		{name: "owner module", token: "develop-read", want: http.StatusOK},
		{name: "different module", token: "manager-read", want: http.StatusNotFound},
		{name: "missing read permission", token: "create-only", want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/executions/develop-export-execution/result", nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func setTransferTestAuthContext(t *testing.T, c *gin.Context, tenantID, userID uint) {
	t.Helper()
	tenantIDText := strconv.FormatUint(uint64(tenantID), 10)
	userIDText := strconv.FormatUint(uint64(userID), 10)
	authContext := authtest.NewTenantUserAuthContext(tenantIDText, userIDText, []string{"transfer.task.read"})
	if err := commonAuth.SetAuthContextForGin(c, authContext); err != nil {
		t.Fatalf("set canonical AuthContext: %v", err)
	}
}
