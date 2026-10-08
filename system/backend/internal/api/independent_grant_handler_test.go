package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/google/uuid"
)

type independentGrantTestService struct {
	create func(engineaccess.CreateIndependentGrantInput) (*engineaccess.SourceGrantView, error)
	list   func(engineaccess.Actor, int64, int, int) ([]engineaccess.SourceGrantView, int64, error)
}

func (s independentGrantTestService) CreateIndependentGrant(_ context.Context, input engineaccess.CreateIndependentGrantInput) (*engineaccess.SourceGrantView, error) {
	return s.create(input)
}
func (s independentGrantTestService) ListSourceGrants(_ context.Context, a engineaccess.Actor, id int64, page, size int) ([]engineaccess.SourceGrantView, int64, error) {
	return s.list(a, id, page, size)
}
func (s independentGrantTestService) RevokeGrant(context.Context, engineaccess.RevokeGrantInput) (*engineaccess.GrantRevocation, error) {
	panic("unexpected revocation")
}

func TestIndependentGrantHTTPContract(t *testing.T) {
	const path = "/api/v1/system/engines/1/access_grants"
	projection := testIAMActorContext("tenant")
	permission := func(keys ...string) {
		projection.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.direct_grant", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: shared.AssignmentScope{Type: "tenant", TenantID: projection.Context.TenantID}, Permissions: keys}}
	}
	unused := independentGrantTestService{create: func(engineaccess.CreateIndependentGrantInput) (*engineaccess.SourceGrantView, error) {
		t.Fatal("invalid request reached service")
		return nil, nil
	}, list: func(engineaccess.Actor, int64, int, int) ([]engineaccess.SourceGrantView, int64, error) {
		t.Fatal("invalid request reached service")
		return nil, 0, nil
	}}
	permission("system.engine_access_fulfillment.create", "system.engine_access_grant.revoke")
	for _, method := range []string{"POST", "GET"} {
		engineDelegationTestRequest(t, grantTestRouter(t, &projection, unused), method, path, nil, 403)
	}
	permission("system.engine_access_grant.create", "system.engine_access_grant.read")
	id := uuid.New()
	body := func() map[string]any {
		return map[string]any{"request_id": id.String(), "catalog_path": engineplugin.TabularItemPath(1, "schema", "public", "orders"), "requirement_version": "1", "recipient_type": "user", "recipient_id": "9007199254740993", "action": "read", "expiry_mode": "until_revoked", "reason": "Explicit read access"}
	}
	for _, mutate := range []func(map[string]any){
		func(b map[string]any) { b["tenant_id"] = "2" }, func(b map[string]any) { b["operator_id"] = "2" },
		func(b map[string]any) { b["recipient_id"] = 9007199254740993 }, func(b map[string]any) { b["recipient_id"] = "01" },
		func(b map[string]any) { b["requirement_version"] = 1 }, func(b map[string]any) { delete(b, "requirement_version") },
		func(b map[string]any) { b["request_id"] = uuid.Nil.String() },
	} {
		b := body()
		mutate(b)
		engineDelegationTestRequest(t, grantTestRouter(t, &projection, unused), "POST", path, b, 400)
	}
	engineDelegationTestRequest(t, grantTestRouter(t, &projection, unused), "POST", path+"?page=1", body(), 400)
	for _, suffix := range []string{"?page=0", "?page=01", "?page_size=101", "?page=1&page=2", "?tenant_id=2", "?page=1;page_size=2", "?unknown=1"} {
		engineDelegationTestRequest(t, grantTestRouter(t, &projection, unused), "GET", path+suffix, nil, 400)
	}
	qualified := independentGrantTestService{create: func(input engineaccess.CreateIndependentGrantInput) (*engineaccess.SourceGrantView, error) {
		if input.RecipientID != 9007199254740993 || input.EngineID != 1 || input.RequestID != id || input.Actor.PrincipalID <= 0 || input.Actor.MembershipID <= 0 || input.RequirementVersion != 1 {
			t.Fatalf("lost provenance: %+v", input)
		}
		return &engineaccess.SourceGrantView{RequestID: id, RecipientID: input.RecipientID, RequirementVersion: input.RequirementVersion, ApprovalMode: "independent"}, nil
	}, list: func(a engineaccess.Actor, engineID int64, page, size int) ([]engineaccess.SourceGrantView, int64, error) {
		if a.PrincipalID <= 0 || engineID != 1 || page != 2 || size != 10 {
			t.Fatalf("lost list scope: %+v %d %d %d", a, engineID, page, size)
		}
		return []engineaccess.SourceGrantView{{RequestID: id, RecipientID: 9007199254740993}}, 11, nil
	}}
	response := engineDelegationTestRequest(t, grantTestRouter(t, &projection, qualified), "POST", path, body(), 201)
	var fields map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || fields["recipient_id"] != "9007199254740993" || fields["requirement_version"] != "1" {
		t.Fatalf("lossy IDs: %s %v", response.Body.String(), err)
	}
	first := body()
	first["initialize_approval"] = true
	initializer := independentGrantTestService{create: func(input engineaccess.CreateIndependentGrantInput) (*engineaccess.SourceGrantView, error) {
		if !input.InitializeApproval {
			t.Fatal("lost first-authorization intent")
		}
		return &engineaccess.SourceGrantView{RequestID: id}, nil
	}}
	engineDelegationTestRequest(t, grantTestRouter(t, &projection, initializer), "POST", path, first, 201)
	engineDelegationTestRequest(t, grantTestRouter(t, &projection, qualified), "GET", path+"?page=2&page_size=10", nil, 200)
	for _, failure := range []struct {
		err    error
		status int
	}{{commonapi.ErrForbidden, 403}, {engineaccess.ErrIndependentGrantConflict, 409}, {engineaccess.ErrIndependentGrantBasis, 409}, {engineaccess.ErrIndependentGrantExpiry, 409}, {engineaccess.ErrIndependentGrantSourceUnavailable, 503}} {
		s := independentGrantTestService{create: func(engineaccess.CreateIndependentGrantInput) (*engineaccess.SourceGrantView, error) {
			return nil, failure.err
		}}
		engineDelegationTestRequest(t, grantTestRouter(t, &projection, s), http.MethodPost, path, body(), failure.status)
	}
}
