package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
	"github.com/addp/ontology/internal/service"
	"github.com/gin-gonic/gin"
)

type fakeCommands struct {
	RevisionCommands
	actor        models.Actor
	scope        semantic.Scope
	version      uint64
	action       string
	definition   semantic.Definition
	err          error
	admissionErr error
	token        string
	generation   string
	calls        int
}

const generation = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
const executionID = "ffffffff-bbbb-4ccc-8ddd-eeeeeeeeeeee"

func (f *fakeCommands) record(a models.Actor, s semantic.Scope, version uint64) (*models.Revision, error) {
	f.calls++
	f.actor = a
	f.scope = s
	f.version = version
	g, e := generation, executionID
	return &models.Revision{OntologyID: s.OntologyID, Revision: s.Revision, Version: version + 1, Status: models.Published, Payload: `{"definition":{}}`, Generation: &g, BuildExecutionID: &e}, f.err
}
func (f *fakeCommands) CreateDraft(_ context.Context, a models.Actor, d semantic.Definition) (*models.Revision, error) {
	f.definition = d
	return f.record(a, d.Scope, 0)
}
func (f *fakeCommands) SaveDraft(_ context.Context, a models.Actor, v uint64, d semantic.Definition) (*models.Revision, error) {
	f.definition = d
	return f.record(a, d.Scope, v)
}
func (f *fakeCommands) Get(_ context.Context, a models.Actor, s semantic.Scope) (*models.Revision, error) {
	return f.record(a, s, 1)
}
func (f *fakeCommands) Transition(_ context.Context, a models.Actor, s semantic.Scope, v uint64, action string) (*models.Revision, error) {
	f.action = action
	return f.record(a, s, v)
}
func (f *fakeCommands) RebuildProjection(_ context.Context, a models.Actor, s semantic.Scope, v uint64, failed string, baseline uint64) (*models.Projection, error) {
	_, err := f.record(a, s, v)
	f.generation = failed
	return &models.Projection{Generation: generation, ExecutionID: executionID, BaselineVersion: baseline}, err
}
func (f *fakeCommands) AdmitProjection(_ context.Context, a models.Actor, s semantic.Scope, v uint64, g, token string, _ service.ExecutionAuthorizationIssuer) error {
	f.token = token
	f.generation = g
	return f.admissionErr
}
func (f *fakeCommands) Head(_ context.Context, a models.Actor, id string) (*models.Ontology, error) {
	_, err := f.record(a, semantic.Scope{TenantID: a.TenantID, OntologyID: id, Revision: 1}, 1)
	return &models.Ontology{OntologyID: id, LastRevision: 3, ActivationVersion: 2}, err
}
func (f *fakeCommands) Projection(_ context.Context, a models.Actor, id, g string) (*models.Projection, error) {
	_, err := f.record(a, semantic.Scope{TenantID: a.TenantID, OntologyID: id, Revision: 1}, 1)
	return &models.Projection{OntologyID: id, Generation: g, Status: "failed"}, err
}

func (f *fakeCommands) LatestProjection(ctx context.Context, a models.Actor, s semantic.Scope) (*models.Projection, error) {
	return f.Projection(ctx, a, s.OntologyID, generation)
}

func testRouter(t *testing.T, f *fakeCommands, ac authorization.AuthContext, ready bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer addp_at_test" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(ac)
	}))
	t.Cleanup(system.Close)
	lifecycle := modulelifecycle.NewStandalone("ontology", modulelifecycle.StaticCheck("test", ready, "test_unavailable"))
	return SetupRouter(system.URL, lifecycle, f, nil)
}
func perform(r http.Handler, method, path, body, token, lang string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/api/v1/ontology"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", lang)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}
func allPermissions() []string {
	return []string{"ontology.revision.publish", "ontology.revision.read", "ontology.revision.update", "system.execution_authorization.create"}
}

func TestFormalRouterIdentityAndPermissionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*authorization.AuthContext)
		token  string
		ready  bool
		status int
	}{
		{"anonymous", nil, "", true, 401},
		{"invalid_token", nil, "addp_at_wrong", true, 401},
		{"missing_permission", func(a *authorization.AuthContext) {
			a.Authorization.RoleAssignments[0].Permissions = []string{"ontology.revision.read"}
		}, "addp_at_test", true, 403},
		{"not_ready", nil, "addp_at_test", false, 503},
		{"missing_execution_permission", func(a *authorization.AuthContext) {
			a.Authorization.RoleAssignments[0].Permissions = []string{"ontology.revision.publish"}
		}, "addp_at_test", true, 403},
		{"service_principal", func(a *authorization.AuthContext) {
			a.Principal.Type = "service_principal"
			a.Token.Type = "service_access_token"
			a.Authentication.Methods = []string{"service_secret"}
			a.Authentication.AssuranceLevel = "not_applicable"
		}, "addp_at_test", true, 403},
		{"project_group_candidate", func(a *authorization.AuthContext) {
			group := "7"
			a.Authorization.RoleAssignments[0].Scope.Type = "project_group"
			a.Authorization.RoleAssignments[0].Scope.ProjectGroupID = &group
		}, "addp_at_test", true, 403},
		{"delegated", func(a *authorization.AuthContext) {
			a.Token.Type = "delegated_access_token"
			a.Client.ScopeMode = "restricted"
			a.Client.Audiences = []string{"ontology"}
			a.Client.Scopes = []string{"ontology.publish"}
			a.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *a.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
		}, "addp_at_test", true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ac := authtest.NewTenantUserAuthContext("101", "9", allPermissions())
			if tc.mutate != nil {
				tc.mutate(&ac)
			}
			f := &fakeCommands{}
			r := testRouter(t, f, ac, tc.ready)
			w := perform(r, "POST", "/ontologies/outdoor/revisions/1/publish", `{"version":2}`, tc.token, "en")
			if w.Code != tc.status || f.calls != 0 {
				t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body, f.calls)
			}
		})
	}
}

