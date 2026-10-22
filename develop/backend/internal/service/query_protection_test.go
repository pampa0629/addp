package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
)

type choiceTestGate struct {
	allowDevelopProtectionGate
	validations int
	roles       []string
	deny        bool
}

func (g *choiceTestGate) RoleOptions(context.Context, int64, dataprotection.ResourceReference, []string) ([]dataprotection.FieldRoleOptions, error) {
	return nil, nil
}
func (g *choiceTestGate) ValidateRoleChoices(_ context.Context, _ int64, selection *dataprotection.QueryRoleSelection) error {
	g.validations++
	g.roles = selection.RoleKeys
	if g.deny {
		return dataprotection.ErrRoleOptionUnavailable
	}
	return nil
}

func TestQueryChoiceSaveRechecksEvenNameOnlyAndUsesCurrentUserCredential(t *testing.T) {
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read"})
	ctx := WithExecutionAuthContext(t.Context(), facts)
	roleReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/auth/subject-roles":
			roleReads++
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer addp_at_current" || r.URL.RawQuery != "" {
				t.Error("self observation used selectors or a Service identity")
			}
			json.NewEncoder(w).Encode(authorization.CurrentSubjectRoles{TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: *facts.Context.TenantMembershipID, AuthorizationVersion: facts.Authorization.AuthorizationVersion, RoleKeys: []string{"custom.analytics"}, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute)})
		case "/api/v1/system/runtime/engine-descriptors/11":
			json.NewEncoder(w).Encode(map[string]interface{}{"id": 11, "engine_type": "postgresql"})
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	system := commonClient.NewSystemServiceClient(server.URL, staticServiceTokenSource("addp_at_service"), server.Client())
	gate := &choiceTestGate{}
	sql := NewSQLEngineService(nil, system, nil)
	sql.SetProtectionGate(gate)
	target, err := dataprotection.DataItemTargetFromCatalogPath(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "public", "persons"))
	if err != nil {
		t.Fatal(err)
	}
	choices := []dataprotection.FieldRoleChoice{{Target: target, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", AssessmentRevision: 1, PolicyRevision: 1, OptionID: "hash"}}}
	taskService := NewDevTaskService(repository.NewDevTaskRepository(newNotebookBindingTestDB(t)), system)
	taskService.SetQueryProtection(sql)
	task, err := taskService.CreateDevTask(ctx, "addp_at_current", &models.CreateDevTaskRequest{Name: "choice-query", DevType: "query", Content: models.DevTaskContent{"query": "SELECT phone FROM public.persons", "query_type": "sql", "target_locator": "addp://engine/11/path/public/persons?type=table", "protection_choices": choices}, ExecutionConfig: models.DevTaskContent{"engine_id": 11}}, 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	gate.deny = true
	if _, err := taskService.UpdateDevTask(ctx, "addp_at_current", task.ID, &models.UpdateDevTaskRequest{Name: "should-not-save"}, 7, 9); err == nil {
		t.Fatal("name-only save skipped current role/policy checks")
	}
	stored, err := taskService.GetDevTask(task.ID, 7)
	if err != nil || stored.Name != "choice-query" {
		t.Fatal("invalid choice wrote a task revision", err)
	}
	if roleReads != 2 || gate.validations != 2 || len(gate.roles) != 1 || gate.roles[0] != "custom.analytics" {
		t.Fatal("save did not use live pure-business roles")
	}
}

func TestQueryExecutionChoiceRolesAreBoundToActualAttempt(t *testing.T) {
	executionID := "00000000-0000-0000-0000-000000000041"
	leaseToken := "00000000-0000-0000-0000-000000000042"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/system/execution-authorizations/81/subject-roles" || r.Header.Get("Authorization") != "Bearer addp_at_service" {
			t.Error("wrong role observation credential or binding")
		}
		var req commonClient.ExecutionSubjectRolesRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.ExecutionID != executionID || req.Attempt != 2 || req.LeaseToken != leaseToken {
			t.Error("wrong execution binding")
		}
		json.NewEncoder(w).Encode(commonExecution.SubjectRoles{AuthorizationID: "81", ExecutionID: executionID, Attempt: 2, Owner: "develop", TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: "1", AuthorizationVersion: "1", RoleKeys: []string{"custom.analytics"}, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute)})
	}))
	defer server.Close()
	sql := NewSQLEngineService(nil, commonClient.NewSystemServiceClient(server.URL, staticServiceTokenSource("addp_at_service"), server.Client()), nil)
	task := &models.DevTask{Content: models.DevTaskContent{"query_type": "sql", "protection_choices": []dataprotection.FieldRoleChoice{{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "target"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", AssessmentRevision: 1, PolicyRevision: 1, OptionID: "hash"}}}}}
	issued := &IssuedSQLExecutionAuthorization{AuthorizationID: 81, ActorPrincipalID: 9, ActorTenantMembershipID: 1, IssuedAuthorizationVersion: 1, Effects: []SQLExecutionEffect{SQLExecutionEffectRead}, ExpiresAt: time.Now().Add(2 * time.Minute)}
	for range 2 {
		roles, err := sql.executionQueryRoles(t.Context(), &commonExecution.TaskExecution{ExecutionID: executionID, TenantID: 7}, commonExecution.Lease{Attempt: 2, Token: leaseToken}, issued, task)
		if err != nil || roles == nil || len(roles.RoleKeys) != 1 {
			t.Fatal("execution-bound current roles missing", err)
		}
	}
	if calls != 2 {
		t.Fatal("roles cached across executions")
	}
}
