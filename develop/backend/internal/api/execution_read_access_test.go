package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProfessionalExecutionReadEnforcesCurrentActorAndPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repo := commonExecution.NewTaskExecutionRepository(db)
	actor, membership, version, triggered := int64(9), int64(1), int64(1), 9
	sourceTask := "deleted-definition"
	rows := []commonExecution.TaskExecution{
		{ExecutionID: "mine", TaskType: "query"},
		{ExecutionID: "saved", TaskType: "workflow", SourceTaskID: &sourceTask},
		{ExecutionID: "notebook", TaskType: "script"},
		{ExecutionID: "other", TaskType: "query"},
		{ExecutionID: "cross-tenant", TaskType: "query"},
		{ExecutionID: "missing-origin", TaskType: "query"},
		{ExecutionID: "wrong-membership", TaskType: "query"},
		{ExecutionID: "other-module", TaskType: "query"},
	}
	for i := range rows {
		row := &rows[i]
		row.TenantID = 7
		row.Module = "develop"
		row.Source = "develop"
		row.Status = "success"
		row.TriggerType = "manual"
		row.TriggeredBy = &triggered
		row.ActorPrincipalID = &actor
		row.ActorTenantMembershipID = &membership
		row.IssuedAuthorizationVersion = &version
		row.ExecutionConfig = map[string]interface{}{"content": map[string]interface{}{"query": "SELECT own_private_snapshot"}}
		row.Metadata = map[string]interface{}{"result": map[string]interface{}{"rows": []string{"own-result"}}}
		switch row.ExecutionID {
		case "other":
			p, by := int64(10), 10
			row.ActorPrincipalID = &p
			row.TriggeredBy = &by
		case "cross-tenant":
			row.TenantID = 8
		case "missing-origin":
			row.ActorPrincipalID = nil
		case "wrong-membership":
			m := int64(2)
			row.ActorTenantMembershipID = &m
		case "other-module":
			row.Module = "transfer"
		}
		if err := repo.Create(t.Context(), row); err != nil {
			t.Fatal(err)
		}
	}
	all := []string{"develop.task.read", "develop.data_read.execute", "develop.notebook.read"}
	tokens := map[string]authorization.AuthContext{
		"all":       authtest.NewTenantUserAuthContext("7", "9", all),
		"read-only": authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read"}),
		"data-only": authtest.NewTenantUserAuthContext("7", "9", []string{"develop.data_read.execute"}),
		"revoked":   authtest.NewTenantUserAuthContext("7", "9", all),
	}
	revoked := tokens["revoked"]
	revoked.Authorization.AuthorizationVersion = "2"
	tokens["revoked"] = revoked
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		facts, ok := tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(facts)
	}))
	defer system.Close()
	executor := service.NewDevExecutor(nil, repo, nil, nil, nil, nil, nil, nil)
	handler := NewExecutionHandler(executor, nil, nil)
	router := gin.New()
	router.Use(commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: system.URL}), commonAuth.MustNewContextGuard("tenant"), executionAuthContextMiddleware)
	group := router.Group("/executions", commonAuth.MustNewPermissionGuard("develop.task.read"))
	group.GET("", handler.ListExecutions)
	group.GET("/statistics", handler.GetExecutionStatistics)
	group.GET("/:execution_id", handler.GetExecution)
	group.GET("/:execution_id/logs", handler.GetExecutionLogs)
	for _, test := range []struct {
		token, path string
		status      int
	}{
		{"all", "/executions/mine", 200}, {"all", "/executions/saved", 200}, {"all", "/executions/notebook", 200},
		{"all", "/executions/other", 404}, {"all", "/executions/cross-tenant", 404}, {"all", "/executions/missing-origin", 404}, {"all", "/executions/wrong-membership", 404}, {"all", "/executions/other-module", 404},
		{"all", "/executions/other/logs", 404}, {"read-only", "/executions/mine", 404}, {"data-only", "/executions/mine", 403}, {"revoked", "/executions/mine", 404},
		{"all", "/executions", 200}, {"all", "/executions/statistics", 200}, {"read-only", "/executions", 200}, {"revoked", "/executions/statistics", 200},
		{"all", "/executions?page_size=101", 400},
	} {
		t.Run(test.token+test.path, func(t *testing.T) {
			request := httptest.NewRequest("GET", test.path, nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "actor_principal_id") || strings.Contains(response.Body.String(), "execution_authorization_id") {
				t.Fatal("authorization facts leaked")
			}
			if test.path == "/executions" || test.path == "/executions/statistics" {
				var body map[string]interface{}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				field := "total"
				if test.path == "/executions/statistics" {
					field = "total_executions"
				}
				want := float64(3)
				if test.token != "all" {
					want = 0
				}
				if body[field] != want {
					t.Fatalf("%s=%v want%v", field, body[field], want)
				}
			}
			if test.path == "/executions/mine" && test.status == 200 && !strings.Contains(response.Body.String(), "own-result") {
				t.Fatal("own professional result lost")
			}
		})
	}
}
