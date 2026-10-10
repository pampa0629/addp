package api

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/service"
)

func TestPlatformInspectionUserScopeAndFrozenRead(t *testing.T) {
	snapshot, err := platform.CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	d, _ := snapshot.Context()
	r, _ := snapshot.Review()
	for _, tc := range []struct {
		name   string
		mutate func(*authorization.AuthContext)
		path   string
		err    error
		want   int
	}{
		{"list", nil, "/platform/definitions", nil, 200},
		{"detail", nil, "/platform/definitions/transfer.task.create", nil, 200},
		{"query", nil, "/platform/definitions?tenant_id=1", nil, 400},
		{"detail_query", nil, "/platform/definitions/transfer.task.create?revision=4", nil, 400},
		{"invalid_id", nil, "/platform/definitions/Transfer.task.create", nil, 400},
		{"corrupt", nil, "/platform/definitions/transfer.task.create", repository.ErrIntegrity, 500},
		{"inactive", nil, "/platform/definitions/transfer.task.create", repository.ErrNotActive, 409},
		{"unknown", nil, "/platform/definitions/transfer.task.run", nil, 404},
		{"tenant", func(a *authorization.AuthContext) {
			*a = authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.platform_definition.read"})
		}, "/platform/definitions", nil, 403},
		{"machine", func(a *authorization.AuthContext) {
			a.Principal.Type = "service_principal"
			a.Token.Type = "service_access_token"
		}, "/platform/definitions", nil, 403},
		{"delegated", func(a *authorization.AuthContext) {
			*a = authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.semantic.read"})
			delegatedSemantic(a)
		}, "/platform/definitions", nil, 403},
		{"no_permission", func(a *authorization.AuthContext) {
			a.Authorization.RoleAssignments[0].Permissions = []string{"ontology.platform_definition.publish"}
		}, "/platform/definitions", nil, 403},
		{"tenant_api", nil, "/platform/capabilities", nil, 403},
		{"wrong_scope", func(a *authorization.AuthContext) { a.Authorization.RoleAssignments[0].Scope.Type = "tenant" }, "/platform/definitions", nil, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.platform_definition.read"})
			a.Context.Type, a.Context.TenantID, a.Context.TenantMembershipID = "platform", nil, nil
			a.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
			a.Authorization.RoleAssignments[0].RoleKey = "platform.system_administrator"
			a.Authentication.AssuranceLevel = "aal2"
			if tc.mutate != nil {
				tc.mutate(&a)
			}
			f := &fakeCommands{platformContext: d, platformReview: r, platformErr: tc.err}
			w := perform(testRouter(t, f, a, true), "GET", tc.path, "", "addp_at_test", "en")
			if w.Code != tc.want || f.calls != 0 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if tc.want == 403 && f.platformCalls != 0 {
				t.Fatal("read before authorization")
			}
			if tc.name == "detail" {
				var result service.PlatformDefinition
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Context.Digest != d.Digest || len(result.Review.Bindings) != len(r.Bindings) || f.platformCalls != 1 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("lost frozen release")
				}
			}
		})
	}
}
