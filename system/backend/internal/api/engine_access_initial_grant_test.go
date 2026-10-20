package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type initialGrantHandlerFixture struct {
	calls int
	actor engineaccess.ProductionRuntimeActor
	id    string
	err   error
}

func (f *initialGrantHandlerFixture) IssueInitialTransferGrant(_ context.Context, actor engineaccess.ProductionRuntimeActor, id string) (*shared.InitialProductionResult, error) {
	f.calls++
	f.actor, f.id = actor, id
	return &shared.InitialProductionResult{SchemaVersion: shared.InitialProductionResultSchema, TenantID: actor.TenantID,
		ProducerOwner: "transfer", ExecutionID: uuid.MustParse(id), Outcome: "denied", RecordedAt: time.Now(), DenialCode: "source_access_denied"}, f.err
}

func (f *initialGrantHandlerFixture) ResolveInitialTransferGrant(_ context.Context, actor engineaccess.ProductionRuntimeActor, id string) (*shared.InitialProductionLookup, error) {
	f.calls++
	f.actor, f.id = actor, id
	return &shared.InitialProductionLookup{}, f.err
}

func TestInitialProductionGrantRoutesRequireFixedRuntimeAndNoCallerEvidence(t *testing.T) {
	ac := testIAMServiceActorContext("tenant", "addp-transfer")
	clientID := "addp-transfer"
	ac.Client.ClientID = &clientID
	ac.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.transfer_runtime", SourceType: "bootstrap", ValidFrom: time.Now().Add(-time.Minute),
		Scope: shared.AssignmentScope{Type: "tenant", TenantID: ac.Context.TenantID}, Permissions: []string{authorization.PermissionSystemEngineAccessInitialGrantExecute}}}
	authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &ac})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &initialGrantHandlerFixture{}
	router := gin.New()
	if err := RegisterEngineAccessInitialGrantRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, ServiceCredential: credential}, &EngineAccessInitialGrantHandler{service: fixture}); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	path := "/api/v1/system/runtime/initial_production_grants/transfer/" + id
	request := func(method, url, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, url, nil)
		if body != "" {
			r = httptest.NewRequest(method, url, strings.NewReader(body))
		}
		r.Header.Set("Authorization", "Bearer fixture")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, url, w.Code, status, w.Body.String())
		}
		return w
	}
	request("GET", path, "", 200)
	request("POST", path, "", 200)
	if fixture.calls != 2 || fixture.id != id || fixture.actor.ClientID != "addp-transfer" || fixture.actor.TenantID != 3 || fixture.actor.PrincipalID != 41 || fixture.actor.MembershipID != 4 || fixture.actor.AuthorizationVersion != 1 || !fixture.actor.TokenExpiresAt.Equal(ac.Token.ExpiresAt) {
		t.Fatalf("untrusted runtime input: %+v", fixture)
	}
	for _, method := range []string{"GET", "POST"} {
		request(method, path+"?tenant_id=9", "", 400)
		request(method, path, `{"created_and_committed":true,"recipient_id":"9"}`, 400)
		request(method, strings.TrimSuffix(path, id)+"not-a-uuid", "", 400)
		request(method, strings.TrimSuffix(path, id)+uuid.Nil.String(), "", 400)
	}
	if fixture.calls != 2 {
		t.Fatal("caller-supplied evidence reached service")
	}
	clientID = "addp-catalog"
	request("POST", path, "", 403)
	clientID = "addp-transfer"
	ac.Authorization.RoleAssignments = []shared.RoleAssignment{}
	request("GET", path, "", 403)
	request("POST", path, "", 403)
	if fixture.calls != 2 {
		t.Fatal("unqualified runtime reached service")
	}
}

func TestInitialProductionGrantDependencyFailureIsSafePendingNotDenial(t *testing.T) {
	fixture := &initialGrantHandlerFixture{err: errors.New("database password=secret dependency unavailable")}
	h := &EngineAccessInitialGrantHandler{service: fixture}
	router := gin.New()
	router.POST("/:execution_id", func(c *gin.Context) {
		ac := testIAMServiceActorContext("tenant", "addp-transfer")
		// Use the same context adapter as the real authentication middleware.
		authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &ac})
		if err != nil {
			t.Fatal(err)
		}
		authentication(c)
		if !c.IsAborted() {
			h.Issue(c)
		}
	})
	r := httptest.NewRequest("POST", "/"+uuid.NewString(), nil)
	r.Header.Set("Authorization", "Bearer fixture")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "secret") || !strings.Contains(w.Body.String(), `"error_code":"initial_production_pending"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe pending response=%d %s", w.Code, w.Body.String())
	}
}
