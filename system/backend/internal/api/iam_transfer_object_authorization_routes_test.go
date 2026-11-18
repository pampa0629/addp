package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	auth "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type objectHTTPFixture struct {
	scope            execution.TransferObjectReadScope
	issues, consumes int
}

func (f *objectHTTPFixture) Issue(_ context.Context, in iam.IssueTransferObjectAuthorizationInput) (*iam.IssuedTransferObjectAuthorization, error) {
	f.issues++
	return &iam.IssuedTransferObjectAuthorization{ID: 9, ExecutionID: in.ExecutionID, TenantID: 3, SourceReadScope: f.scope, ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (f *objectHTTPFixture) Authorize(_ context.Context, in iam.AuthorizeTransferObjectInput) (*iam.ExecutionSourceReadObservation, error) {
	f.consumes++
	tenant := uint(3)
	return &iam.ExecutionSourceReadObservation{ObservedAt: time.Now(), Engine: &commonmodels.Engine{ID: 12, TenantID: &tenant, Version: 5, EngineType: "rustfs", LifecycleState: "active", ConnectionInfo: commonmodels.ConnectionInfo{"endpoint": "object.invalid"}}}, nil
}

func TestTransferObjectHTTPSeparatesUserIssueAndFixedRuntime(t *testing.T) {
	binding, err := authorizationfacts.NativeCandidateBinding(plugin.ObjectItemPath(12, "demo", "orders.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	scope := execution.TransferObjectReadScope{ConfigDigest: strings.Repeat("a", 64), ObjectSource: execution.TransferObjectSource{EngineVersion: "5", Binding: binding}, TargetEngineID: 13}
	for _, tc := range []struct {
		name        string
		service     bool
		permissions []string
		client      string
		path        string
		extra       bool
		want        int
	}{
		{"ordinary issue", false, []string{"transfer.task.execute", "manager.data_item.read", "develop.data_write.execute"}, "", "/auth/execution-authorizations/transfer-objects", false, 201},
		{"missing write", false, []string{"transfer.task.execute", "manager.data_item.read"}, "", "/auth/execution-authorizations/transfer-objects", false, 403},
		{"Manager function is insufficient", false, []string{"manager.derived_artifact.create", "manager.data_item.read", "develop.data_write.execute"}, "", "/auth/execution-authorizations/transfer-objects", false, 403},
		{"scope self-report rejected", false, []string{"transfer.task.execute", "manager.data_item.read", "develop.data_write.execute"}, "", "/auth/execution-authorizations/transfer-objects", true, 400},
		{"fixed runtime", true, []string{"system.execution_authorization.execute"}, "addp-transfer", "/execution-authorizations/9/transfer-object-accesses", false, 200},
		{"other runtime", true, []string{"system.execution_authorization.execute"}, "addp-manager", "/execution-authorizations/9/transfer-object-accesses", false, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := testIAMActorContext("tenant")
			if tc.service {
				current = testIAMServiceActorContext("tenant", tc.client)
			}
			sort.Strings(tc.permissions)
			current.Authorization.RoleAssignments = []auth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.object_import", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: auth.AssignmentScope{Type: "tenant", TenantID: current.Context.TenantID}, Permissions: tc.permissions}}
			if err := auth.ValidateAuthContext(current); err != nil {
				t.Fatal(err)
			}
			authentication, err := middleware.NewIAMAuthenticationMiddleware(model3DHTTPResolver{current})
			if err != nil {
				t.Fatal(err)
			}
			user, _ := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
			runtime, _ := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
			fixture := &objectHTTPFixture{scope: scope}
			router := gin.New()
			if err := registerTransferObjectAuthorizationRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, UserAccessCredential: user, ServiceCredential: runtime}, fixture); err != nil {
				t.Fatal(err)
			}
			body := `{"execution_id":"` + model3DHTTPExecution + `"}`
			if tc.extra {
				body = `{"execution_id":"` + model3DHTTPExecution + `","source_read_scope":{}}`
			}
			if tc.service {
				encoded, _ := json.Marshal(client.TransferObjectAccessRequest{ExecutionID: model3DHTTPExecution, Attempt: 2, LeaseToken: model3DHTTPLease, SourceReadScope: scope})
				body = string(encoded)
			}
			request := httptest.NewRequest("POST", "/api/v1/system"+tc.path, strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer fixture")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
			}
			if tc.want >= 400 && fixture.issues+fixture.consumes != 0 {
				t.Fatal("rejected boundary reached the service")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("source decision is cacheable")
			}
		})
	}
}
