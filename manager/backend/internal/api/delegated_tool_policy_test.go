package api

import (
	"reflect"
	"testing"

	managerauthorization "github.com/addp/manager/internal/authorization"
)

func TestManagerDelegatedToolPoliciesMatchPublishedToolPermissions(t *testing.T) {
	policy := managerDelegatedToolPolicies()["GET /api/v1/manager/preview"]

	if !reflect.DeepEqual(policy.RequiredScopes, []string{"data.preview"}) {
		t.Fatalf("data.preview scopes = %#v", policy.RequiredScopes)
	}
	if !reflect.DeepEqual(policy.RequiredPermissions, []string{managerauthorization.PermissionManagerDataItemRead}) {
		t.Fatalf("data.preview permissions = %#v", policy.RequiredPermissions)
	}
}
