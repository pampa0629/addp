package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecutionFailureLocalizationPreservesStoredFacts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Accept-Language", "en")
	commoni18n.I18nMiddleware()(c)
	code := "orchestrator.execution.dispatch_uncertain"
	item := &execution.TaskExecution{Status: "failed", ErrorDetails: commonModels.JSONMap{"code": code, "message": "stored message"}, Metadata: commonModels.JSONMap{"step_results": map[string]interface{}{"step": map[string]interface{}{"error_code": code, "error": "stored step", "result": map[string]interface{}{"execution_id": "841843cf-4576-41e9-9316-9a253abf3694"}}}}}
	projected := localizeExecutionFailure(c, item)
	if !strings.Contains(projected.ErrorDetails["message"].(string), "No automatic replay") || item.ErrorDetails["message"] != "stored message" {
		t.Fatalf("failure=%v source=%v", projected.ErrorDetails, item.ErrorDetails)
	}
	step := projected.Metadata.StepResults["step"]
	if !strings.Contains(step.Error, "No automatic replay") || step.Result == nil || step.Result.ExecutionID != "841843cf-4576-41e9-9316-9a253abf3694" {
		t.Fatalf("step=%v", step)
	}
	stored := item.Metadata["step_results"].(map[string]interface{})["step"].(map[string]interface{})
	if stored["error"] != "stored step" {
		t.Fatal("localization modified stored metadata")
	}
}

func TestAdmissionFailureUsesLocalizedSafeError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/", nil)
	c.Request.Header.Set("Accept-Language", "en")
	commoni18n.I18nMiddleware()(c)
	respondExecutionAdmissionError(c, errors.New("password=private SQL=select*"))
	if recorder.Code != 500 || strings.Contains(recorder.Body.String(), "private") || !strings.Contains(recorder.Body.String(), "Could not create this execution") {
		t.Fatalf("admission response=%d %s", recorder.Code, recorder.Body.String())
	}
}

func TestExecutionReadProjectionExcludesInternalAndChildResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Accept-Language", "en")
	commoni18n.I18nMiddleware()(c)
	authorizationID := int64(91)
	childID := "841843cf-4576-41e9-9316-9a253abf3694"
	item := &execution.TaskExecution{Status: "failed", Attempt: 2, ExecutionAuthorizationID: &authorizationID,
		ActorPrincipalID: &authorizationID, IssuedAuthorizationVersion: &authorizationID,
		ExecutionConfig: commonModels.JSONMap{"parameters": "private-config"},
		ErrorDetails:    commonModels.JSONMap{"code": "orchestrator.execution.child_failed", "message": "private-error", "runtime": "private-runtime"},
		Metadata: commonModels.JSONMap{"private": "private-metadata", "step_results": map[string]interface{}{
			"scan":    map[string]interface{}{"status": "failed", "phase": "terminal", "duration": int64(12), "error_code": "orchestrator.execution.child_failed", "error": "private-step", "result": map[string]interface{}{"execution_id": childID, "rows": "private-rows", "outputs": "private-outputs"}},
			"unknown": map[string]interface{}{"status": "failed", "error_code": "unknown.code", "error": "private-unknown", "result": map[string]interface{}{"execution_id": "private-invalid-id"}},
		}},
	}
	for _, value := range []interface{}{localizeExecutionFailure(c, item), localizedExecutionList(c, []*execution.TaskExecution{item})} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"private-", "execution_authorization_id", "actor_principal_id", "issued_authorization_version", "execution_config"} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("internal fact leaked (%s): %s", forbidden, data)
			}
		}
		if !strings.Contains(string(data), childID) || !strings.Contains(string(data), "orchestrator.execution.child_failed") {
			t.Fatalf("step reference or closed error lost: %s", data)
		}
	}
	if item.ErrorDetails["message"] != "private-error" || item.Metadata["private"] != "private-metadata" {
		t.Fatal("projection mutated stored facts")
	}
}

func TestExecutionReadProjectionBoundsTypedSavedSteps(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	steps := models.StepResults{}
	for index := 0; index < 205; index++ {
		steps[fmt.Sprintf("step-%03d", index)] = models.StepResult{Status: "running", Phase: "waiting", Result: map[string]interface{}{"execution_id": "841843cf-4576-41e9-9316-9a253abf3694", "private": "private-result"}}
	}
	item := &execution.TaskExecution{Status: "running", Attempt: 2, Metadata: commonModels.JSONMap{"step_results": steps}}
	response := localizeExecutionFailure(c, item)
	if len(response.Metadata.StepResults) != 200 || !response.StepsTruncated || !response.StepsAttemptUnverified {
		t.Fatalf("missing bounds or retry evidence: %#v", response)
	}
	if response.Metadata.StepResults["step-000"].Result == nil {
		t.Fatal("typed stored child reference was lost")
	}
	if len(steps) != 205 {
		t.Fatal("stored steps were mutated")
	}
	if localizeExecutionFailure(c, nil) != nil {
		t.Fatal("nil execution invented")
	}
	data, err := json.Marshal(localizedExecutionList(c, nil))
	if err != nil || string(data) != "[]" {
		t.Fatalf("empty history=%s error=%v", data, err)
	}
}

