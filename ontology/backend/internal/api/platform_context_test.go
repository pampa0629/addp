package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/ontology/internal/platform"
)

func TestPlatformContextUsesSeparateDefinitionAndExactDelegation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*authorization.AuthContext)
		path   string
		ready  bool
		want   int
	}{
		{"user", nil, "/platform/capabilities/transfer.task.create", true, 200},
		{"delegated", func(a *authorization.AuthContext) {
			delegatedSemantic(a)
			a.Client.Scopes = []string{"platform.capability.context"}
		}, "/platform/capabilities/transfer.task.create", true, 200},
		{"wrong_scope", delegatedSemantic, "/platform/capabilities/transfer.task.create", true, 403},
		{"no_permission", func(a *authorization.AuthContext) {
			a.Authorization.RoleAssignments[0].Permissions = []string{"ontology.revision.read"}
		}, "/platform/capabilities/transfer.task.create", true, 403},
		{"query_rejected", nil, "/platform/capabilities/transfer.task.create?tenant_id=8", true, 400},
		{"unknown_capability", nil, "/platform/capabilities/transfer.task.run", true, 404},
		{"not_ready", nil, "/platform/capabilities/transfer.task.create", false, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.semantic.read"})
			if tc.mutate != nil {
				tc.mutate(&a)
			}
			f := &fakeCommands{}
			w := perform(testRouter(t, f, a, tc.ready), "GET", tc.path, "", "addp_at_test", "en")
			if w.Code != tc.want || f.calls != 0 {
				t.Fatalf("status=%d body=%s Tenant reads=%d", w.Code, w.Body, f.calls)
			}
			if w.Code == 200 {
				var context platform.Context
				if err := json.Unmarshal(w.Body.Bytes(), &context); err != nil {
					t.Fatal(err)
				}
				if context.KnowledgeKind != "platform_definition" || context.Availability != "not_observed" || len(context.Digest) != 64 {
					t.Fatalf("context=%+v", context)
				}
				expected, err := platform.TransferContext()
				if err != nil || !reflect.DeepEqual(context, expected) {
					t.Fatalf("API did not consume the compiled release: %v", err)
				}
			}
		})
	}
}
