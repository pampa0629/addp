package api

import (
	"net/http/httptest"
	"testing"
)

func TestNewPlatformMetricsRequireBothPermissionsAndStrictQuery(t *testing.T) {
	for _, tc := range []struct {
		path       string
		permission string
	}{
		{"engine_runtime_observations?source=meilisearch&endpoint=localhost", "platform.engine_deployment.read"},
		{"process_resource_totals?module_name=meta", "platform.module.read"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, withOwner := range []bool{false, true} {
				ctx := targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.resource_observation.read")
				want := 403
				if withOwner {
					want = 400
					owner := &ctx.Authorization.RoleAssignments[0].Permissions
					*owner = append(*owner, tc.permission)
				}
				req := httptest.NewRequest("GET", "/api/v1/monitor/platform/"+tc.path, nil)
				req.Header.Set("Authorization", "Bearer addp_at_user")
				res := httptest.NewRecorder()
				resourceAPIRouter(t, ctx).ServeHTTP(res, req)
				if res.Code != want {
					t.Fatalf("owner permission %t: status %d, want %d", withOwner, res.Code, want)
				}
			}
		})
	}
}