func TestOrchestratorDirectExecutionReadsUseOwnerScope(t *testing.T) {
	permissions := []string{"orchestrator.workflow.read"}
	identities := map[string]authorization.AuthContext{
		"owner": authtest.NewTenantUserAuthContext("7", "11", permissions),
		"peer":  authtest.NewTenantUserAuthContext("7", "22", permissions),
		"oauth": authtest.NewTenantUserAuthContext("7", "11", permissions),
	}
	oauth := identities["oauth"]
	oauth.Token.Type = "oauth_access_token"
	oauth.Client.ScopeMode = "restricted"
	oauth.Client.Scopes = []string{"addp.api"}
	identities["oauth"] = oauth
	for _, key := range []string{"delegated", "ticket", "machine"} {
		facts := authtest.NewTenantUserAuthContext("7", "11", permissions)
		if key != "machine" {
			facts.Client.ScopeMode = "restricted"
			facts.Client.Audiences = []string{"orchestrator"}
		}
		if key == "delegated" {
			facts.Token.Type = "delegated_access_token"
			facts.Client.Scopes = []string{"orchestrator.read"}
			facts.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *facts.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
		} else if key == "ticket" {
			facts.Token.Type = "resource_access_ticket"
			facts.Client.Scopes = []string{"resource:read"}
		} else {
			facts.Principal.Type = "service_principal"
			facts.Token.Type = "service_access_token"
			facts.Authentication.Methods = []string{"service_secret"}
			facts.Authentication.AssuranceLevel = "not_applicable"
		}
		identities[key] = facts
	}
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			http.NotFound(w, r)
			return
		}
		facts, ok := identities[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(facts)
	}))
	defer authServer.Close()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	history, empty := "999", ""
	own, peer := 11, 22
	rows := []execution.TaskExecution{
		{ID: 1, ExecutionID: "deleted-plan", SourceTaskID: &history, TriggeredBy: &peer},
		{ID: 2, ExecutionID: "own-ad-hoc", TriggeredBy: &own},
		{ID: 3, ExecutionID: "peer-ad-hoc", TriggeredBy: &peer},
		{ID: 4, ExecutionID: "unknown-actor"},
		{ID: 5, ExecutionID: "empty-source", SourceTaskID: &empty, TriggeredBy: &own},
		{ID: 6, ExecutionID: "foreign", TenantID: 8, SourceTaskID: &history},
		{ID: 7, ExecutionID: "cleanup", TaskType: "cleanup_executor", SourceTaskID: &history},
		{ID: 8, ExecutionID: "meta-scan", Module: "meta", TaskType: "scan", SourceTaskID: &history},
	}
	for _, row := range rows {
		if row.TenantID == 0 {
			row.TenantID = 7
		}
		if row.Module == "" {
			row.Module = "orchestrator"
		}
		if row.TaskType == "" {
			row.TaskType = "orchestration"
		}
		row.Status = "success"
		row.TriggerType = execution.TriggerTypeManual
		if err = db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := SetupRouter(nil, service.NewExecutionService(db), nil, authServer.URL, nil, nil, nil, nil, modulelifecycle.NewStandalone("orchestrator"))
	request := func(token, path string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/orchestrator"+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != status {
			t.Fatalf("%s %s status=%d want=%d body=%s", token, path, response.Code, status, response.Body.String())
		}
		return response
	}
	for _, token := range []string{"owner", "peer", "oauth"} {
		response := request(token, "/executions?page=1&page_size=1", 200)
		var page orchestrationExecutionListResponse
		if err = json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 || page.TotalPages != 2 || len(page.Data) != 1 {
			t.Fatalf("list total leaked: %s", response.Body.String())
		}
		request(token, "/executions/deleted-plan", 200)
		allowed, denied := "own-ad-hoc", "peer-ad-hoc"
		allowedID, deniedID := "2", "3"
		if token == "peer" {
			allowed, denied = denied, allowed
			allowedID, deniedID = deniedID, allowedID
		}
		request(token, "/executions/"+allowed, 200)
		request(token, "/executions/"+denied, 404)
		request(token, "/orch-executions/"+allowedID, 200)
		request(token, "/orch-executions/"+deniedID, 404)
	}
	for _, id := range []string{"unknown-actor", "empty-source", "foreign", "cleanup", "meta-scan", "missing"} {
		request("owner", "/executions/"+id, 404)
	}
	for _, token := range []string{"delegated", "ticket", "machine"} {
		request(token, "/executions", 403)
		request(token, "/executions/deleted-plan", 403)
	}
	revoked := identities["owner"]
	revoked.Authorization.RoleAssignments[0].Permissions = []string{"monitor.execution.read"}
	identities["owner"] = revoked
	request("owner", "/executions", 403)
	request("owner", "/executions/deleted-plan", 403)
}
