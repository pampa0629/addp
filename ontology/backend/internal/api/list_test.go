package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
)

func (f *fakeCommands) ListOntologies(_ context.Context, a models.Actor, p models.ListPage) ([]models.Ontology, int64, error) {
	f.actor, f.page = a, p
	f.calls++
	if p.Page > 1 {
		return nil, 1, f.err
	}
	return []models.Ontology{{TenantID: a.TenantID, OntologyID: "outdoor", LastRevision: 2, ActivationVersion: 1}}, 1, f.err
}

func (f *fakeCommands) ListRevisions(_ context.Context, a models.Actor, id string, p models.ListPage) ([]models.RevisionSummary, int64, error) {
	f.actor, f.page = a, p
	f.scope.OntologyID = id
	f.calls++
	if p.Page > 1 {
		return nil, 1, f.err
	}
	return []models.RevisionSummary{{OntologyID: id, Revision: 2, Version: 3, Status: models.Published}}, 1, f.err
}

func TestListPaginationAndStrictQuery(t *testing.T) {
	for _, path := range []string{"/ontologies", "/ontologies/outdoor/revisions"} {
		t.Run(path, func(t *testing.T) {
			f := &fakeCommands{}
			r := testRouter(t, f, authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.revision.read"}), true)
			w := perform(r, "GET", path, "", "addp_at_test", "en")
			if w.Code != 200 || f.actor.TenantID != 101 || f.actor.PrincipalID != 9 || f.page != (models.ListPage{Page: 1, PageSize: 20}) {
				t.Fatalf("default: %d %s %+v", w.Code, w.Body, f)
			}
			for _, field := range []string{`"snapshot"`, `"payload"`, `"tenant_id"`, `"authorization"`, `"graph_key"`} {
				if strings.Contains(w.Body.String(), field) {
					t.Fatalf("private or large field %s in list: %s", field, w.Body)
				}
			}
			w = perform(r, "GET", path+"?page=2&page_size=1", "", "addp_at_test", "en")
			var body struct {
				Data       []json.RawMessage `json:"data"`
				Total      int64             `json:"total"`
				Page       int               `json:"page"`
				PageSize   int               `json:"page_size"`
				TotalPages int               `json:"total_pages"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || body.Data == nil || len(body.Data) != 0 || body.Total != 1 || body.Page != 2 || body.PageSize != 1 || body.TotalPages != 1 {
				t.Fatalf("empty page: %d %s %v", w.Code, w.Body, err)
			}
			calls := f.calls
			for _, query := range []string{"page=0", "page=-1", "page=01", "page=%2B1", "page=", "page=1.0", "page=x", "page=999999999999999999999", "page=2147483648&page_size=100", "page_size=0", "page_size=101", "page_size=", "page_size=01", "page=1&page=2", "page_size=1&page_size=1", "tenant_id=102", "search=outdoor", "sort=payload", "page=1;page_size=1", "page=%zz"} {
				w = perform(r, "GET", path+"?"+query, "", "addp_at_test", "en")
				if w.Code != 400 || f.calls != calls || !strings.Contains(w.Body.String(), "invalid_ontology_request") {
					t.Fatalf("accepted %s: %d %s", query, w.Code, w.Body)
				}
			}
			for _, tc := range []struct {
				err    error
				status int
			}{{repository.ErrNotFound, 404}, {errors.New("private database detail"), 500}} {
				f.err = tc.err
				w = perform(r, "GET", path, "", "addp_at_test", "en")
				if w.Code != tc.status || strings.Contains(w.Body.String(), "private") {
					t.Fatalf("unsafe error: %d %s", w.Code, w.Body)
				}
			}
		})
	}
}

func TestListsRequireTenantUserReadPermission(t *testing.T) {
	for _, path := range []string{"/ontologies", "/ontologies/outdoor/revisions"} {
		for _, tc := range []struct {
			name   string
			mutate func(*authorization.AuthContext)
			token  string
			ready  bool
			status int
		}{
			{"anonymous", nil, "", true, 401},
			{"not_ready", nil, "addp_at_test", false, 503},
			{"write_only", func(a *authorization.AuthContext) {
				a.Authorization.RoleAssignments[0].Permissions = []string{"ontology.revision.update"}
			}, "addp_at_test", true, 403},
			{"project_scope", func(a *authorization.AuthContext) {
				group := "7"
				a.Authorization.RoleAssignments[0].Scope.Type = "project_group"
				a.Authorization.RoleAssignments[0].Scope.ProjectGroupID = &group
			}, "addp_at_test", true, 403},
			{"service", func(a *authorization.AuthContext) {
				a.Principal.Type = "service_principal"
				a.Token.Type = "service_access_token"
				a.Authentication.Methods = []string{"service_secret"}
				a.Authentication.AssuranceLevel = "not_applicable"
			}, "addp_at_test", true, 403},
			{"delegated", func(a *authorization.AuthContext) {
				a.Token.Type = "delegated_access_token"
				a.Client.ScopeMode = "restricted"
				a.Client.Audiences = []string{"ontology"}
				a.Client.Scopes = []string{"ontology.read"}
				a.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *a.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
			}, "addp_at_test", true, 403},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.revision.read"})
				if tc.mutate != nil {
					tc.mutate(&a)
				}
				f := &fakeCommands{}
				w := perform(testRouter(t, f, a, tc.ready), "GET", path, "", tc.token, "en")
				if w.Code != tc.status || f.calls != 0 {
					t.Fatalf("%d %s calls=%d", w.Code, w.Body, f.calls)
				}
			})
		}
	}
}
