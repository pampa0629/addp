package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type collectionHandlerFixture struct {
	calls int
	actor engineaccess.Actor
}

func (f *collectionHandlerFixture) GetMetaCollectionTaskFacts(_ context.Context, actor engineaccess.Actor, engineID int64, taskID string) (*execution.MetaScanTaskFacts, error) {
	f.calls++
	f.actor = actor
	return &execution.MetaScanTaskFacts{SchemaVersion: execution.MetaScanTaskFactsSchemaV1, TaskID: taskID, TaskDefinitionDigest: strings.Repeat("a", 64), EngineID: uint(engineID), ScopeKind: "engine", ScanDepth: "basic"}, nil
}
func (f *collectionHandlerFixture) ListMetaCollections(_ context.Context, actor engineaccess.Actor, _ int64, _, _ int) ([]engineaccess.MetaCollectionQualification, int64, error) {
	f.calls++
	f.actor = actor
	return []engineaccess.MetaCollectionQualification{}, 0, nil
}
func (f *collectionHandlerFixture) ApproveMetaCollection(_ context.Context, input engineaccess.ApproveMetaCollectionInput) (*engineaccess.MetaCollectionQualification, error) {
	f.calls++
	f.actor = input.Actor
	return &engineaccess.MetaCollectionQualification{ID: input.CommandID}, nil
}
func (f *collectionHandlerFixture) RevokeMetaCollection(_ context.Context, input engineaccess.RevokeMetaCollectionInput) (*engineaccess.MetaCollectionQualification, error) {
	f.calls++
	f.actor = input.Actor
	return &engineaccess.MetaCollectionQualification{ID: input.ID}, nil
}
func (f *collectionHandlerFixture) CheckMetaCollection(_ context.Context, actor engineaccess.Actor, input execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error) {
	f.calls++
	f.actor = actor
	return &execution.MetaCollectionAccessObservation{ExecutionID: input.ExecutionID, ExecutionPlanDigest: input.ExecutionPlanDigest, QualificationID: uuid.NewString(), ObservedAt: time.Now()}, nil
}

func collectionTestRouter(t *testing.T, current *shared.AuthContext, fixture *collectionHandlerFixture) *gin.Engine {
	t.Helper()
	authenticate, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: current})
	if err != nil {
		t.Fatal(err)
	}
	user, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
	if err != nil {
		t.Fatal(err)
	}
	service, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	if err := RegisterMetaCollectionRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authenticate, UserAccessCredential: user, ServiceCredential: service}, &MetaCollectionHandler{service: fixture}); err != nil {
		t.Fatal(err)
	}
	return router
}

func collectionRequest(t *testing.T, router *gin.Engine, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer fixture")
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != status {
		t.Fatalf("%s %s = %d want %d: %s", method, path, response.Code, status, response.Body.String())
	}
	return response
}

func collectionPermissions(current *shared.AuthContext, permissions ...string) {
	sort.Strings(permissions)
	current.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.collection", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: shared.AssignmentScope{Type: current.Context.Type, TenantID: current.Context.TenantID}, Permissions: permissions}}
}

func TestMetaCollectionRoutesSeparateHumanApprovalAndMachineConsumption(t *testing.T) {
	const base = "/api/v1/system/engines/9/meta_collection_qualifications"
	const consume = "/api/v1/system/runtime/meta_collection_checks"
	permissions := []string{authorization.PermissionSystemMetaCollectionQualificationRead, authorization.PermissionSystemMetaCollectionQualificationApprove, authorization.PermissionSystemMetaCollectionQualificationRevoke, authorization.PermissionSystemMetaCollectionQualificationExecute}
	for _, current := range []shared.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-meta")} {
		fixture := &collectionHandlerFixture{}
		router := collectionTestRouter(t, &current, fixture)
		collectionRequest(t, router, "GET", base, "", 403)
		collectionRequest(t, router, "POST", base, "{}", 403)
		collectionRequest(t, router, "DELETE", base+"/"+uuid.NewString(), "{}", 403)
		collectionRequest(t, router, "GET", "/api/v1/system/engines/9/meta_collection_task_facts/41", "", 403)
		if fixture.calls != 0 {
			t.Fatal("unqualified actor reached service")
		}
	}
	current := testIAMActorContext("tenant")
	collectionPermissions(&current, permissions...)
	fixture := &collectionHandlerFixture{}
	router := collectionTestRouter(t, &current, fixture)
	collectionRequest(t, router, "POST", consume, "{}", 403)
	for _, body := range []string{`{"tenant_id":"7"}`, `{"purpose":"a","purpose":"b"}`, `null`, `{} {}`, strings.Repeat(" ", 8193) + `{}`} {
		collectionRequest(t, router, "POST", base, body, 400)
	}
	for _, suffix := range []string{"?", "?tenant_id=4", "?page=1&page=2", "?page=%zz"} {
		collectionRequest(t, router, "GET", base+suffix, "", 400)
	}
	collectionRequest(t, router, "GET", base, "{}", 400)
	if fixture.calls != 0 {
		t.Fatal("caller authority or malformed request reached service")
	}
	response := collectionRequest(t, router, "GET", base, "", 200)
	if response.Header().Get("Cache-Control") != "no-store" || fixture.actor.TenantID != 3 || fixture.actor.PrincipalID != 12 || fixture.actor.MembershipID != 4 {
		t.Fatal("untrusted human provenance")
	}
	current = testIAMServiceActorContext("tenant", "addp-meta")
	collectionPermissions(&current, permissions...)
	collectionRequest(t, router, "POST", base, "{}", 403)
	request := execution.MetaCollectionAccessRequest{ExecutionID: uuid.NewString(), Attempt: 1, LeaseToken: uuid.NewString(), ExecutionPlanDigest: strings.Repeat("a", 64)}
	raw, _ := json.Marshal(request)
	response = collectionRequest(t, router, "POST", consume, string(raw), 200)
	if response.Header().Get("Cache-Control") != "no-store" || fixture.actor.PrincipalID != 41 || !fixture.actor.TokenExpiresAt.Equal(current.Token.ExpiresAt) {
		t.Fatal("untrusted machine provenance")
	}
	before := fixture.calls
	for _, clientID := range []string{"addp-system", "addp-manager"} {
		current.Client.ClientID = &clientID
		collectionRequest(t, router, "POST", consume, string(raw), 403)
	}
	current = testIAMServiceActorContext("tenant", "addp-meta")
	collectionRequest(t, router, "POST", consume, string(raw), 403)
	collectionPermissions(&current, permissions...)
	for _, body := range []string{`{"actor_principal_id":"12"}`, strings.Replace(string(raw), `"attempt":1`, `"attempt":1,"attempt":2`, 1), strings.Replace(string(raw), request.LeaseToken, "", 1)} {
		collectionRequest(t, router, "POST", consume, body, 400)
	}
	if fixture.calls != before {
		t.Fatal("unqualified or tampered runtime reached service")
	}
}
