package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type developFileHTTPFixture struct {
	issued   *client.IssuedDevelopFileAuthorization
	calls    int
	input    iam.CheckDevelopFileInput
	issue    iam.IssueDevelopFileAuthorizationInput
	approval iam.ApproveDevelopFileCreateInput
	err      error
}

func (f *developFileHTTPFixture) ApproveCreate(_ context.Context, input iam.ApproveDevelopFileCreateInput) (*iam.DevelopFileCreateApprovalReceipt, error) {
	f.calls++
	f.approval = input
	return &iam.DevelopFileCreateApprovalReceipt{ApprovalID: "ca5846c6-1fbb-40ae-bfda-7ed0e7be08f4", ApprovedAt: time.Now().Add(-time.Second), ExpiresAt: input.ExpiresAt}, f.err
}

func (f *developFileHTTPFixture) Issue(_ context.Context, input iam.IssueDevelopFileAuthorizationInput) (*client.IssuedDevelopFileAuthorization, error) {
	f.calls++
	f.issue = input
	if f.err != nil {
		return nil, f.err
	}
	return f.issued.Clone(), nil
}
func (f *developFileHTTPFixture) Check(_ context.Context, input iam.CheckDevelopFileInput) (*client.DevelopFileAdmissionObservation, *client.DevelopFileSourceAccessObservation, error) {
	f.calls++
	f.input = input
	if f.err != nil {
		return nil, nil, f.err
	}
	if input.Source == nil {
		return &client.DevelopFileAdmissionObservation{AuthorizationID: "9", ExecutionID: input.ExecutionID.String(), ExpectedAttempt: 1, ScopeDigest: input.ScopeDigest, State: "ready", ObservedAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(time.Minute)}, nil, nil
	}
	tenant := uint(input.TenantID)
	return nil, &client.DevelopFileSourceAccessObservation{AuthorizationID: "9", ExecutionID: input.ExecutionID.String(), Attempt: 1, ScopeDigest: input.ScopeDigest, Source: *input.Source, ObservedAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(time.Minute), BusinessDeadline: time.Now().Add(2 * time.Minute), Engine: &models.Engine{ID: 11, TenantID: &tenant, EngineType: "nfs", Version: 2, LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"server": "fixture-only"}}}, nil
}

type developFileHTTPResolver struct{ current authorization.AuthContext }

