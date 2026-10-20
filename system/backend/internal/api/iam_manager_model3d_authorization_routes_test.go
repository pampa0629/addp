package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	auth "github.com/addp/common/authorization"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const model3DHTTPExecution = "05aa6b01-7947-4301-a5a6-00bb9ebd4300"
const model3DHTTPLease = "718a9dcf-75e4-4921-827f-ddb23309689b"
const model3DHTTPIssuePath = "/api/v1/system/auth/execution-authorizations/manager-model3d-glb"
const model3DHTTPConsumePath = "/api/v1/system/execution-authorizations/9/manager-model3d-glb-accesses"

func model3DHTTPScope(t *testing.T) execution.ManagerModel3DReadScope {
	t.Helper()
	composition, err := authorizationfacts.NewFileCompositionBoundary("3ds", []authorizationfacts.FileCompositionRef{
		{Target: plugin.FileItemPath(12, "models/scene.3ds"), Role: "model", Primary: true, Required: true},
		{Target: plugin.FileItemPath(12, "models/Texture.PNG"), Role: "texture", Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return execution.ManagerModel3DReadScope{ConfigDigest: strings.Repeat("a", 64), OwnerTaskID: "10", OwnerTaskVersion: "7", EngineVersion: "5", FileComposition: *composition}
}

type model3DHTTPFixture struct {
	t                        *testing.T
	issues, consumes         int
	err                      error
	nilIssue, nilObservation bool
	issued                   iam.IssueManagerModel3DAuthorizationInput
	consumed                 iam.AuthorizeManagerModel3DInput
	mutateIssue              func(*iam.IssuedManagerModel3DAuthorization)
	mutateObservation        func(*iam.ManagerModel3DAccessObservation)
}

func (f *model3DHTTPFixture) Issue(_ context.Context, input iam.IssueManagerModel3DAuthorizationInput) (*iam.IssuedManagerModel3DAuthorization, error) {
	f.issues++
	f.issued = input
	if f.err != nil {
		return nil, f.err
	}
	if f.nilIssue {
		return nil, nil
	}
	result := &iam.IssuedManagerModel3DAuthorization{ID: 9, ExecutionID: input.ExecutionID, TenantID: 3, SourceReadScope: model3DHTTPScope(f.t), ExpiresAt: time.Now().Add(15 * time.Minute)}
	if f.mutateIssue != nil {
		f.mutateIssue(result)
	}
	return result, nil
}
func (f *model3DHTTPFixture) Authorize(_ context.Context, input iam.AuthorizeManagerModel3DInput) (*iam.ManagerModel3DAccessObservation, error) {
	f.consumes++
	f.consumed = input
	if f.err != nil {
		return nil, f.err
	}
	if f.nilObservation {
		return nil, nil
	}
	tenant := uint(3)
	result := &iam.ManagerModel3DAccessObservation{ObservedAt: time.Now().UTC(), Engine: &commonmodels.Engine{ID: 12, TenantID: &tenant, Version: 5, EngineType: "ftp", LifecycleState: "active", ConnectionInfo: commonmodels.ConnectionInfo{"host": "ftp.current.fixture", "password": "connection-only-secret"}}}
	if f.mutateObservation != nil {
		f.mutateObservation(result)
	}
	return result, nil
}

func model3DHTTPAuth(service bool) auth.AuthContext {
	current := testIAMActorContext("tenant")
	permissions := []string{"manager.data_item.read", "manager.derived_artifact.create"}
	if service {
		current = testIAMServiceActorContext("tenant", "addp-manager")
		permissions = []string{"system.execution_authorization.execute"}
	}
	current.Authorization.RoleAssignments = []auth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.model3d", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: auth.AssignmentScope{Type: "tenant", TenantID: current.Context.TenantID}, Permissions: permissions}}
	return current
}

type model3DHTTPResolver struct{ current auth.AuthContext }

func (r model3DHTTPResolver) ResolveAuthContext(context.Context, string) (*auth.AuthContext, error) {
	if !r.current.Token.ExpiresAt.After(time.Now()) {
		return nil, commonapi.ErrUnauthorized
	}
	return &r.current, nil
}
func model3DHTTPRouter(t *testing.T, current auth.AuthContext, fixture *model3DHTTPFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := auth.ValidateAuthContext(current); err != nil {
		t.Fatalf("invalid auth fixture: %v", err)
	}
	authentication, err := middleware.NewIAMAuthenticationMiddleware(model3DHTTPResolver{current})
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
	if err := RegisterIAMManagerModel3DAuthorizationRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, UserAccessCredential: user, ServiceCredential: service}, &IAMManagerModel3DAuthorizationHandler{service: fixture}); err != nil {
		t.Fatal(err)
	}
	return router
}
func model3DHTTPBody(t *testing.T, service bool) string {
	t.Helper()
	if !service {
		return `{"execution_id":"` + model3DHTTPExecution + `","expires_in":120}`
	}
	raw, err := json.Marshal(commonclient.ManagerModel3DAccessRequest{ExecutionID: model3DHTTPExecution, Attempt: 2, LeaseToken: model3DHTTPLease, SourceReadScope: model3DHTTPScope(t)})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func model3DHTTPRequest(t *testing.T, current auth.AuthContext, fixture *model3DHTTPFixture, path, body, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("POST", path, strings.NewReader(body))
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	model3DHTTPRouter(t, current, fixture).ServeHTTP(response, request)
	return response
}

func TestManagerModel3DHTTPUsesSDKContractsAndCurrentActors(t *testing.T) {
	for _, kind := range []string{"first-party issue", "OAuth API issue", "manager service consume"} {
		t.Run(kind, func(t *testing.T) {
			service := kind == "manager service consume"
			current := model3DHTTPAuth(service)
			if kind == "OAuth API issue" {
				current.Token.Type = middleware.IAMTokenTypeOAuthAccess
				current.Client.ScopeMode = "restricted"
				current.Client.Scopes = []string{"addp.api"}
			}
			path, status := model3DHTTPIssuePath, 201
			if service {
				path, status = model3DHTTPConsumePath, 200
			}
			fixture := &model3DHTTPFixture{t: t}
			response := model3DHTTPRequest(t, current, fixture, path, model3DHTTPBody(t, service), "addp_at_current")
			if response.Code != status || fixture.issues+fixture.consumes != 1 || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(response.Body.Bytes(), &fields) != nil {
				t.Fatal("invalid response")
			}
			if service {
				var result commonclient.ManagerModel3DAccessObservation
				if json.Unmarshal(response.Body.Bytes(), &result) != nil || len(fields) != 2 || result.ObservedAt.IsZero() || result.Engine == nil || result.Engine.Version != 5 || result.Engine.ConnectionInfo["host"] != "ftp.current.fixture" {
					t.Fatal("SDK observation lost current engine")
				}
				in := fixture.consumed
				if in.AuthorizationID != 9 || in.ExecutionID != uuid.MustParse(model3DHTTPExecution) || in.Attempt != 2 || in.LeaseToken != uuid.MustParse(model3DHTTPLease) || in.TenantID != 3 || in.ServicePrincipalID != 41 || in.ServiceClientID != "addp-manager" || !reflect.DeepEqual(in.SourceReadScope, model3DHTTPScope(t)) {
					t.Fatal("untrusted consume inputs")
				}
			} else {
				var result commonclient.IssuedManagerModel3DAuthorization
				if json.Unmarshal(response.Body.Bytes(), &result) != nil || len(fields) != 6 || !result.Matches(3, model3DHTTPScope(t)) || result.ExecutionID != model3DHTTPExecution {
					t.Fatal("SDK issuance receipt mismatch")
				}
				if strings.Contains(response.Body.String(), "connection_info") || strings.Contains(response.Body.String(), "password") {
					t.Fatal("issuance exposed connection")
				}
				if fixture.issued.SourceAccessToken != "addp_at_current" || fixture.issued.ExecutionID != uuid.MustParse(model3DHTTPExecution) || fixture.issued.ExpiresIn != 120*time.Second {
					t.Fatal("issuance lost exact current bearer or TTL")
				}
			}
		})
	}
}

func TestManagerModel3DHTTPRejectsAmbiguousRequestsBeforeDomain(t *testing.T) {
	for _, service := range []bool{false, true} {
		body, path := model3DHTTPBody(t, service), model3DHTTPIssuePath
		if service {
			path = model3DHTTPConsumePath
		}
		cases := []struct{ name, body, suffix string }{
			{"empty", "", ""}, {"empty object", "{}", ""}, {"null", "null", ""}, {"array", "[]", ""},
			{"unknown identity", strings.TrimSuffix(body, "}") + `,"tenant_id":3}`, ""},
			{"duplicate execution", strings.TrimSuffix(body, "}") + `,"execution_id":"` + model3DHTTPExecution + `"}`, ""},
			{"escaped duplicate", strings.TrimSuffix(body, "}") + `,"\u0065xecution_id":"` + model3DHTTPExecution + `"}`, ""},
			{"case alias", strings.Replace(body, `"execution_id"`, `"Execution_ID"`, 1), ""},
			{"multiple JSON", body + "{}", ""}, {"query", body, "?actor_id=12"}, {"empty query", body, "?"},
			{"oversized", `{"execution_id":"` + strings.Repeat("x", 513<<10) + `"}`, ""},
			{"invalid UTF8", `{"execution_id":"` + string([]byte{0xff}) + `"}`, ""},
			{"noncanonical UUID", strings.Replace(body, model3DHTTPExecution, strings.ToUpper(model3DHTTPExecution), 1), ""},
			{"zero UUID", strings.Replace(body, model3DHTTPExecution, uuid.Nil.String(), 1), ""},
		}
		if service {
			cases = append(cases, struct{ name, body, suffix string }{"duplicate scope field", strings.Replace(body, `"config_digest":`, `"config_digest":"`+strings.Repeat("b", 64)+`","config_digest":`, 1), ""})
			cases = append(cases, struct{ name, body, suffix string }{"unknown scope field", strings.Replace(body, `"config_digest":`, `"actor_id":"12","config_digest":`, 1), ""})
			for _, value := range []string{"0", "-1", "2.5", "9223372036854775808", "null"} {
				cases = append(cases, struct{ name, body, suffix string }{"bad attempt " + value, strings.Replace(body, `"attempt":2`, `"attempt":`+value, 1), ""})
			}
			cases = append(cases, struct{ name, body, suffix string }{"zero lease", strings.Replace(body, model3DHTTPLease, uuid.Nil.String(), 1), ""})
		} else {
			for _, value := range []string{"-1", "3601", "1.5", "9223372036854775808", "null"} {
				cases = append(cases, struct{ name, body, suffix string }{"bad TTL " + value, strings.Replace(body, `"expires_in":120`, `"expires_in":`+value, 1), ""})
			}
		}
		for _, tc := range cases {
			t.Run(tc.name+path, func(t *testing.T) {
				fixture := &model3DHTTPFixture{t: t}
				response := model3DHTTPRequest(t, model3DHTTPAuth(service), fixture, path+tc.suffix, tc.body, "addp_at_current")
				if response.Code != 400 || fixture.issues+fixture.consumes != 0 || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status=%d calls=%d body=%s", response.Code, fixture.issues+fixture.consumes, response.Body.String())
				}
			})
		}
	}
	for _, id := range []string{"09", "0", "-1", "9223372036854775808", "+9"} {
		fixture := &model3DHTTPFixture{t: t}
		path := strings.Replace(model3DHTTPConsumePath, "/9/", "/"+id+"/", 1)
		response := model3DHTTPRequest(t, model3DHTTPAuth(true), fixture, path, model3DHTTPBody(t, true), "addp_at_current")
		if response.Code != 400 || fixture.consumes != 0 {
			t.Fatalf("invalid ID accepted: %s", id)
		}
	}
}

func TestManagerModel3DHTTPAdmissionUsesOnlyExactCredentialsAndPermissions(t *testing.T) {
	for _, service := range []bool{false, true} {
		path := model3DHTTPIssuePath
		if service {
			path = model3DHTTPConsumePath
		}
		cases := []struct {
			name, bearer string
			change       func(*auth.AuthContext)
			status       int
		}{
			{"no bearer", "", func(*auth.AuthContext) {}, 401},
			{"no permission", "addp_at_current", func(c *auth.AuthContext) { c.Authorization.RoleAssignments = []auth.RoleAssignment{} }, 403},
			{"wrong credential", "addp_at_current", func(c *auth.AuthContext) { *c = model3DHTTPAuth(!service) }, 403},
			{"expired", "addp_at_current", func(c *auth.AuthContext) {
				c.Token.ExpiresAt = time.Now().Add(-time.Second)
				c.Token.IssuedAt = time.Now().Add(-time.Minute)
			}, 401},
		}
		if !service {
			cases = append(cases,
				struct {
					name, bearer string
					change       func(*auth.AuthContext)
					status       int
				}{"generic EA permission only", "addp_at_current", func(c *auth.AuthContext) {
					c.Authorization.RoleAssignments[0].Permissions = []string{"system.execution_authorization.create"}
				}, 403},
				struct {
					name, bearer string
					change       func(*auth.AuthContext)
					status       int
				}{"missing source read", "addp_at_current", func(c *auth.AuthContext) {
					c.Authorization.RoleAssignments[0].Permissions = []string{"manager.derived_artifact.create"}
				}, 403},
				struct {
					name, bearer string
					change       func(*auth.AuthContext)
					status       int
				}{"missing derived create", "addp_at_current", func(c *auth.AuthContext) {
					c.Authorization.RoleAssignments[0].Permissions = []string{"manager.data_item.read"}
				}, 403},
				struct {
					name, bearer string
					change       func(*auth.AuthContext)
					status       int
				}{"OAuth tools only", "addp_at_current", func(c *auth.AuthContext) {
					c.Token.Type = middleware.IAMTokenTypeOAuthAccess
					c.Client.ScopeMode = "restricted"
					c.Client.Scopes = []string{"data.preview"}
				}, 403},
			)
		}
		for _, kind := range []string{"delegated", "ticket"} {
			k := kind
			cases = append(cases, struct {
				name, bearer string
				change       func(*auth.AuthContext)
				status       int
			}{k, "addp_at_current", func(c *auth.AuthContext) {
				*c = model3DHTTPAuth(false)
				c.Client.Audiences = []string{"manager"}
				c.Client.ScopeMode = "restricted"
				if k == "delegated" {
					c.Token.Type = middleware.IAMTokenTypeDelegatedAccess
					c.Client.Scopes = []string{"data.preview"}
					c.Delegation = &auth.DelegationFacts{DelegatedByClientID: *c.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
				} else {
					c.Token.Type = "resource_access_ticket"
					c.Client.Scopes = []string{auth.BrowserResourceAccessScope}
				}
			}, 403})
		}
		for _, tc := range cases {
			t.Run(tc.name+path, func(t *testing.T) {
				current := model3DHTTPAuth(service)
				tc.change(&current)
				fixture := &model3DHTTPFixture{t: t}
				response := model3DHTTPRequest(t, current, fixture, path, model3DHTTPBody(t, service), tc.bearer)
				if response.Code != tc.status || fixture.issues+fixture.consumes != 0 || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status=%d calls=%d body=%s", response.Code, fixture.issues+fixture.consumes, response.Body.String())
				}
				if strings.Contains(response.Body.String(), "connection-only-secret") {
					t.Fatal("unauthorized connection leak")
				}
			})
		}
	}
	for _, clientID := range []string{"addp-meta", "addp-system"} {
		current := model3DHTTPAuth(true)
		current.Client.ClientID = &clientID
		fixture := &model3DHTTPFixture{t: t}
		response := model3DHTTPRequest(t, current, fixture, model3DHTTPConsumePath, model3DHTTPBody(t, true), "addp_at_current")
		if response.Code != 403 || fixture.consumes != 0 {
			t.Fatal("wrong runtime consumed authorization")
		}
	}
}

func TestManagerModel3DHTTPDoesNotLeakDomainErrorsOrInvalidReceipts(t *testing.T) {
	for _, service := range []bool{false, true} {
		path := model3DHTTPIssuePath
		if service {
			path = model3DHTTPConsumePath
		}
		for _, tc := range []struct {
			err    error
			status int
		}{{commonapi.ErrForbidden, 403}, {commonapi.ErrUnauthorized, 401}, {iam.ErrExecutionAuthorizationConflict, 409}, {iam.ErrExecutionAuthorizationUnavailable, 403}, {engineaccess.ErrAuthorizationFactsUnavailable, 503}, {engineaccess.ErrAuthorizationBoundaryChanged, 409}, {errors.New("private database credential addp_at_current connection-only-secret"), 500}} {
			fixture := &model3DHTTPFixture{t: t, err: tc.err}
			response := model3DHTTPRequest(t, model3DHTTPAuth(service), fixture, path, model3DHTTPBody(t, service), "addp_at_current")
			if response.Code != tc.status || fixture.issues+fixture.consumes != 1 {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			for _, secret := range []string{"private database", "addp_at_current", "connection-only-secret"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatal("domain error leaked private facts")
				}
			}
		}
	}
	for _, kind := range []string{"nil", "wrong tenant", "wrong execution", "expired", "bad scope"} {
		t.Run("issue "+kind, func(t *testing.T) {
			f := &model3DHTTPFixture{t: t}
			f.nilIssue = kind == "nil"
			f.mutateIssue = func(result *iam.IssuedManagerModel3DAuthorization) {
				switch kind {
				case "wrong tenant":
					result.TenantID = 4
				case "wrong execution":
					result.ExecutionID = uuid.New()
				case "expired":
					result.ExpiresAt = time.Now().Add(-time.Second)
				case "bad scope":
					result.SourceReadScope.EngineVersion = "05"
				}
			}
			response := model3DHTTPRequest(t, model3DHTTPAuth(false), f, model3DHTTPIssuePath, model3DHTTPBody(t, false), "addp_at_current")
			if response.Code != 500 {
				t.Fatalf("invalid issuance receipt accepted: %s %d", kind, response.Code)
			}
		})
	}
	for _, kind := range []string{"nil", "zero time", "nil engine", "wrong tenant", "wrong engine", "wrong version", "wrong type", "inactive", "deleted", "missing connection"} {
		t.Run("consume "+kind, func(t *testing.T) {
			f := &model3DHTTPFixture{t: t}
			f.nilObservation = kind == "nil"
			f.mutateObservation = func(result *iam.ManagerModel3DAccessObservation) {
				switch kind {
				case "zero time":
					result.ObservedAt = time.Time{}
				case "nil engine":
					result.Engine = nil
				case "wrong tenant":
					tenant := uint(4)
					result.Engine.TenantID = &tenant
				case "wrong engine":
					result.Engine.ID = 13
				case "wrong version":
					result.Engine.Version = 6
				case "wrong type":
					result.Engine.EngineType = "nfs"
				case "inactive":
					result.Engine.LifecycleState = "inactive"
				case "deleted":
					now := time.Now()
					result.Engine.DeletedAt = &now
				case "missing connection":
					result.Engine.ConnectionInfo = nil
				}
			}
			response := model3DHTTPRequest(t, model3DHTTPAuth(true), f, model3DHTTPConsumePath, model3DHTTPBody(t, true), "addp_at_current")
			if response.Code != 500 || strings.Contains(response.Body.String(), "connection-only-secret") {
				t.Fatalf("invalid observation accepted: %s %d", kind, response.Code)
			}
		})
	}
}
