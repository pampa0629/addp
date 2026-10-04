package engineaccess

import (
	"context"
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
	auth "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
)

func previewCheckContext() auth.AuthContext {
	tenant, member, client := "3", "8", "addp-web"
	return auth.AuthContext{Principal: auth.AuthPrincipal{Type: "user", ID: "12"},
		Context: auth.AuthSessionContext{Type: "tenant", TenantID: &tenant, TenantMembershipID: &member},
		Client:  auth.ClientConstraints{ClientID: &client, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted"},
		Token:   auth.TokenFacts{Type: "first_party_access_token"},
		Authorization: auth.AuthorizationFacts{RoleAssignments: []auth.RoleAssignment{{
			Scope: auth.AssignmentScope{Type: "tenant", TenantID: &tenant}, Permissions: []string{ManagerPreviewReadPermission},
		}}},
	}
}

func TestManagerPreviewReadQualification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*auth.AuthContext)
		allowed bool
	}{
		{"first party", func(*auth.AuthContext) {}, true},
		{"OAuth API", func(c *auth.AuthContext) {
			c.Token.Type = "oauth_access_token"
			c.Client.ScopeMode = "restricted"
			c.Client.Scopes = []string{"addp.api"}
		}, true},
		{"OAuth Tool scope cannot expand API", func(c *auth.AuthContext) {
			c.Token.Type = "oauth_access_token"
			c.Client.ScopeMode = "restricted"
			c.Client.Scopes = []string{"data.preview"}
		}, false},
		{"OAuth empty scope", func(c *auth.AuthContext) { c.Token.Type = "oauth_access_token"; c.Client.ScopeMode = "restricted" }, false},
		{"wrong audience", func(c *auth.AuthContext) { c.Client.Audiences = []string{"manager"} }, false},
		{"extra audience", func(c *auth.AuthContext) { c.Client.Audiences = []string{"addp.api", "manager"} }, false},
		{"no function permission", func(c *auth.AuthContext) { c.Authorization.RoleAssignments = nil }, false},
		{"organization permission is not Tenant", func(c *auth.AuthContext) { c.Authorization.RoleAssignments[0].Scope.Type = "department" }, false},
		{"other Tenant assignment", func(c *auth.AuthContext) { other := "4"; c.Authorization.RoleAssignments[0].Scope.TenantID = &other }, false},
		{"inventory is not read", func(c *auth.AuthContext) {
			c.Authorization.RoleAssignments[0].Permissions = []string{"catalog.inventory.read"}
		}, false},
		{"platform", func(c *auth.AuthContext) { c.Context.Type = "platform" }, false},
		{"service", func(c *auth.AuthContext) {
			c.Principal.Type = "service_principal"
			c.Token.Type = "service_access_token"
		}, false},
		{"resource ticket", func(c *auth.AuthContext) { c.Token.Type = "resource_access_ticket" }, false},
		{"delegated", func(c *auth.AuthContext) { c.Token.Type = "delegated_access_token" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := previewCheckContext()
			tc.mutate(&current)
			err := qualifyManagerPreviewRead(current)
			if tc.allowed && err != nil || !tc.allowed && !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("qualification allowed=%t error=%v", tc.allowed, err)
			}
		})
	}
}

func TestManagerPreviewReadCheckValidatesSetBeforeDatabase(t *testing.T) {
	service := NewService(NewRepository(nil), nil)
	path := testFulfillmentRequest().Path
	for _, targets := range [][]engineplugin.EngineCatalogPath{nil,
		{engineplugin.TabularNamespacePath(path.EngineID, "schema", "public")},
		make([]engineplugin.EngineCatalogPath, 201),
	} {
		result, err := service.CheckManagerPreviewRead(context.Background(), "not-a-credential", ManagerPreviewReadCheckRequest{Targets: targets})
		if result != nil || !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("invalid target set reached database: %+v %v", result, err)
		}
	}
}
