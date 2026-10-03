package execution_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReadScopesFilterListCountTreeAndAdHocOwnership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repo := execution.NewTaskExecutionRepository(db)
	task, actor, otherActor := "deleted-task", 9, 10
	root := "root"
	emptyTask := ""
	rows := []execution.TaskExecution{
		{ExecutionID: root, TenantID: 7, Module: "orchestrator", TaskType: "orchestration", SourceTaskID: &task},
		{ExecutionID: "visible", TenantID: 7, Module: "transfer", TaskType: "sync", SourceTaskID: &task, ParentExecutionID: &root},
		{ExecutionID: "hidden-child", TenantID: 7, Module: "quality", TaskType: "quality_plan", SourceTaskID: &task, ParentExecutionID: &root},
		{ExecutionID: "mine", TenantID: 7, Module: "transfer", TaskType: "sync", TriggeredBy: &actor},
		{ExecutionID: "other", TenantID: 7, Module: "transfer", TaskType: "sync", TriggeredBy: &otherActor},
		{ExecutionID: "empty-task", TenantID: 7, Module: "transfer", TaskType: "sync", SourceTaskID: &emptyTask},
		{ExecutionID: "no-actor", TenantID: 7, Module: "transfer", TaskType: "sync"},
		{ExecutionID: "cross-tenant", TenantID: 8, Module: "transfer", TaskType: "sync", SourceTaskID: &task},
	}
	for i := range rows {
		rows[i].Status = "success"
		rows[i].TriggerType = "manual"
		if err = repo.Create(context.Background(), &rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	scopes := []execution.ReadScope{{Module: "transfer", TenantID: 7, PrincipalID: 9, Grants: []execution.ReadGrant{{TaskType: "sync", TaskHistory: true, OwnAdHoc: true}}}}
	ctx := execution.WithReadScopes(context.Background(), scopes)
	scopes[0].Grants[0].TaskHistory = false
	list, total, err := repo.List(ctx, execution.TaskExecutionFilter{TenantID: 7, Page: 1, PageSize: 20})
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("list=%#v total=%d err=%v", list, total, err)
	}
	stats, err := repo.GetStatistics(ctx, execution.TaskExecutionFilter{TenantID: 7})
	if err != nil || stats.Total != 2 {
		t.Fatalf("stats=%#v err=%v", stats, err)
	}
	children, err := repo.ListChildrenByParentExecutionID(ctx, root, 7, 20)
	if err != nil || len(children) != 1 || children[0].ExecutionID != "visible" {
		t.Fatalf("children=%#v err=%v", children, err)
	}
	for _, id := range []string{"empty-task", "other", "no-actor", "hidden-child", "cross-tenant"} {
		if _, err = repo.GetByExecutionID(ctx, id, 7); err == nil {
			t.Fatalf("read denied %s", id)
		}
	}
	_, total, err = repo.List(execution.WithReadScopes(context.Background(), []execution.ReadScope{}), execution.TaskExecutionFilter{TenantID: 7})
	if err != nil || total != 0 {
		t.Fatalf("empty grant total=%d err=%v", total, err)
	}
}

func TestOwnerReadScopeReauthenticatesUserAndRejectsService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	system := authtest.NewTenantAuthContextServer(t, "7", "transfer.task.read")
	defer system.Close()
	router := gin.New()
	router.Use(auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: system.URL}), auth.MustNewContextGuard("tenant"))
	router.GET("/scope", execution.OwnerReadScopeHandler("transfer", map[string]string{"sync": "transfer.task.read", "denied": "quality.plan.read"}))
	for _, test := range []struct {
		token  string
		status int
	}{{authtest.UserToken, 200}, {authtest.AssetServiceToken, 403}, {authtest.OtherServiceToken, 403}} {
		request := httptest.NewRequest("GET", "/scope", nil)
		request.Header.Set("Authorization", "Bearer "+test.token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s status=%d body=%s", test.token, response.Code, response.Body.String())
		}
		if response.Code == http.StatusOK {
			var scope execution.ReadScope
			if err := json.Unmarshal(response.Body.Bytes(), &scope); err != nil || scope.TenantID != 7 || scope.PrincipalID != 9 || len(scope.Grants) != 1 || scope.Grants[0].TaskType != "sync" {
				t.Fatalf("scope=%#v", scope)
			}
		}
	}
}

