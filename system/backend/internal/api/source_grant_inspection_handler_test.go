package api

import (
	"encoding/json"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
)

func TestSourceGrantInspectionHTTPContract(t *testing.T) {
	const route = "/api/v1/system/engines/1/access_grants/inspection"
	projection := testIAMActorContext("tenant")
	path := engineplugin.TabularItemPath(1, "schema", "public", "orders")
	service := independentGrantTestService{inspect: func(actor engineaccess.Actor, engineID, accountID int64, target engineplugin.EngineCatalogPath) (*engineaccess.SourceGrantInspection, error) {
		if actor.PrincipalID <= 0 || actor.MembershipID <= 0 || engineID != 1 || accountID != 9007199254740993 || target.EngineID != 1 {
			t.Fatal("lost management actor or selected object")
		}
		return &engineaccess.SourceGrantInspection{AccountID: accountID, CatalogPath: target, ObservedAt: time.Now().UTC(), Reason: "explicit_deny", Sources: []engineaccess.SourceGrantInspectionSource{{RecipientType: "user", RecipientID: accountID, GrantCount: 1, ExpiryMode: "until_revoked"}}}, nil
	}}
	body := func() map[string]any { return map[string]any{"account_id": "9007199254740993", "catalog_path": path} }
	engineDelegationTestRequest(t, grantTestRouter(t, &projection, service), "POST", route, body(), 403)
	projection.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.inspect", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: shared.AssignmentScope{Type: "tenant", TenantID: projection.Context.TenantID}, Permissions: []string{"system.engine_access_grant.read"}}}
	for _, mutate := range []func(map[string]any){
		func(b map[string]any) { b["account_id"] = 33 }, func(b map[string]any) { b["account_id"] = "01" },
		func(b map[string]any) { b["account_id"] = "0" }, func(b map[string]any) { b["account_id"] = "9223372036854775808" },
		func(b map[string]any) { delete(b, "account_id") }, func(b map[string]any) { b["tenant_id"] = "2" },
		func(b map[string]any) { b["authorization_version"] = "1" }, func(b map[string]any) { b["action"] = "write" },
	} {
		invalid := body()
		mutate(invalid)
		engineDelegationTestRequest(t, grantTestRouter(t, &projection, service), "POST", route, invalid, 400)
	}
	engineDelegationTestRequest(t, grantTestRouter(t, &projection, service), "POST", route+"?account_id=33", body(), 400)
	response := engineDelegationTestRequest(t, grantTestRouter(t, &projection, service), "POST", route, body(), 200)
	var fields map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || fields["account_id"] != "9007199254740993" || fields["rule_covered"] != false || fields["reason"] != "explicit_deny" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid observation: %s", response.Body.String())
	}
	if len(fields) != 6 {
		t.Fatalf("unexpected exposure: %s", response.Body.String())
	}
	for _, status := range []int{403, 404} {
		service.inspect = func(engineaccess.Actor, int64, int64, engineplugin.EngineCatalogPath) (*engineaccess.SourceGrantInspection, error) {
			if status == 403 {
				return nil, commonapi.ErrForbidden
			}
			return nil, commonapi.ErrNotFound
		}
		engineDelegationTestRequest(t, grantTestRouter(t, &projection, service), "POST", route, body(), status)
	}
}
