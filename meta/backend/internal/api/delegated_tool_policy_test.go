package api

import (
	"reflect"
	"testing"

	metaauthorization "github.com/addp/meta/internal/authorization"
)

func TestMetaDelegatedToolPoliciesMatchPublishedToolPermissions(t *testing.T) {
	policies := metaDelegatedToolPolicies()
	for _, route := range []struct {
		path  string
		scope string
	}{
		{path: "GET /api/v1/meta/resource-tree/:engine_id/node", scope: "resource.children.list"},
		{path: "GET /api/v1/meta/resource-tree/:engine_id/ancestors", scope: "resource.ancestors.get"},
	} {
		policy := policies[route.path]
		if !reflect.DeepEqual(policy.RequiredScopes, []string{route.scope}) {
			t.Fatalf("%s scopes = %#v", route.scope, policy.RequiredScopes)
		}
		if !reflect.DeepEqual(policy.RequiredPermissions, []string{metaauthorization.PermissionMetaCatalogRead}) {
			t.Fatalf("%s permissions = %#v", route.scope, policy.RequiredPermissions)
		}
	}
}
