package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	devrepository "github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQueryExportUsesIndependentFrozenChoiceAuthorityAndCurrentDeliveryQualification(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repo := commonExecution.NewTaskExecutionRepository(db)
	target, _ := dataprotection.DataItemTargetFromCatalogPath(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "public", "persons"))
	choice := dataprotection.FieldRoleChoice{Target: target, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "export-policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "export-sm3"}}
	queryChoice := choice
	queryChoice.Selection.PolicyID = "query-policy"
	queryChoice.Selection.OptionID = "query-mask"
	by := 9
	actor, member, version := int64(9), int64(1), int64(1)
	record := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "develop", TaskType: "query", Source: "develop", Status: "success", ExecutionBoundary: "bounded", TriggerType: "manual", TriggeredBy: &by, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, ExecutionConfig: commonModels.JSONMap{"engine_id": 11, "content": map[string]any{"query": "SELECT phone FROM public.persons", "query_type": "sql", "target_locator": "addp://engine/11/path/public/persons?type=table", "protection_choices": []dataprotection.FieldRoleChoice{queryChoice}, "export_protection_choices": []dataprotection.FieldRoleChoice{choice}}}, Metadata: commonModels.JSONMap{"result": map[string]any{"result_kind": "table", "columns": []string{"phone"}}}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repo.Create(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	denied := ""
	checks, issues := 0, 0
	issuedID := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer addp_at_current" {
			t.Error("export borrowed a Service credential")
		}
		switch r.URL.Path {
		case "/api/v1/transfer/source-protection-checks":
			checks++
			var request commonClient.TransferSourceProtectionSelection
			json.NewDecoder(r.Body).Decode(&request)
			if request.SourceLocator != "addp://engine/11/path/public/persons?type=table" || len(request.Choices) != 1 || request.Choices[0] != choice {
				t.Error("export reused query choice or changed frozen source")
			}
			if denied != "" {
				w.WriteHeader(409)
				json.NewEncoder(w).Encode(map[string]string{"error": "PRIVATE_RESPONSE", "error_code": denied})
				return
			}
			w.Write([]byte(`{"valid":true}`))
		case "/api/v1/system/auth/execution-authorizations":
			issues++
			var request commonClient.IssueExecutionAuthorizationRequest
			json.NewDecoder(r.Body).Decode(&request)
			issuedID = request.ExecutionID
			if request.Audience != "transfer" || len(request.Accesses) != 1 || request.Accesses[0].EngineID != "11" || len(request.Accesses[0].Effects) != 1 || request.Accesses[0].Effects[0] != "read" {
				t.Error("export requested wrong effects")
			}
			json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{ID: "81", ExecutionID: request.ExecutionID, Audience: request.Audience, Accesses: request.Accesses, TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: "1", IssuedAuthorizationVersion: "1", SourceType: "user", ExpiresAt: time.Now().Add(time.Hour)})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transfer := &queryExportTransferClientStub{}
	exporter := newQueryExportServiceForTest(t, db, repo, transfer)
	exporter.executor.sqlEngine = NewSQLEngineService(&config.Config{TransferServiceURL: server.URL}, nil, commonClient.NewSystemExecutionAuthorizationClient(server.URL, server.Client()))
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read", "develop.data_read.execute"})
	ctx := WithExportUserToken(WithExecutionAuthContext(t.Context(), facts), "addp_at_current")
	response, err := exporter.Create(ctx, record.ExecutionID, models.CreateQueryExportRequest{FileName: "role-export"}, 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	if !response.RequiresCurrentUser || response.TransferExecutionID != issuedID || transfer.request.ExecutionAuthorization == nil || transfer.request.Config.Source.ProtectionChoices[0] != choice || issues != 1 || checks != 1 {
		t.Fatal("missing exact frozen export choice and identity")
	}
	for _, refusal := range []struct {
		code  string
		cause error
	}{
		{"transfer_protection_roles_unavailable", dataprotection.ErrRoleOptionIneligible},
		{"transfer_protection_choice_invalid", dataprotection.ErrRoleOptionUnavailable},
	} {
		denied = refusal.code
		if _, err := exporter.Create(ctx, record.ExecutionID, models.CreateQueryExportRequest{FileName: "must-not-create"}, 7, 9); !errors.Is(err, refusal.cause) {
			t.Fatal("create lost exact export refusal", refusal.code, err)
		}
		if _, err := exporter.Get(ctx, response.ID, 7, 9); !errors.Is(err, refusal.cause) {
			t.Fatal("status lost exact export refusal", refusal.code, err)
		}
		if _, err := exporter.Open(ctx, response.ID, 7, 9); !errors.Is(err, refusal.cause) {
			t.Fatal("file lost exact export refusal", refusal.code, err)
		}
		if issues != 1 {
			t.Fatal("rejection issued new execution authority")
		}
	}
	// Choosing a query method alone never imposes it on the independent export outlet.
	content := record.ExecutionConfig["content"].(map[string]any)
	delete(content, "export_protection_choices")
	record.ExecutionID = uuid.NewString()
	record.ID = 0
	if err := repo.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	beforeChecks := checks
	response, err = exporter.Create(ctx, record.ExecutionID, models.CreateQueryExportRequest{FileName: "default-export"}, 7, 9)
	if err != nil || response.RequiresCurrentUser || len(transfer.request.Config.Source.ProtectionChoices) != 0 || transfer.request.ExecutionAuthorization != nil || checks != beforeChecks || issues != 1 {
		t.Fatal("query choice leaked into default export", err)
	}
}

func TestExportOnlySavedChoiceRecheckedOnNameOnlyUpdate(t *testing.T) {
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read"})
	denied := false
	checks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/runtime/engine-descriptors/11":
			json.NewEncoder(w).Encode(map[string]any{"id": 11, "engine_type": "postgresql"})
		case "/api/v1/transfer/source-protection-checks":
			checks++
			if r.Header.Get("Authorization") != "Bearer addp_at_current" {
				t.Error("save did not forward current User credential")
			}
			if denied {
				w.WriteHeader(409)
				w.Write([]byte(`{"error":"choice changed","error_code":"transfer_protection_choice_invalid"}`))
				return
			}
			w.Write([]byte(`{"valid":true}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	system := commonClient.NewSystemServiceClient(server.URL, staticServiceTokenSource("addp_at_service"), server.Client())
	sql := NewSQLEngineService(&config.Config{TransferServiceURL: server.URL}, system, nil)
	svc := NewDevTaskService(devrepository.NewDevTaskRepository(newNotebookBindingTestDB(t)), system)
	svc.SetQueryProtection(sql)
	target, _ := dataprotection.DataItemTargetFromCatalogPath(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "public", "persons"))
	choices := []dataprotection.FieldRoleChoice{{Target: target, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "export", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "hash"}}}
	ctx := WithExecutionAuthContext(t.Context(), facts)
	task, err := svc.CreateDevTask(ctx, "addp_at_current", &models.CreateDevTaskRequest{Name: "export choice", DevType: "query", Content: models.DevTaskContent{"query": "SELECT phone FROM public.persons", "query_type": "sql", "target_locator": "addp://engine/11/path/public/persons?type=table", "export_protection_choices": choices}, ExecutionConfig: models.DevTaskContent{"engine_id": 11}}, 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	denied = true
	if _, err := svc.UpdateDevTask(ctx, "addp_at_current", task.ID, &models.UpdateDevTaskRequest{Name: "must not save"}, 7, 9); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		t.Fatal("name-only update ignored export choice", err)
	}
	stored, err := svc.GetDevTask(task.ID, 7)
	if err != nil || stored.Name != "export choice" || checks != 2 {
		t.Fatal("invalid export definition saved", err)
	}
}
