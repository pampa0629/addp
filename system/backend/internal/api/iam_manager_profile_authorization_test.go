package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	auth "github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const profileHTTPExecution = "05aa6b01-7947-4301-a5a6-00bb9ebd4300"
const profileHTTPLease = "718a9dcf-75e4-4921-827f-ddb23309689b"

func profileHTTPReadScope(t *testing.T) execution.ManagerProfileReadScope {
	t.Helper()
	set, err := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "C"))
	if err != nil {
		t.Fatal(err)
	}
	return execution.ManagerProfileReadScope{ConfigDigest: strings.Repeat("a", 64), ReadSet: *set}
}

type profileHTTPFixture struct {
	issues, consumes        int
	err                     error
	incomplete, wrongTenant bool
	issued                  iam.IssueManagerProfileAuthorizationInput
	consumed                iam.AuthorizeManagerProfileInput
}

func (f *profileHTTPFixture) Issue(_ context.Context, input iam.IssueManagerProfileAuthorizationInput) (*iam.IssuedManagerProfileAuthorization, error) {
	f.issues++
	f.issued = input
	if f.err != nil {
		return nil, f.err
	}
	if f.incomplete {
		return nil, nil
	}
	set, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "C"))
	tenant := int64(3)
	if f.wrongTenant {
		tenant = 4
	}
	return &iam.IssuedManagerProfileAuthorization{ID: 9, ExecutionID: input.ExecutionID, TenantID: tenant,
		ExpiresAt: time.Now().Add(15 * time.Minute), SourceReadScope: execution.ManagerProfileReadScope{ConfigDigest: strings.Repeat("a", 64), ReadSet: *set}}, nil
}

func (f *profileHTTPFixture) Authorize(_ context.Context, input iam.AuthorizeManagerProfileInput) (time.Time, error) {
	f.consumes++
	f.consumed = input
	if f.err != nil {
		return time.Time{}, f.err
	}
	if f.incomplete {
		return time.Time{}, nil
	}
	return time.Now().UTC(), nil
}

func profileHTTPAuth(service bool) auth.AuthContext {
	current := testIAMActorContext("tenant")
	permissions := []string{"manager.data_item.read", "manager.data_profile.execute"}
	if service {
		current = testIAMServiceActorContext("tenant", "addp-manager")
		permissions = []string{"system.execution_authorization.execute"}
	}
	current.Authorization.RoleAssignments = []auth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.profile", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute),
		Scope: auth.AssignmentScope{Type: "tenant", TenantID: current.Context.TenantID}, Permissions: permissions}}
	return current
}

type profileHTTPResolver struct{ current auth.AuthContext }

func (r profileHTTPResolver) ResolveAuthContext(context.Context, string) (*auth.AuthContext, error) {
	if !r.current.Token.ExpiresAt.After(time.Now()) {
		return nil, commonapi.ErrUnauthorized
	}
	return &r.current, nil
}

func profileHTTPRouter(t *testing.T, current auth.AuthContext, fixture *profileHTTPFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := auth.ValidateAuthContext(current); err != nil {
		t.Fatalf("invalid auth fixture: %v", err)
	}
	fromToken, err := middleware.NewIAMAuthenticationMiddleware(profileHTTPResolver{current: current})
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
	if err := RegisterIAMManagerProfileAuthorizationRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: fromToken, UserAccessCredential: user, ServiceCredential: service}, &IAMManagerProfileAuthorizationHandler{service: fixture}); err != nil {
		t.Fatal(err)
	}
	return router
}

