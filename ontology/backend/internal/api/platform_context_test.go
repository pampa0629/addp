package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/service"
)

func TestPlatformContextUsesSeparateDefinitionAndExactDelegation(t *testing.T) {
	snapshot, err := platform.CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := snapshot.Context()
	if err != nil {
		t.Fatal(err)
	}
	// The reader supplies a different active revision from the embedded release.
	stored.Revision, stored.Digest = 9, strings.Repeat("a", 64)
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
		{"invalid_capability", nil, "/platform/capabilities/Transfer.task.create", true, 400},
		{"not_ready", nil, "/platform/capabilities/transfer.task.create", false, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.semantic.read"})
			if tc.mutate != nil {
				tc.mutate(&a)
			}
			f := &fakeCommands{platformContext: stored}
			w := perform(testRouter(t, f, a, tc.ready), "GET", tc.path, "", "addp_at_test", "en")
			if w.Code != tc.want || f.calls != 0 {
				t.Fatalf("status=%d body=%s Tenant reads=%d", w.Code, w.Body, f.calls)
			}
			wantCalls := 0
			if tc.want == 200 || tc.want == 404 {
				wantCalls = 1
			}
			if f.platformCalls != wantCalls {
				t.Fatalf("platform calls=%d want=%d", f.platformCalls, wantCalls)
			}
			if w.Code == 200 {
				var context platform.Context
				if err := json.Unmarshal(w.Body.Bytes(), &context); err != nil {
					t.Fatal(err)
				}
				if context.KnowledgeKind != "platform_definition" || context.Availability != "not_observed" || len(context.Digest) != 64 {
					t.Fatalf("context=%+v", context)
				}
				if !reflect.DeepEqual(context, stored) || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("API did not consume the active PG reader")
				}
			}
		})
	}
}

func TestPlatformCatalogBoundaryAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*authorization.AuthContext)
		query  string
		err    error
		ready  bool
		want   int
	}{
		{"empty_user", nil, "", nil, true, 200},
		{"delegated", func(a *authorization.AuthContext) {
			delegatedSemantic(a)
			a.Client.Scopes = []string{"platform.capabilities.list"}
		}, "", nil, true, 200},
		{"wrong_scope", delegatedSemantic, "", nil, true, 403},
		{"no_permission", func(a *authorization.AuthContext) {
			a.Authorization.RoleAssignments[0].Permissions = []string{"ontology.revision.read"}
		}, "", nil, true, 403},
		{"query", nil, "?capability=transfer.task.create", nil, true, 400},
		{"corrupt", nil, "", repository.ErrIntegrity, true, 500},
		{"oversize", nil, "", service.ErrResultTooLarge, true, 413},
		{"not_ready", nil, "", nil, false, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.semantic.read"})
			if tc.mutate != nil {
				tc.mutate(&a)
			}
			f := &fakeCommands{platformErr: tc.err}
			w := perform(testRouter(t, f, a, tc.ready), "GET", "/platform/capabilities"+tc.query, "", "addp_at_test", "en")
			if w.Code != tc.want || f.calls != 0 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			wantCalls := 0
			if tc.want == 200 || tc.err != nil {
				wantCalls = 1
			}
			if f.platformCalls != wantCalls {
				t.Fatal("unauthorized catalog read")
			}
			if tc.ready && tc.want != 403 && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cached platform catalog")
			}
			if tc.want == 200 && !strings.Contains(w.Body.String(), `"capabilities":[]`) {
				t.Fatal("empty catalog must be an array")
			}
			if tc.err != nil && strings.Contains(w.Body.String(), "capabilities") {
				t.Fatal("partial catalog leaked")
			}
		})
	}
}

func TestPlatformContextReaderErrorsRemainExplicit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"absent", repository.ErrNotFound, 404},
		{"unpublished", repository.ErrNotActive, 409},
		{"corrupt", repository.ErrIntegrity, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := authtest.NewTenantUserAuthContext("101", "9", []string{"ontology.semantic.read"})
			f := &fakeCommands{platformErr: tc.err}
			w := perform(testRouter(t, f, a, true), "GET", "/platform/capabilities/transfer.task.create", "", "addp_at_test", "en")
			if w.Code != tc.status || f.platformCalls != 1 || f.calls != 0 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "platform_definition") {
				t.Fatalf("unexpected fallback: %d %s", w.Code, w.Body)
			}
		})
	}
}
