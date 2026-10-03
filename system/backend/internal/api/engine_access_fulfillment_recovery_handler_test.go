package api

import (
	"context"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type recoveryHandlerFixture struct {
	calls int
	actor engineaccess.FulfillmentRuntimeActor
}

func (f *recoveryHandlerFixture) ResolveFulfillment(_ context.Context, actor engineaccess.FulfillmentRuntimeActor, _ uuid.UUID, _ shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentLookup, error) {
	f.calls++
	f.actor = actor
	return &shared.SharingFulfillmentLookup{}, nil
}
func (f *recoveryHandlerFixture) CloseFulfillment(_ context.Context, actor engineaccess.FulfillmentRuntimeActor, id uuid.UUID, b shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentResolution, error) {
	f.calls++
	f.actor = actor
	return &shared.SharingFulfillmentResolution{RequestID: id, TenantID: actor.TenantID, Binding: b, Outcome: "closed", RecordedAt: time.Now()}, nil
}

func TestFulfillmentRecoveryRoutesUseServiceIdentityAndStrictBinding(t *testing.T) {
	ac := testIAMServiceActorContext("tenant", "addp-catalog")
	clientID := "addp-catalog"
	ac.Client.ClientID = &clientID
	ac.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.catalog_runtime", SourceType: "bootstrap", ValidFrom: time.Now().Add(-time.Minute),
		Scope: shared.AssignmentScope{Type: "tenant", TenantID: ac.Context.TenantID}, Permissions: []string{engineaccess.FulfillmentReconcilePermission}}}
	authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &ac})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeServiceAccess)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &recoveryHandlerFixture{}
	router := gin.New()
	if err := RegisterEngineAccessFulfillmentRecoveryRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, ServiceCredential: credential}, &EngineAccessFulfillmentRecoveryHandler{service: fixture}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterEngineAccessFulfillmentAcceptanceRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, ServiceCredential: credential}, &EngineAccessFulfillmentAcceptanceHandler{service: fixture}); err != nil {
		t.Fatal(err)
	}
	binding := shared.SharingFulfillmentBinding{CallerPrincipalID: 41, Operator: shared.SharingFulfillmentOperator{PrincipalID: 3, MembershipID: 4, AuthorizationVersion: 5},
		Path: plugin.TabularItemPath(12, "schema", "public", "fixture"), DecisionID: uuid.New(), RequirementVersion: 1, RecipientType: "user", RecipientID: 7, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked}
	path := "/api/v1/system/runtime/engine-access-fulfillments/" + uuid.NewString()
	engineDelegationTestRequest(t, router, "POST", path+"/resolve", binding, 200)
	engineDelegationTestRequest(t, router, "POST", path+"/close", binding, 200)
	engineDelegationTestRequest(t, router, "POST", path+"/accept", binding, 200)
	if fixture.calls != 3 || fixture.actor.ClientID != "addp-catalog" || fixture.actor.TenantID != 3 || fixture.actor.PrincipalID != 41 || fixture.actor.MembershipID != 4 || fixture.actor.AuthorizationVersion != 1 || !fixture.actor.TokenExpiresAt.Equal(ac.Token.ExpiresAt) {
		t.Fatalf("calls=%d actor=%+v", fixture.calls, fixture.actor)
	}
	for _, suffix := range []string{"/resolve", "/close", "/accept"} {
		engineDelegationTestRequest(t, router, "POST", path+suffix+"?tenant_id=2", binding, 400)
		engineDelegationTestRequest(t, router, "POST", path+suffix, map[string]any{"tenant_id": "2"}, 400)
		forged := binding
		forged.CallerPrincipalID++
		engineDelegationTestRequest(t, router, "POST", path+suffix, forged, 403)
	}
	clientID = "addp-meta"
	engineDelegationTestRequest(t, router, "POST", path+"/resolve", binding, 403)
	engineDelegationTestRequest(t, router, "POST", path+"/accept", binding, 403)
	clientID = "addp-catalog"
	ac.Authorization.RoleAssignments = []shared.RoleAssignment{}
	engineDelegationTestRequest(t, router, "POST", path+"/close", binding, 403)
	engineDelegationTestRequest(t, router, "POST", path+"/accept", binding, 403)
	if fixture.calls != 3 {
		t.Fatal("denied request reached service")
	}
}

func (f *recoveryHandlerFixture) AcceptFulfillment(_ context.Context, actor engineaccess.FulfillmentRuntimeActor, id uuid.UUID, b shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentResolution, error) {
	f.calls++
	f.actor = actor
	now := time.Now()
	deadline := now.Add(5 * time.Minute)
	return &shared.SharingFulfillmentResolution{RequestID: id, TenantID: actor.TenantID, Binding: b, Outcome: "accepted", RecordedAt: now, Deadline: &deadline}, nil
}
