package api

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
	"github.com/addp/ontology/internal/service"
)

func (f *fakeCommands) ListClasses(_ context.Context, a models.Actor, id string) (*service.ClassDirectory, error) {
	f.record(a, semantic.Scope{TenantID: a.TenantID, OntologyID: id, Revision: 1}, 1)
	return &service.ClassDirectory{SemanticBinding: service.SemanticBinding{OntologyID: id, Revision: 1, Generation: generation, ActivationVersion: 1, Digest: strings.Repeat("a", 64), KnowledgeKind: "native_definition"}, Classes: []semantic.Class{}}, f.err
}
func (f *fakeCommands) ClassContext(_ context.Context, a models.Actor, id, classID string, revision uint64, g string, activation uint64) (*service.SemanticContext, error) {
	f.record(a, semantic.Scope{TenantID: a.TenantID, OntologyID: id, Revision: revision}, activation)
	f.generation, f.action = g, classID
	return &service.SemanticContext{}, f.err
}

func delegatedSemantic(a *authorization.AuthContext) {
	a.Token.Type = "delegated_access_token"
	a.Client.ScopeMode = "restricted"
	a.Client.Audiences = []string{"ontology"}
	a.Client.Scopes = []string{"ontology.classes.list"}
	a.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *a.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
}

func TestSemanticAuthorizationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*authorization.AuthContext)
		path   string
		want   int
	}{
		{"user", nil, "/ontologies/outdoor/semantic/classes", 200},
		{"delegated", delegatedSemantic, "/ontologies/outdoor/semantic/classes", 200},
		{"delegated_management_denied", delegatedSemantic, "/ontologies/outdoor/revisions/1", 403},
		{"wrong_scope", func(a *authorization.AuthContext) {
			delegatedSemantic(a)
			a.Client.Scopes = []string{"ontology.class.context"}
		}, "/ontologies/outdoor/semantic/classes", 403},
		{"wrong_audience", func(a *authorization.AuthContext) { delegatedSemantic(a); a.Client.Audiences = []string{"manager"} }, "/ontologies/outdoor/semantic/classes", 403},
		{"no_permission", func(a *authorization.AuthContext) {
			a.Authorization.RoleAssignments[0].Permissions = []string{"ontology.revision.read"}
		}, "/ontologies/outdoor/semantic/classes", 403},
		{"project_not_tenant", func(a *authorization.AuthContext) {
			group := "7"
			a.Authorization.RoleAssignments[0].Scope.Type = "project_group"
			a.Authorization.RoleAssignments[0].Scope.ProjectGroupID = &group
		}, "/ontologies/outdoor/semantic/classes", 403},
		{"service_denied", func(a *authorization.AuthContext) {
			a.Principal.Type = "service_principal"
			a.Token.Type = "service_access_token"
			a.Authentication.Methods = []string{"service_secret"}
			a.Authentication.AssuranceLevel = "not_applicable"
		}, "/ontologies/outdoor/semantic/classes", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.semantic.read", "ontology.revision.read"})
			if tc.mutate != nil {
				tc.mutate(&a)
			}
			f := &fakeCommands{}
			w := perform(testRouter(t, f, a, true), "GET", tc.path, "", "addp_at_test", "en")
			if w.Code != tc.want || (tc.want != 200 && f.calls != 0) {
				t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body, f.calls)
			}
		})
	}
}

func TestSemanticStrictBindingAndStableErrors(t *testing.T) {
	a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.semantic.read"})
	delegatedSemantic(&a)
	a.Client.Scopes = []string{"ontology.class.context"}
	f := &fakeCommands{}
	r := testRouter(t, f, a, true)
	base := "/ontologies/outdoor/semantic/classes/activity"
	query := "revision=2&generation=" + generation + "&activation_version=3"
	w := perform(r, "GET", base+"?"+query, "", "addp_at_test", "en")
	if w.Code != 200 || f.scope.Revision != 2 || f.version != 3 || f.generation != generation || f.action != "activity" || f.actor.TenantID != 101 {
		t.Fatalf("%d %s %+v", w.Code, w.Body, f)
	}
	for _, q := range []string{"", query + "&revision=2", query + "&tenant_id=101", strings.Replace(query, "revision=2", "revision=02", 1), strings.Replace(query, "activation_version=3", "activation_version=0", 1), strings.Replace(query, generation, "invalid", 1), query + "&unknown=%xx"} {
		f.calls = 0
		w = perform(r, "GET", base+"?"+q, "", "addp_at_test", "en")
		if w.Code != 400 || f.calls != 0 {
			t.Fatalf("q=%s: %d %s", q, w.Code, w.Body)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{repository.ErrNotActive, 409, "ontology_not_active"}, {service.ErrActivationChanged, 409, "ontology_activation_changed"}, {service.ErrResultTooLarge, 413, "result_too_large"}, {repository.ErrNotFound, 404, "ontology_not_found"}} {
		f.err = tc.err
		for _, lang := range []string{"en", "zh-cn"} {
			w = perform(r, "GET", base+"?"+query, "", "addp_at_test", lang)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		}
	}
}
