package service

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
)

type roleGateStub struct {
	deny   bool
	checks int
	roles  []string
}

func (g *roleGateStub) RoleOptions(context.Context, int64, dataprotection.ResourceReference, string, []string, time.Time) ([]dataprotection.FieldRoleOptions, error) {
	return []dataprotection.FieldRoleOptions{}, nil
}
func (g *roleGateStub) ValidateRoleChoices(_ context.Context, tenant int64, s *dataprotection.QueryRoleSelection, action string, now time.Time) error {
	g.checks++
	g.roles = s.RoleKeys
	if tenant != 7 || action != "export" || s.Validate(now) != nil {
		return dataprotection.ErrRoleOptionIneligible
	}
	if g.deny {
		return dataprotection.ErrRoleOptionUnavailable
	}
	return nil
}

type roleIssuerFunc func(context.Context, string, commonClient.IssueExecutionAuthorizationRequest) (*commonClient.IssuedExecutionAuthorization, error)

func (f roleIssuerFunc) Issue(ctx context.Context, token string, r commonClient.IssueExecutionAuthorizationRequest) (*commonClient.IssuedExecutionAuthorization, error) {
	return f(ctx, token, r)
}
func TestRoleTaskSaveNameOnlyStartAndRevokedSelection(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	svc := NewTaskService(db, nil, nil)
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"transfer.task.execute"})
	ctx := WithTaskAuthorizationToken(WithProtectionAuthContext(commonExecution.WithUserOrigin(t.Context(), facts), facts), "addp_at_current")
	roleReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/auth/subject-roles":
			roleReads++
			if r.Header.Get("Authorization") != "Bearer addp_at_current" {
				t.Error("used worker role for save")
			}
			json.NewEncoder(w).Encode(authorization.CurrentSubjectRoles{TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: *facts.Context.TenantMembershipID, AuthorizationVersion: facts.Authorization.AuthorizationVersion, RoleKeys: []string{"custom.analytics"}, ObservedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)})
		case "/api/v1/system/runtime/engine-descriptors/1":
			json.NewEncoder(w).Encode(map[string]any{"id": 1, "engine_type": "postgresql"})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	gate := &roleGateStub{}
	issuerCalls := 0
	issuer := roleIssuerFunc(func(_ context.Context, token string, r commonClient.IssueExecutionAuthorizationRequest) (*commonClient.IssuedExecutionAuthorization, error) {
		issuerCalls++
		if token != "addp_at_current" || r.Audience != "transfer" || len(r.Accesses) != 2 || r.Accesses[0].EngineID != "1" || r.Accesses[0].Effects[0] != "read" || r.Accesses[1].EngineID != "2" || r.Accesses[1].Effects[0] != "write" {
			t.Fatal("wrong user or engine effects")
		}
		return &commonClient.IssuedExecutionAuthorization{ID: "81", ExecutionID: r.ExecutionID, Audience: "transfer", TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: *facts.Context.TenantMembershipID, IssuedAuthorizationVersion: facts.Authorization.AuthorizationVersion, SourceType: "user", ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	svc.SetRoleProtection(NewRoleProtectionService(commonClient.NewSystemServiceClient(server.URL, roleTestTokens{}, server.Client()), issuer, gate))
	target, err := dataprotection.DataItemTargetFromCatalogPath(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), plugin.TabularItemPath(1, plugin.EngineCatalogTermSchema, "public", "roads"))
	if err != nil {
		t.Fatal(err)
	}
	config := validTableTransferTaskConfig()
	choice := dataprotection.FieldRoleChoice{Target: target, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "hash"}}
	config["source"].(map[string]any)["protection_choices"] = []dataprotection.FieldRoleChoice{choice}
	task, err := svc.CreateTask(ctx, &models.CreateTaskRequest{Name: "role task", TaskType: "sync", Config: config}, 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	gate.deny = true
	name := "must not save"
	if _, err := svc.UpdateTask(ctx, task.ID, 7, &models.UpdateTaskRequest{Name: &name}); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		t.Fatal("name-only update skipped qualification", err)
	}
	stored, err := svc.GetTask(ctx, task.ID, 7)
	if err != nil || stored.Name != "role task" {
		t.Fatal("invalid revision persisted", err)
	}
	record := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Source: "transfer", TriggerType: "manual"}
	if err := svc.authorizeRoleExecution(ctx, task, record); err == nil || issuerCalls != 0 {
		t.Fatal("revoked choice issued authority")
	}
	gate.deny = false
	if err := svc.authorizeRoleExecution(ctx, task, record); err != nil {
		t.Fatal(err)
	}
	if record.ExecutionAuthorizationID == nil || *record.ExecutionAuthorizationID != 81 || record.TriggeredBy == nil || *record.TriggeredBy != 9 || issuerCalls != 1 || roleReads != 4 {
		t.Fatal("authority or current role binding missing")
	}
	parent := uuid.NewString()
	record.ParentExecutionID = &parent
	if err := svc.authorizeRoleExecution(ctx, task, record); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		t.Fatal("orchestration borrowed user roles", err)
	}
	task.Schedule = "0 */5 * * * *"
	if err := svc.validateRoleTask(ctx, task); err == nil {
		t.Fatal("scheduled role choice accepted")
	}
	// Explicit removal restores a default task without a role service dependency.
	delete(config["source"].(map[string]any), "protection_choices")
	task.Config = config
	if err := svc.validateRoleTask(context.Background(), task); err != nil {
		t.Fatal("default started depending on user role facts", err)
	}
}
func TestTransferRuntimeRolesUseCurrentExecutionAttemptLease(t *testing.T) {
	id, token := uuid.NewString(), uuid.NewString()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request commonClient.ExecutionSubjectRolesRequest
		json.NewDecoder(r.Body).Decode(&request)
		if r.URL.Path != "/api/v1/system/execution-authorizations/81/subject-roles" || request.ExecutionID != id || request.Attempt != 2 || request.LeaseToken != token {
			t.Error("wrong lease binding")
		}
		json.NewEncoder(w).Encode(commonExecution.SubjectRoles{AuthorizationID: "81", ExecutionID: id, Attempt: 2, Owner: "transfer", TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: "13", AuthorizationVersion: "2", RoleKeys: []string{"custom.analytics"}, ObservedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)})
	}))
	defer server.Close()
	engine := &ExecutionEngineService{systemRuntime: commonClient.NewSystemServiceClient(server.URL, roleTestTokens{}, server.Client())}
	config := validTableTransferTaskConfig()
	config["source"].(map[string]any)["protection_choices"] = []dataprotection.FieldRoleChoice{{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "hash"}}}
	by := 9
	aid, principal, member, version := int64(81), int64(9), int64(13), int64(2)
	expiry := time.Now().Add(time.Hour)
	record := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: id, TriggerType: "manual", TriggeredBy: &by, ExecutionAuthorizationID: &aid, ActorPrincipalID: &principal, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, AuthorizationExpiresAt: &expiry}
	resolve := engine.executionRoleResolver(record, config)
	ctx := commonExecution.ContextWithLease(t.Context(), commonExecution.Lease{ExecutionID: id, TenantID: 7, Attempt: 2, Token: token})
	for range 2 {
		selection, err := resolve(ctx)
		if err != nil || len(selection.RoleKeys) != 1 {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("cached roles across batches")
	}
	if _, err := resolve(t.Context()); err == nil {
		t.Fatal("missing lease accepted")
	}
	record.ActorPrincipalID = &member
	if _, err := resolve(ctx); err == nil {
		t.Fatal("wrong actor accepted")
	}
}

