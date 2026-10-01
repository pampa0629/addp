package api

import (
	"context"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/engineaccess"
)

type unusedDelegationService struct{}

func (unusedDelegationService) List(context.Context, int64, int64, int, int) ([]engineaccess.View, int64, error) {
	panic("denied request reached service")
}
func (unusedDelegationService) Get(context.Context, int64, int64, int64) (*engineaccess.View, error) {
	panic("denied request reached service")
}
func (unusedDelegationService) Create(context.Context, engineaccess.CreateInput) (*engineaccess.View, error) {
	panic("denied request reached service")
}
func (unusedDelegationService) Revoke(context.Context, engineaccess.RevokeInput) (*engineaccess.View, error) {
	panic("denied request reached service")
}

func TestEngineDelegationRoutesRejectUnqualifiedContextsBeforeService(t *testing.T) {
	const path = "/api/v1/system/engines/1/access_delegations"
	for _, tc := range []struct {
		name string
		auth commonauth.AuthContext
	}{
		{"tenant without permission", testIAMActorContext("tenant")},
		{"platform user", testIAMActorContext("platform")},
		{"tenant service", testIAMServiceActorContext("tenant", "addp-meta")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := engineDelegationTestRouter(t, unusedDelegationService{}, &tc.auth)
			engineDelegationTestRequest(t, router, "GET", path, nil, 403)
			engineDelegationTestRequest(t, router, "POST", path, nil, 403)
			engineDelegationTestRequest(t, router, "GET", path+"/1", nil, 403)
			engineDelegationTestRequest(t, router, "POST", path+"/1/revoke", nil, 403)
		})
	}
	projection := testIAMActorContext("tenant")
	projection.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.delegator", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute),
		Scope:       commonauth.AssignmentScope{Type: "tenant", TenantID: projection.Context.TenantID},
		Permissions: []string{"system.engine_access_delegation.create", "system.engine_access_delegation.revoke"}}}
	router := engineDelegationTestRouter(t, unusedDelegationService{}, &projection)
	engineDelegationTestRequest(t, router, "POST", path+"?tenant_id=3", nil, 400)
	engineDelegationTestRequest(t, router, "POST", path, map[string]any{"tenant_id": "3"}, 400)
	engineDelegationTestRequest(t, router, "POST", path, map[string]any{"tenant_membership_id": "01"}, 400)
	engineDelegationTestRequest(t, router, "POST", path+"/1/revoke?tenant_id=3", nil, 400)
	engineDelegationTestRequest(t, router, "POST", path+"/1/revoke", map[string]any{"version": 0, "reason": "invalid"}, 400)
	// History has no mutation, recovery, deletion or legacy alternate route.
	for _, operation := range []struct{ method, suffix string }{{"PUT", "/1"}, {"PATCH", "/1"}, {"DELETE", "/1"}, {"POST", "/1/restore"}} {
		engineDelegationTestRequest(t, router, operation.method, path+operation.suffix, nil, 404)
	}
}
