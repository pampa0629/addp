package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
	"github.com/addp/ontology/internal/service"
)

func (f *fakeCommands) Trial(_ context.Context, a models.Actor, id, ruleID string, revision uint64, g string, activation uint64, facts map[string]semantic.Fact) (*service.TrialResult, error) {
	f.record(a, semantic.Scope{TenantID: a.TenantID, OntologyID: id, Revision: revision}, activation)
	f.generation, f.action, f.trialFacts = g, ruleID, facts
	return &service.TrialResult{Decision: semantic.Decision{Mode: "hypothetical", Outcome: semantic.Undetermined, Code: "insufficient_evidence"}}, f.err
}

func TestTrialUserBoundaryAndStrictRequest(t *testing.T) {
	path := "/ontologies/outdoor/semantic/rules/registered/trial"
	body := `{"revision":2,"generation":"` + generation + `","activation_version":3,"inputs":{"status":{"state":"known","value":"报名中"}}}`
	for _, tc := range []struct {
		name   string
		mutate func(*authorization.AuthContext)
		want   int
	}{
		{"user", nil, 200},
		{"delegated", delegatedSemantic, 403},
		{"no_permission", func(a *authorization.AuthContext) {
			a.Authorization.RoleAssignments[0].Permissions = []string{"ontology.revision.read"}
		}, 403},
		{"project_scope", func(a *authorization.AuthContext) {
			group := "7"
			a.Authorization.RoleAssignments[0].Scope.Type = "project_group"
			a.Authorization.RoleAssignments[0].Scope.ProjectGroupID = &group
		}, 403},
		{"service", func(a *authorization.AuthContext) {
			a.Principal.Type = "service_principal"
			a.Token.Type = "service_access_token"
			a.Authentication.Methods = []string{"service_secret"}
			a.Authentication.AssuranceLevel = "not_applicable"
		}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.semantic.read"})
			if tc.mutate != nil {
				tc.mutate(&a)
			}
			f := &fakeCommands{}
			w := perform(testRouter(t, f, a, true), "POST", path, body, "addp_at_test", "en")
			if w.Code != tc.want || (tc.want != 200 && f.calls != 0) {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, f.calls, w.Body)
			}
			if tc.want == 200 && (f.actor.TenantID != 101 || f.scope.Revision != 2 || f.version != 3 || f.generation != generation || f.action != "registered" || f.trialFacts["status"].Value != "报名中" || w.Header().Get("Cache-Control") != "no-store") {
				t.Fatalf("lost binding: %+v", f)
			}
		})
	}
	a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.semantic.read"})
	invalid := []string{"", `{}`, strings.Replace(body, `"revision":2`, `"revision":0`, 1), strings.Replace(body, generation, "bad", 1), strings.Replace(body, `"inputs":{`, `"tenant_id":101,"inputs":{`, 1), strings.Replace(body, `"state":"known"`, `"state":"unknown"`, 1), strings.Replace(body, `"state":"known"`, `"state":"missing"`, 1), body + `{}`, strings.Replace(body, `"value":"报名中"`, `"expression":"true"`, 1), strings.Replace(body, `"value":"报名中"`, `"value":"`+strings.Repeat("x", 96<<10)+`"`, 1)}
	for index, b := range invalid {
		f := &fakeCommands{}
		w := perform(testRouter(t, f, a, true), "POST", path, b, "addp_at_test", "en")
		if w.Code != 400 || f.calls != 0 {
			t.Fatalf("strict parsing case=%d status=%d calls=%d", index, w.Code, f.calls)
		}
	}
	for _, b := range []string{strings.Replace(body, `{"status":{"state":"known","value":"报名中"}}`, `{}`, 1), strings.Replace(body, `"known","value":"报名中"`, `"invalid"`, 1), strings.Replace(body, `"known","value":"报名中"`, `"absent"`, 1), strings.Replace(body, `"known","value":"报名中"`, `"unknown"`, 1), strings.Replace(body, `"value":"报名中"`, `"value":false`, 1)} {
		f := &fakeCommands{}
		w := perform(testRouter(t, f, a, true), "POST", path, b, "addp_at_test", "zh-cn")
		if w.Code != 200 {
			t.Fatalf("valid transport: %d %s", w.Code, w.Body)
		}
	}
	f := &fakeCommands{}
	if w := perform(testRouter(t, f, a, true), "POST", path+"?tenant_id=101", body, "addp_at_test", "en"); w.Code != 400 || f.calls != 0 {
		t.Fatal(w.Code)
	}
	var request map[string]any
	_ = json.Unmarshal([]byte(body), &request)
	inputs := map[string]any{}
	for i := 0; i < 17; i++ {
		inputs["a"+strings.Repeat("x", i)] = map[string]any{"state": "unknown"}
	}
	request["inputs"] = inputs
	oversized, _ := json.Marshal(request)
	if w := perform(testRouter(t, f, a, true), "POST", path, string(oversized), "addp_at_test", "en"); w.Code != 400 || f.calls != 0 {
		t.Fatal(w.Code)
	}
}