func TestCommandIdentityAndAdmissionOutcome(t *testing.T) {
	ac := authtest.NewTenantUserAuthContext("101", "9", allPermissions())
	f := &fakeCommands{}
	r := testRouter(t, f, ac, true)
	w := perform(r, "POST", "/ontologies/outdoor/revisions", `{"revision":1,"definition":{"classes":[{"id":"activity","name":"北京活动"}]}}`, "addp_at_test", "en")
	if w.Code != 201 || f.actor.TenantID != 101 || f.actor.PrincipalID != 9 || f.actor.MembershipID != 1 || f.actor.AuthorizationVersion != 1 || f.definition.Scope.OntologyID != "outdoor" {
		t.Fatalf("create: %d %s %+v", w.Code, w.Body, f)
	}
	w = perform(r, "POST", "/ontologies/outdoor/revisions/1/publish", `{"version":2}`, "addp_at_test", "en")
	if w.Code != 202 || f.action != "publish" || f.token != "addp_at_test" || !strings.Contains(w.Body.String(), generation) || strings.Contains(w.Body.String(), f.token) {
		t.Fatalf("publish: %d %s %+v", w.Code, w.Body, f)
	}
	f.admissionErr = errors.New("private connection secret")
	w = perform(r, "POST", "/ontologies/outdoor/revisions/1/rebuild", `{"version":3,"failed_generation":"`+generation+`","activation_version":2}`, "addp_at_test", "en")
	if w.Code != 502 || !strings.Contains(w.Body.String(), `"intent"`) || !strings.Contains(w.Body.String(), `"version":3`) || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("admission: %d %s", w.Code, w.Body)
	}
}

func TestStrictInputAndSafeErrors(t *testing.T) {
	ac := authtest.NewTenantUserAuthContext("101", "9", allPermissions())
	f := &fakeCommands{}
	r := testRouter(t, f, ac, true)
	for _, body := range []string{`{}`, `null`, `{"version":0}`, `{"version":-1}`, `{"version":1,"tenant_id":102}`, `{"version":1} {}`, `{"version":1,"definition":{}}`, strings.Repeat(" ", (1<<20)+4097)} {
		w := perform(r, "POST", "/ontologies/outdoor/revisions/1/submit", body, "addp_at_test", "en")
		if w.Code != 400 || f.calls != 0 {
			t.Fatalf("invalid body: %d %s calls=%d", w.Code, w.Body, f.calls)
		}
	}
	for _, path := range []string{"/ontologies/INVALID/revisions/1", "/ontologies/outdoor/revisions/01", "/ontologies/outdoor/revisions/0"} {
		w := perform(r, "GET", path, "", "addp_at_test", "en")
		if w.Code != 400 {
			t.Fatalf("path=%s: %d", path, w.Code)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{repository.ErrConflict, 409, "resource_version_conflict"}, {repository.ErrNotFound, 404, "ontology_not_found"}, {repository.ErrIntegrity, 500, "ontology_operation_failed"}, {errors.New("private SQL secret"), 500, "ontology_operation_failed"}} {
		f.err = tc.err
		w := perform(r, "GET", "/ontologies/outdoor/revisions/1", "", "addp_at_test", "en")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("error: %d %s", w.Code, w.Body)
		}
	}
	w := perform(r, "GET", "/ontologies/outdoor/revisions/1", "", "addp_at_test", "zh-cn")
	if !strings.Contains(w.Body.String(), "本体操作失败") {
		t.Fatal(w.Body.String())
	}
}

func TestHealthAndReadDTOs(t *testing.T) {
	f := &fakeCommands{}
	r := testRouter(t, f, authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.revision.read"}), true)
	for _, path := range []string{"/health/live", "/health/ready"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	w := perform(r, "GET", "/ontologies/outdoor", "", "addp_at_test", "en")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"active_generation":null`) || !strings.Contains(w.Body.String(), `"activation_version":2`) {
		t.Fatal(w.Body.String())
	}
	w = perform(r, "GET", "/ontologies/outdoor/projections/"+generation, "", "addp_at_test", "en")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"failed"`) || strings.Contains(w.Body.String(), "lease") {
		t.Fatal(w.Body.String())
	}
	w = perform(r, "GET", "/ontologies/outdoor/revisions/1/projection", "", "addp_at_test", "en")
	if w.Code != 200 || !strings.Contains(w.Body.String(), generation) {
		t.Fatalf("latest: %d %s", w.Code, w.Body)
	}
}

func TestEditTransitionRoutes(t *testing.T) {
	f := &fakeCommands{}
	r := testRouter(t, f, authtest.NewTenantUserAuthContext("101", "9", allPermissions()), true)
	w := perform(r, "PUT", "/ontologies/outdoor/revisions/2", `{"version":3,"definition":{"classes":[{"id":"activity","name":"活动"}]}}`, "addp_at_test", "en")
	if w.Code != 200 || f.version != 3 || f.scope.Revision != 2 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	for _, action := range []string{"submit", "return", "withdraw"} {
		w = perform(r, "POST", "/ontologies/outdoor/revisions/2/"+action, `{"version":3}`, "addp_at_test", "en")
		if w.Code != 200 || f.action != action {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body)
		}
	}
	f.calls = 0
	w = perform(r, "POST", "/ontologies/outdoor/revisions", `{"revision":1,"definition":{"scope":{"tenant_id":102},"classes":[]}}`, "addp_at_test", "en")
	if w.Code != 400 || f.calls != 0 {
		t.Fatal("accepted caller scope", w.Code)
	}
}