func (r developFileHTTPResolver) ResolveAuthContext(context.Context, string) (*authorization.AuthContext, error) {
	return &r.current, nil
}
func developFileHTTPAuth(service bool) authorization.AuthContext {
	a := profileHTTPAuth(false)
	a.Authorization.RoleAssignments[0].Permissions = []string{"develop.data_read.execute", "develop.task.execute"}
	if service {
		a = profileHTTPAuth(true)
		id := "addp-develop"
		a.Client.ClientID = &id
	}
	return a
}
func developFileHTTPRouter(t *testing.T, current authorization.AuthContext, f *developFileHTTPFixture) *gin.Engine {
	t.Helper()
	if err := authorization.ValidateAuthContext(current); err != nil {
		t.Fatal("invalid authorization fixture", err)
	}
	auth, err := middleware.NewIAMAuthenticationMiddleware(developFileHTTPResolver{current})
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
	r := gin.New()
	if err := RegisterIAMDevelopFileAuthorizationRoutes(r.Group("/api/v1/system"), &IAMRuntime{Authentication: auth, UserAccessCredential: user, ServiceCredential: service}, &IAMDevelopFileAuthorizationHandler{service: f}); err != nil {
		t.Fatal(err)
	}
	return r
}
func developFileHTTPIssued(t *testing.T) (execution.DevelopFileIntent, *client.IssuedDevelopFileAuthorization) {
	t.Helper()
	intent, err := execution.NewDevelopFileIntent(json.RawMessage(`{"value":9007199254740993}`), 1, 45, []execution.DevelopFileSourceIntent{{NodeID: "load", Target: plugin.FileItemPath(11, "sources/A.gpkg")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := execution.NewDevelopBoundFileScope(*intent, []execution.DevelopFileSourceReference{{Target: intent.Sources[0].Target, ItemFingerprint: strings.Repeat("a", 64), BoundaryHash: strings.Repeat("b", 64), EngineVersion: "2"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := scope.Digest()
	auth := developFileHTTPAuth(false)
	issued := &client.IssuedDevelopFileAuthorization{Scope: *scope, ScopeDigest: digest, Authorization: client.IssuedExecutionAuthorization{ID: "9", ExecutionID: profileHTTPExecution, Audience: "develop", TenantID: *auth.Context.TenantID, ActorPrincipalID: auth.Principal.ID, TenantMembershipID: *auth.Context.TenantMembershipID, IssuedAuthorizationVersion: auth.Authorization.AuthorizationVersion, SourceType: "user", ExpiresAt: time.Now().Add(15 * time.Minute), Accesses: []client.ExecutionEngineAccessScope{{EngineID: "11", Effects: []string{"read"}}}}}
	return *intent, issued
}

func TestDevelopFileHTTPGuardsAndStrictInputs(t *testing.T) {
	intent, issued := developFileHTTPIssued(t)
	digest, _ := intent.Digest()
	issue := `{"execution_id":"` + profileHTTPExecution + `","intent_digest":"` + digest + `"}`
	admit := `{"execution_id":"` + profileHTTPExecution + `","expected_attempt":1,"scope_digest":"` + issued.ScopeDigest + `"}`
	for _, tc := range []struct {
		name, path, body string
		service          bool
		status, calls    int
		mutate           func(*authorization.AuthContext)
		err              error
	}{
		{"issue", "/auth/execution-authorizations/develop-geopackage-workflows", issue, false, 201, 1, nil, nil},
		{"admit", "/execution-authorizations/9/develop-geopackage-admission-checks", admit, true, 200, 1, nil, nil},
		{"unavailable", "/execution-authorizations/9/develop-geopackage-admission-checks", admit, true, 503, 1, nil, iam.ErrDevelopFileDependencyUnavailable},
		{"unknown", "/auth/execution-authorizations/develop-geopackage-workflows", strings.TrimSuffix(issue, "}") + `,"tenant_id":"4"}`, false, 400, 0, nil, nil},
		{"duplicate", "/auth/execution-authorizations/develop-geopackage-workflows", strings.TrimSuffix(issue, "}") + `,"execution_id":"` + profileHTTPExecution + `"}`, false, 400, 0, nil, nil},
		{"query", "/auth/execution-authorizations/develop-geopackage-workflows?source=x", issue, false, 400, 0, nil, nil},
		{"user-cannot-consume", "/execution-authorizations/9/develop-geopackage-admission-checks", admit, false, 403, 0, nil, nil},
		{"service-cannot-issue", "/auth/execution-authorizations/develop-geopackage-workflows", issue, true, 403, 0, nil, nil},
		{"wrong-client", "/execution-authorizations/9/develop-geopackage-admission-checks", admit, true, 403, 0, func(a *authorization.AuthContext) { id := "addp-manager"; a.Client.ClientID = &id }, nil},
		{"missing-function", "/execution-authorizations/9/develop-geopackage-admission-checks", admit, true, 403, 0, func(a *authorization.AuthContext) { a.Authorization.RoleAssignments = []authorization.RoleAssignment{} }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &developFileHTTPFixture{issued: issued, err: tc.err}
			current := developFileHTTPAuth(tc.service)
			if tc.mutate != nil {
				tc.mutate(&current)
			}
			r := developFileHTTPRouter(t, current, f)
			req := httptest.NewRequest("POST", "/api/v1/system"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer addp_at_test")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status || f.calls != tc.calls {
				t.Fatal(w.Code, f.calls, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "addp_at_") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestDevelopFileHTTPCreateApprovalRequiresHumanPermission(t *testing.T) {
	const path = "/api/v1/system/execution-authorizations/9/develop-geopackage-create-approvals"
	body := `{"scope_digest":"` + strings.Repeat("a", 64) + `","expires_at":"2030-01-01T00:00:00Z"}`
	for _, tc := range []struct {
		name, body         string
		service, permitted bool
		status, calls      int
	}{
		{"human", body, false, true, 201, 1},
		{"missing permission", body, false, false, 403, 0},
		{"machine", body, true, true, 403, 0},
		{"invented target", strings.TrimSuffix(body, "}") + `,"target":"other.gpkg"}`, false, true, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := developFileHTTPAuth(tc.service)
			if tc.permitted {
				current.Authorization.RoleAssignments[0].Permissions = []string{"system.develop_file_create_approval.create"}
			}
			fixture := &developFileHTTPFixture{}
			router := developFileHTTPRouter(t, current, fixture)
			req := httptest.NewRequest("POST", path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer addp_at_test")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status || fixture.calls != tc.calls || strings.Contains(w.Body.String(), "addp_at_") {
				t.Fatal(w.Code, fixture.calls, w.Body.String())
			}
			if tc.calls == 1 && (fixture.approval.AuthorizationID != 9 || fixture.approval.ScopeDigest != strings.Repeat("a", 64) || fixture.approval.ExpiresAt.IsZero()) {
				t.Fatal("approval binding lost")
			}
		})
	}
}

func TestDevelopFileSDKConsumesFormalSystemRouteShapes(t *testing.T) {
	intent, issued := developFileHTTPIssued(t)
	fixture := &developFileHTTPFixture{issued: issued}
	userServer := httptest.NewServer(developFileHTTPRouter(t, developFileHTTPAuth(false), fixture))
	defer userServer.Close()
	value, err := client.NewSystemExecutionAuthorizationClient(userServer.URL, userServer.Client()).IssueDevelopFile(t.Context(), "addp_at_user", profileHTTPExecution, intent)
	if err != nil || value == nil || fixture.issue.SourceAccessToken != "addp_at_user" {
		t.Fatal(value, err)
	}
	serviceServer := httptest.NewServer(developFileHTTPRouter(t, developFileHTTPAuth(true), fixture))
	defer serviceServer.Close()
	tenant, _ := strconv.ParseUint(issued.Authorization.TenantID, 10, 64)
	system := client.NewSystemServiceClient(serviceServer.URL, developFileHTTPToken{}, serviceServer.Client()).WithTenantID(uint(tenant))
	admission, err := system.CheckDevelopFileAdmission(t.Context(), *value, client.DevelopFileAdmissionRequest{ExecutionID: profileHTTPExecution, ExpectedAttempt: 1, ScopeDigest: value.ScopeDigest})
	if err != nil || admission.State != "ready" {
		t.Fatal(admission, err)
	}
	access, err := system.CheckDevelopFileSourceAccess(t.Context(), *value, client.DevelopFileSourceAccessRequest{ExecutionID: profileHTTPExecution, Attempt: 1, LeaseToken: profileHTTPLease, ScopeDigest: value.ScopeDigest, Source: value.Scope.Sources[0].Target})
	if err != nil || access.Engine == nil || access.BusinessDeadline.IsZero() || access.ExpiresAt.After(access.BusinessDeadline) || fixture.input.ServiceAccessToken != "addp_at_service" {
		t.Fatal(access, err)
	}
}

type developFileHTTPToken struct{}

func (developFileHTTPToken) Token(context.Context, uint) (string, error) {
	return "addp_at_service", nil
}
func (developFileHTTPToken) PlatformToken(context.Context) (string, error) { return "", nil }
