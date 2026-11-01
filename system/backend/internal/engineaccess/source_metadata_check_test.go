package engineaccess

import (
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
)

func TestMetaQueryCredentialDoesNotBorrowPreviewOrMachineAuthority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*shared.AuthContext)
		allowed bool
	}{
		{"ordinary metadata reader", func(*shared.AuthContext) {}, true},
		{"preview permission is not metadata function", func(a *shared.AuthContext) {
			a.Authorization.RoleAssignments[0].Permissions = []string{ManagerPreviewReadPermission}
		}, false},
		{"machine", func(a *shared.AuthContext) {
			a.Principal.Type = "service_principal"
			a.Token.Type = "service_access_token"
		}, false},
		{"resource ticket", func(a *shared.AuthContext) { a.Token.Type = "resource_access_ticket" }, false},
		{"delegate not admitted", func(a *shared.AuthContext) { a.Token.Type = "delegated_access_token" }, false},
		{"OAuth API", func(a *shared.AuthContext) {
			a.Token.Type = "oauth_access_token"
			a.Client.ScopeMode = "restricted"
			a.Client.Scopes = []string{"addp.api"}
		}, true},
		{"OAuth other scope", func(a *shared.AuthContext) {
			a.Token.Type = "oauth_access_token"
			a.Client.ScopeMode = "restricted"
			a.Client.Scopes = []string{"data.preview"}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := previewCheckContext()
			a.Authorization.RoleAssignments[0].Permissions = []string{MetaCatalogReadPermission}
			tc.mutate(&a)
			err := qualifyMetaMetadataRead(a)
			if tc.allowed && err != nil || !tc.allowed && !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("allowed=%t err=%v", tc.allowed, err)
			}
		})
	}
}

func TestMetaQueryDelegationIsExactExistingToolOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*shared.AuthContext)
		allowed bool
	}{
		{"children", func(*shared.AuthContext) {}, true},
		{"ancestors", func(a *shared.AuthContext) { a.Client.Scopes = []string{"resource.ancestors.get"} }, true},
		{"Manager audience", func(a *shared.AuthContext) { a.Client.Audiences = []string{"manager"} }, false},
		{"extra audience", func(a *shared.AuthContext) { a.Client.Audiences = append(a.Client.Audiences, "system") }, false},
		{"other scope", func(a *shared.AuthContext) { a.Client.Scopes = []string{"data.preview"} }, false},
		{"combined scopes", func(a *shared.AuthContext) { a.Client.Scopes = append(a.Client.Scopes, "resource.ancestors.get") }, false},
		{"no binding", func(a *shared.AuthContext) { a.Delegation = nil }, false},
		{"wrong client", func(a *shared.AuthContext) { a.Delegation.DelegatedByClientID = "other" }, false},
		{"missing run", func(a *shared.AuthContext) { a.Delegation.AgentRunID = "" }, false},
		{"missing call", func(a *shared.AuthContext) { a.Delegation.ToolCallID = "" }, false},
		{"no function", func(a *shared.AuthContext) { a.Authorization.RoleAssignments = []shared.RoleAssignment{} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := previewCheckContext()
			a.Authorization.RoleAssignments[0].Permissions = []string{MetaCatalogReadPermission}
			a.Token.Type = "delegated_access_token"
			a.Client.Audiences = []string{"meta"}
			a.Client.ScopeMode = "restricted"
			a.Client.Scopes = []string{"resource.children.list"}
			a.Delegation = &shared.DelegationFacts{DelegatedByClientID: *a.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
			tc.mutate(&a)
			if err := qualifyMetaMetadataRead(a); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
}