func TestManagerProfileHTTPContracts(t *testing.T) {
	consume, err := json.Marshal(IAMManagerProfileAccessRequest{ExecutionID: profileHTTPExecution, Attempt: 2, LeaseToken: profileHTTPLease, SourceReadScope: profileHTTPReadScope(t)})
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range []bool{false, true} {
		path, body, status := "/api/v1/system/auth/execution-authorizations/manager-profiles", `{"execution_id":"`+profileHTTPExecution+`"}`, 201
		if service {
			path, body, status = "/api/v1/system/execution-authorizations/9/manager-profile-accesses", string(consume), 200
		}
		for _, tc := range []struct {
			name, suffix, body      string
			err                     error
			incomplete, wrongTenant bool
			status, calls           int
		}{
			{"current", "", body, nil, false, false, status, 1},
			{"empty", "", `{}`, nil, false, false, 400, 0},
			{"unknown identity", "", strings.TrimSuffix(body, "}") + `,"tenant_id":9}`, nil, false, false, 400, 0},
			{"query identity", "?tenant_id=9", body, nil, false, false, 400, 0},
			{"multiple JSON", "", body + `{}`, nil, false, false, 400, 0},
			{"oversized", "", `{"execution_id":"` + strings.Repeat("x", 513<<10) + `"}`, nil, false, false, 400, 0},
			{"forbidden", "", body, commonapi.ErrForbidden, false, false, 403, 1},
			{"stale credential", "", body, commonapi.ErrUnauthorized, false, false, 401, 1},
			{"private failure", "", body, errors.New("private database credential"), false, false, 500, 1},
			{"incomplete", "", body, nil, true, false, 500, 1},
		} {
			t.Run(tc.name+path, func(t *testing.T) {
				fixture := &profileHTTPFixture{err: tc.err, incomplete: tc.incomplete}
				request := httptest.NewRequest("POST", path+tc.suffix, strings.NewReader(tc.body))
				request.Header.Set("Authorization", "Bearer addp_at_test")
				response := httptest.NewRecorder()
				profileHTTPRouter(t, profileHTTPAuth(service), fixture).ServeHTTP(response, request)
				if response.Code != tc.status || fixture.issues+fixture.consumes != tc.calls {
					t.Fatalf("status=%d calls=%d/%d body=%s", response.Code, fixture.issues, fixture.consumes, response.Body.String())
				}
				if strings.Contains(response.Body.String(), "private database") || strings.Contains(response.Body.String(), "addp_at_test") {
					t.Fatal("private facts leaked")
				}
				if response.Code == status {
					var result map[string]json.RawMessage
					if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					wantFields := 6
					if service {
						wantFields = 1
						if result["observed_at"] == nil {
							t.Fatal("missing observation")
						}
					}
					if len(result) != wantFields || response.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("expanded or cacheable response")
					}
					if service {
						in := fixture.consumed
						if in.TenantID != 3 || in.ServicePrincipalID != 41 || in.ServiceClientID != "addp-manager" || in.AuthorizationID != 9 || in.Attempt != 2 || in.LeaseToken != uuid.MustParse(profileHTTPLease) {
							t.Fatalf("untrusted input: %+v", in)
						}
					} else if fixture.issued.SourceAccessToken != "addp_at_test" || fixture.issued.ExecutionID != uuid.MustParse(profileHTTPExecution) {
						t.Fatal("issuance did not use current credential")
					}
				}
			})
		}
	}
}

func TestManagerProfileHTTPAdmissionGuards(t *testing.T) {
	consume, _ := json.Marshal(IAMManagerProfileAccessRequest{ExecutionID: profileHTTPExecution, Attempt: 1, LeaseToken: profileHTTPLease, SourceReadScope: profileHTTPReadScope(t)})
	for _, service := range []bool{false, true} {
		path, body := "/api/v1/system/auth/execution-authorizations/manager-profiles", `{"execution_id":"`+profileHTTPExecution+`"}`
		if service {
			path, body = "/api/v1/system/execution-authorizations/9/manager-profile-accesses", string(consume)
		}
		for _, tc := range []struct {
			name   string
			mutate func(*auth.AuthContext)
			bearer string
			status int
		}{
			{"no bearer", func(*auth.AuthContext) {}, "", 401},
			{"missing permission", func(c *auth.AuthContext) { c.Authorization.RoleAssignments = []auth.RoleAssignment{} }, "addp_at_test", 403},
			{"wrong credential", func(c *auth.AuthContext) {
				*c = profileHTTPAuth(!service)
			}, "addp_at_test", 403},
			{"expired", func(c *auth.AuthContext) {
				c.Token.ExpiresAt = time.Now().Add(-time.Second)
				c.Token.IssuedAt = time.Now().Add(-time.Minute)
			}, "addp_at_test", 401},
		} {
			t.Run(tc.name+path, func(t *testing.T) {
				current := profileHTTPAuth(service)
				tc.mutate(&current)
				fixture := &profileHTTPFixture{}
				request := httptest.NewRequest("POST", path, strings.NewReader(body))
				if tc.bearer != "" {
					request.Header.Set("Authorization", "Bearer "+tc.bearer)
				}
				response := httptest.NewRecorder()
				profileHTTPRouter(t, current, fixture).ServeHTTP(response, request)
				if response.Code != tc.status || fixture.issues+fixture.consumes != 0 {
					t.Fatalf("guard status=%d body=%s", response.Code, response.Body.String())
				}
			})
		}
	}
	for _, client := range []string{"addp-meta", "addp-system"} {
		current := profileHTTPAuth(true)
		current.Client.ClientID = &client
		fixture := &profileHTTPFixture{}
		request := httptest.NewRequest("POST", "/api/v1/system/execution-authorizations/9/manager-profile-accesses", strings.NewReader(string(consume)))
		request.Header.Set("Authorization", "Bearer addp_at_test")
		response := httptest.NewRecorder()
		profileHTTPRouter(t, current, fixture).ServeHTTP(response, request)
		if response.Code != 403 || fixture.consumes != 0 {
			t.Fatal("wrong runtime consumed authorization")
		}
	}
}