func TestObservationExcludesPayloadsAndSanitizesErrors(t *testing.T) {
	secret := "sensitive-sentinel"
	item := &execution.TaskExecution{ExecutionID: "observed", Status: "failed", ExecutionConfig: map[string]interface{}{"password": secret}, Metadata: map[string]interface{}{
		"execution_logs": secret, "result": map[string]interface{}{"final_result": secret, "traceback": secret, "runtime_status": map[string]interface{}{"error_code": "EXECUTION_FAILED", "details": secret}},
		"step_results": map[string]interface{}{"read": map[string]interface{}{"status": "failed", "error": "password=" + secret, "result": map[string]interface{}{"row": secret}, "duration": int64(12)}},
		"continuous":   map[string]interface{}{"diagnostics": map[string]interface{}{"health": "healthy", "credentials": secret}},
	}, ErrorDetails: map[string]interface{}{"message": "connect failed password=" + secret, "stack": secret, "failed_target_samples": []string{secret}}}
	observation := execution.Observe(item)
	payload, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	for _, denied := range []string{secret, "execution_config", "final_result", "traceback", "failed_target_samples"} {
		if strings.Contains(string(payload), denied) {
			t.Fatalf("exposed %s: %s", denied, payload)
		}
	}
	if len(observation.Steps) != 1 || observation.Steps[0].Status != "failed" || (observation.Steps[0].Duration == nil || *observation.Steps[0].Duration != 12) {
		t.Fatalf("steps=%#v", observation.Steps)
	}
	item.Attempt = 2
	if !execution.Observe(item).StepsAttemptUnverified {
		t.Fatal("untagged retry steps presented as current attempt evidence")
	}
	item.Status = "success"
	if len(execution.Observe(item).ErrorDetails) != 0 {
		t.Fatal("success exposed error details")
	}
}

func TestDiagnosticCredentialsWithQuotedSpacesAreRemoved(t *testing.T) {
	for _, text := range []string{`password="secret with spaces"`, `token='secret with spaces'`, `https://user:secret@host/query?key=secret`} {
		safe := execution.SafeDiagnosticText(text)
		if strings.Contains(safe, "secret") || strings.Contains(safe, "spaces") {
			t.Fatalf("unsafe diagnostic: %s", safe)
		}
	}
}

func TestObservationBoundsHistoryAndPreservesOnlyCanonicalResourceContext(t *testing.T) {
	inputs := make([]execution.LineageResourceRef, 101)
	for i := range inputs {
		inputs[i] = execution.LineageResourceRef{Locator: "addp://engine/1/path/public/roads?type=table&password=private-sentinel", SchemaSnapshot: &execution.LineageSchemaSnapshot{Hash: "private-sentinel"}}
	}
	observation := execution.Observe(&execution.TaskExecution{Status: "failed", Metadata: map[string]interface{}{
		"lineage_facts": execution.LineageFacts{SchemaVersion: execution.LineageFactsSchemaVersion, Inputs: inputs},
		"step_results":  map[string]interface{}{"unknown": map[string]interface{}{"status": "running"}},
		"continuous":    map[string]interface{}{"diagnostics": map[string]interface{}{"error": "syntax error near SELECT row-secret FROM customer"}},
	}, ErrorDetails: map[string]interface{}{"message": "bad query SELECT row-secret FROM customer"}})
	payload, _ := json.Marshal(observation)
	if strings.Contains(string(payload), "private-sentinel") || strings.Contains(string(payload), "SELECT") || !observation.DiagnosticsTruncated {
		t.Fatalf("unsafe observation=%s", payload)
	}
	if len(observation.Steps) != 1 || observation.Steps[0].Duration != nil {
		t.Fatalf("invented duration=%#v", observation.Steps)
	}
}

func TestOwnerReadScopeDoesNotPromoteDepartmentPermission(t *testing.T) {
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"transfer.task.read"})
	department := "11"
	facts.Authorization.RoleAssignments[0].Scope.Type = "department"
	facts.Authorization.RoleAssignments[0].Scope.DepartmentID = &department
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(facts) }))
	defer system.Close()
	router := gin.New()
	router.Use(auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: system.URL}), auth.MustNewContextGuard("tenant"))
	router.GET("/scope", execution.OwnerReadScopeHandler("transfer", map[string]string{"sync": "transfer.task.read"}))
	request := httptest.NewRequest("GET", "/scope", nil)
	request.Header.Set("Authorization", "Bearer user")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var scope execution.ReadScope
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &scope) != nil || len(scope.Grants) != 0 {
		t.Fatalf("department grant promoted: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestStepProjectionShowsClosedPhaseAndFailureCause(t *testing.T) {
	item := &execution.TaskExecution{Metadata: models.JSONMap{"step_results": map[string]interface{}{
		"uncertain": map[string]interface{}{"status": "failed", "phase": "terminal", "error_code": "orchestrator.execution.dispatch_uncertain", "error": "password=private", "result": map[string]interface{}{"execution_id": "private-child"}},
		"waiting":   map[string]interface{}{"status": "running", "phase": "waiting"},
		"unsafe":    map[string]interface{}{"status": "running", "phase": "password=private"},
	}}}
	observed := execution.Observe(item)
	for _, step := range observed.Steps {
		switch step.ID {
		case "uncertain":
			if step.ErrorCode != "submission_uncertain" || step.Phase != "terminal" {
				t.Fatalf("step=%+v", step)
			}
		case "waiting":
			if step.Phase != "waiting" {
				t.Fatalf("step=%+v", step)
			}
		case "unsafe":
			if step.Phase != "" {
				t.Fatal("private phase leaked")
			}
		}
	}
}
