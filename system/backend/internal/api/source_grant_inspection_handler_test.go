package api

import (
	"encoding/json"
	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"testing"
	"time"
)

func TestAccountSourceGrantListHTTPContract(t *testing.T) {
	const route = "/api/v1/system/engines/1/access_grants?recipient_type=user&recipient_id=9007199254740993"
	projection := testIAMActorContext("tenant")
	path := engineplugin.TabularItemPath(1, "schema", "public", "orders")
	encoded, _ := json.Marshal(path)
	service := independentGrantTestService{relations: func(actor engineaccess.Actor, engineID int64, page, size int, filter engineaccess.SourceGrantFilter) ([]engineaccess.SourceGrantRelation, int64, error) {
		if actor.PrincipalID <= 0 || actor.MembershipID <= 0 || engineID != 1 || filter.RecipientID != 9007199254740993 || filter.RecipientType != "user" {
			t.Fatal("lost management actor or selected object")
		}
		return []engineaccess.SourceGrantRelation{{CatalogPath: encoded, RecipientType: "user", RecipientID: filter.RecipientID,
			Inspection: &engineaccess.SourceGrantInspection{AccountID: filter.RecipientID, CatalogPath: path, ObservedAt: time.Now().UTC(), Reason: "explicit_deny",
				Sources: []engineaccess.SourceGrantInspectionSource{{RecipientType: "department", RecipientID: 42, GrantCount: 1, ExpiryMode: "until_revoked"}}}}}, 1, nil
	}}
	engineDelegationTestRequest(t, grantTestRouter(t, &projection, service), "GET", route, nil, 403)
	projection.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.inspect", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: shared.AssignmentScope{Type: "tenant", TenantID: projection.Context.TenantID}, Permissions: []string{"system.engine_access_grant.read"}}}
	response := engineDelegationTestRequest(t, grantTestRouter(t, &projection, service), "GET", route, nil, 200)
	var fields struct {
		Data  []engineaccess.SourceGrantRelation
		Total int64
	}
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || fields.Total != 1 || len(fields.Data) != 1 || fields.Data[0].Inspection.AccountID != 9007199254740993 || fields.Data[0].Inspection.Reason != "explicit_deny" || fields.Data[0].Inspection.RuleCovered || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid observation: %s", response.Body.String())
	}
	engineDelegationTestRequest(t, grantTestRouter(t, &projection, service), "POST", "/api/v1/system/engines/1/access_grants/inspection", nil, 404)
	for _, status := range []int{403, 404} {
		service.relations = func(engineaccess.Actor, int64, int, int, engineaccess.SourceGrantFilter) ([]engineaccess.SourceGrantRelation, int64, error) {
			if status == 403 {
				return nil, 0, commonapi.ErrForbidden
			}
			return nil, 0, commonapi.ErrNotFound
		}
		engineDelegationTestRequest(t, grantTestRouter(t, &projection, service), "GET", route, nil, status)
	}
}
