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

func (f *collectionHandlerFixture) CheckMetaCollection(_ context.Context, actor engineaccess.Actor, input execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error) {
	f.calls++
	f.actor = actor
	return &execution.MetaCollectionAccessObservation{ExecutionID: input.ExecutionID, ExecutionPlanDigest: input.ExecutionPlanDigest, ObservedAt: time.Now()}, nil
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

func TestMetaCollectionRuntimeAndRetiredApprovalRoutes(t *testing.T) {
	const consume = "/api/v1/system/runtime/meta_collection_checks"
	current := testIAMActorContext("tenant")
	collectionPermissions(&current, authorization.PermissionSystemMetaCollectionExecute)
	fixture := &collectionHandlerFixture{}
	router := collectionTestRouter(t, &current, fixture)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		collectionRequest(t, router, method, "/api/v1/system/engines/9/meta_collection_qualifications", "{}", 404)
	}
	collectionRequest(t, router, "GET", "/api/v1/system/engines/9/meta_collection_task_facts/41", "", 404)
	collectionRequest(t, router, "POST", consume, "{}", 403)
	current = testIAMServiceActorContext("tenant", "addp-meta")
	collectionRequest(t, router, "POST", consume, "{}", 403)
	collectionPermissions(&current, authorization.PermissionSystemMetaCollectionExecute)
	request := execution.MetaCollectionAccessRequest{ExecutionID: uuid.NewString(), Attempt: 1, LeaseToken: uuid.NewString(), ExecutionPlanDigest: strings.Repeat("a", 64)}
	raw, _ := json.Marshal(request)
	for _, body := range []string{`{"actor_principal_id":"12"}`, `null`, `{} {}`, strings.Repeat(" ", 8193) + `{}`, strings.Replace(string(raw), `"attempt":1`, `"attempt":1,"attempt":2`, 1), strings.Replace(string(raw), request.LeaseToken, "", 1)} {
		collectionRequest(t, router, "POST", consume, body, 400)
	}
	if fixture.calls != 0 {
		t.Fatal("unqualified or malformed request reached service")
	}
	response := collectionRequest(t, router, "POST", consume, string(raw), 200)
	if response.Header().Get("Cache-Control") != "no-store" || fixture.actor.PrincipalID != 41 || !fixture.actor.TokenExpiresAt.Equal(current.Token.ExpiresAt) {
		t.Fatal("untrusted runtime identity")
	}
	for _, id := range []string{"addp-system", "addp-manager"} {
		current.Client.ClientID = &id
		collectionRequest(t, router, "POST", consume, string(raw), 403)
	}
	if fixture.calls != 1 {
		t.Fatal("another service consumed collection permission")
	}
}