type roleTestTokens struct{}

func (roleTestTokens) Token(context.Context, uint) (string, error) {
	return "addp_at_role_service", nil
}
func (roleTestTokens) PlatformToken(context.Context) (string, error) {
	return "addp_at_role_platform", nil
}

type roleExportSourceResolverFunc func(context.Context, string, uint, commonClient.TransferExportSessionReference, string) (*commonClient.ExportExecutionSource, error)

func (f roleExportSourceResolverFunc) ResolveExportExecutionSource(ctx context.Context, owner string, tenant uint, ref commonClient.TransferExportSessionReference, digest string) (*commonClient.ExportExecutionSource, error) {
	return f(ctx, owner, tenant, ref, digest)
}
func TestRoleModuleExportUsesVerifiedSessionUserAndSeparateAuthority(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	repo := commonExecution.NewTaskExecutionRepository(db)
	svc := NewTaskService(db, nil, nil)
	svc.SetExecutionService(NewExecutionService(db, repo))
	id := uuid.NewString()
	req := &models.CreateAdHocExecutionRequest{Name: "role export", Config: commonClient.TransferExecutionConfig{Runtime: commonClient.TransferExecutionRuntime{Boundary: "bounded"}, Load: commonClient.TransferExecutionLoad{Mode: "snapshot"}, Source: commonClient.TransferExecutionEndpoint{Locator: "addp://engine/1/path/public/roads?type=table", DataType: "table", Representation: "native", ProtectionChoices: []dataprotection.FieldRoleChoice{{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "hash"}}}}, Target: commonClient.TransferExecutionEndpoint{ParentLocator: "addp-infra://minio/manager/tenant_7/export/develop/test/?type=directory", Name: "test.csv", DataType: "table", Representation: "encoded", Format: "csv", Policy: map[string]any{"apply_mode": "replace"}}}, ExportSession: &commonClient.TransferExportSessionReference{SessionID: 1, ExecutionID: id}, ExecutionAuthorization: &commonClient.IssuedExecutionAuthorization{ID: "81", ExecutionID: id, Audience: "transfer", TenantID: "7", ActorPrincipalID: "10", TenantMembershipID: "13", IssuedAuthorizationVersion: "2", SourceType: "user", ExpiresAt: time.Now().Add(time.Minute)}}
	calls := 0
	svc.SetExportExecutionSourceResolver(roleExportSourceResolverFunc(func(_ context.Context, owner string, tenant uint, ref commonClient.TransferExportSessionReference, digest string) (*commonClient.ExportExecutionSource, error) {
		calls++
		expected, _ := commonClient.ExportRequestDigest(req)
		if owner != "develop" || tenant != 7 || ref.ExecutionID != id || digest != expected {
			t.Error("provenance lost the exact authority-bound digest")
		}
		return &commonClient.ExportExecutionSource{TenantID: 7, UserID: 9}, nil
	}))
	if _, err := svc.CreateAdHocExecution(t.Context(), req, "develop", 7); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		t.Fatal("module promoted another actor's authority", err)
	}
	req.ExecutionAuthorization.ActorPrincipalID = "9"
	response, err := svc.CreateAdHocExecution(t.Context(), req, "develop", 7)
	if err != nil {
		t.Fatal(err)
	}
	row, err := repo.GetByExecutionID(t.Context(), response.ExecutionID, 7)
	if err != nil || calls != 2 || row.Source != "develop" || row.SourceTaskID != nil || row.ActorPrincipalID == nil || *row.ActorPrincipalID != 9 || row.ExecutionAuthorizationID == nil || *row.ExecutionAuthorizationID != 81 || row.TriggeredBy == nil || *row.TriggeredBy != 9 {
		t.Fatal("verified export actor or authority was lost", err)
	}
}