func TestManagerProfileHTTPInvalidBindings(t *testing.T) {
	scope := profileHTTPReadScope(t)
	for _, tc := range []struct {
		name, id, executionID, lease string
		attempt                      int
		scope                        execution.ManagerProfileReadScope
	}{
		{"noncanonical ID", "09", profileHTTPExecution, profileHTTPLease, 1, scope},
		{"overflow ID", "9223372036854775808", profileHTTPExecution, profileHTTPLease, 1, scope},
		{"zero attempt", "9", profileHTTPExecution, profileHTTPLease, 0, scope},
		{"nil lease", "9", profileHTTPExecution, uuid.Nil.String(), 1, scope},
		{"noncanonical execution", "9", strings.ToUpper(profileHTTPExecution), profileHTTPLease, 1, scope},
		{"missing scope", "9", profileHTTPExecution, profileHTTPLease, 1, execution.ManagerProfileReadScope{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(IAMManagerProfileAccessRequest{ExecutionID: tc.executionID, LeaseToken: tc.lease, Attempt: tc.attempt, SourceReadScope: tc.scope})
			fixture := &profileHTTPFixture{}
			request := httptest.NewRequest("POST", "/api/v1/system/execution-authorizations/"+tc.id+"/manager-profile-accesses", strings.NewReader(string(body)))
			request.Header.Set("Authorization", "Bearer addp_at_test")
			response := httptest.NewRecorder()
			profileHTTPRouter(t, profileHTTPAuth(true), fixture).ServeHTTP(response, request)
			if response.Code != 400 || fixture.consumes != 0 {
				t.Fatalf("invalid binding accepted: %d", response.Code)
			}
		})
	}
	for _, ttl := range []string{"-1", "3601", "9223372036854775808"} {
		fixture := &profileHTTPFixture{}
		request := httptest.NewRequest("POST", "/api/v1/system/auth/execution-authorizations/manager-profiles", strings.NewReader(`{"execution_id":"`+profileHTTPExecution+`","expires_in":`+ttl+`}`))
		request.Header.Set("Authorization", "Bearer addp_at_test")
		response := httptest.NewRecorder()
		profileHTTPRouter(t, profileHTTPAuth(false), fixture).ServeHTTP(response, request)
		if response.Code != 400 || fixture.issues != 0 {
			t.Fatalf("invalid TTL accepted: %s", ttl)
		}
	}
	fixture := &profileHTTPFixture{wrongTenant: true}
	request := httptest.NewRequest("POST", "/api/v1/system/auth/execution-authorizations/manager-profiles", strings.NewReader(`{"execution_id":"`+profileHTTPExecution+`"}`))
	request.Header.Set("Authorization", "Bearer addp_at_test")
	response := httptest.NewRecorder()
	profileHTTPRouter(t, profileHTTPAuth(false), fixture).ServeHTTP(response, request)
	if response.Code != 500 || fixture.issues != 1 {
		t.Fatal("mismatched trusted tenant returned")
	}
}
