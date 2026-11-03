package api

import (
	"context"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type uploadGrantHandlerFixture struct {
	calls  int
	actor  engineaccess.UploadRuntimeActor
	id, fp string
}

func (f *uploadGrantHandlerFixture) IssueInitialUploadGrant(_ context.Context, a engineaccess.UploadRuntimeActor, id, fp string) (*shared.UploadInitialResult, error) {
	f.calls++
	f.actor = a
	f.id = id
	f.fp = fp
	return &shared.UploadInitialResult{TenantID: a.TenantID, UploadID: id, ItemFingerprint: fp, Outcome: "denied", Reason: "not_new"}, nil
}
func TestUploadInitialGrantFixedRuntimeHasNoCallerSuppliedEvidence(t *testing.T) {
	ac := testIAMServiceActorContext("tenant", "addp-manager")
	clientID := "addp-manager"
	ac.Client.ClientID = &clientID
	ac.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.manager_runtime", SourceType: "bootstrap", ValidFrom: time.Now().Add(-time.Minute), Scope: shared.AssignmentScope{Type: "tenant", TenantID: ac.Context.TenantID}, Permissions: []string{authorization.PermissionSystemEngineAccessInitialGrantExecute}}}
	authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &ac})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &uploadGrantHandlerFixture{}
	r := gin.New()
	if err := RegisterUploadInitialGrantRoute(r.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, ServiceCredential: credential}, &UploadInitialGrantHandler{service: fixture}); err != nil {
		t.Fatal(err)
	}
	id, fp := uuid.NewString(), strings.Repeat("a", 64)
	path := "/api/v1/system/runtime/initial-upload-grants/" + id + "/items/" + fp
	request := func(url, body string, want int) {
		t.Helper()
		req := httptest.NewRequest("POST", url, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer fixture")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
		}
	}
	request(path, "", 200)
	if fixture.calls != 1 || fixture.id != id || fixture.fp != fp || fixture.actor.ClientID != "addp-manager" || fixture.actor.TenantID != 3 {
		t.Fatalf("actor=%+v", fixture)
	}
	request(path+"?tenant_id=9", "", 400)
	request(path, `{"recipient_id":"9","created":true}`, 400)
	request(strings.TrimSuffix(path, fp)+"bad", "", 400)
	clientID = "addp-transfer"
	request(path, "", 403)
	clientID = "addp-manager"
	ac.Authorization.RoleAssignments = []shared.RoleAssignment{}
	request(path, "", 403)
	if fixture.calls != 1 {
		t.Fatal("invalid request reached signing service")
	}
}
