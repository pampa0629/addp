package iam

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
)

func TestManagerProfileScopeRejectsIncompleteSourcesAndFreezesWholeConfig(t *testing.T) {
	path := plugin.TabularItemPath(12, "schema", "业务.域", " 表/名 ")
	readSet, err := plugin.NewQueryReadSet(path)
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"config_version": "data-profile-config/v6", "engine_id": 12, "read_set": readSet,
		"budget": map[string]any{"sample_size": 10, "timeout_ms": 1000}, "condition": json.Number("9007199254740993")}
	raw, _ := json.Marshal(config)
	base, err := managerProfileScope(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(map[string]any){
		func(c map[string]any) { c["engine_id"] = 13 },
		func(c map[string]any) { delete(c, "read_set") },
		func(c map[string]any) { c["read_set"] = map[string]any{"paths": readSet.Paths, "extra": true} },
		func(c map[string]any) {
			c["read_set"] = plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{path, path}}
		},
		func(c map[string]any) {
			c["read_set"] = plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{plugin.TabularNamespacePath(12, "schema", "public")}}
		},
		func(c map[string]any) { c["config_version"] = "data-profile-config/v5" },
	} {
		var copy map[string]any
		_ = json.Unmarshal(raw, &copy)
		mutate(copy)
		invalid, _ := json.Marshal(copy)
		if scope, err := managerProfileScope(invalid); scope != nil || err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	for _, mutate := range []func(map[string]any){
		func(c map[string]any) { c["budget"] = map[string]any{"sample_size": 11, "timeout_ms": 1000} },
		func(c map[string]any) { c["condition"] = json.Number("9007199254740992") },
		func(c map[string]any) { c["selection"] = "changed" },
	} {
		var copy map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		_ = decoder.Decode(&copy)
		mutate(copy)
		changed, _ := json.Marshal(copy)
		scope, err := managerProfileScope(changed)
		if err != nil || scope.ConfigDigest == base.ConfigDigest {
			t.Fatalf("operation change not bound: %s %v", changed, err)
		}
	}
	cloned := base.Clone()
	cloned.ReadSet.Paths[0].Segments[2].Name = "replacement"
	if base.ReadSet.Paths[0].Segments[2].Name != " 表/名 " {
		t.Fatal("scope aliases caller")
	}
}

func TestManagerProfilePermissionsRequireBothTenantFunctions(t *testing.T) {
	rows := []RoleAssignmentPermissionProjection{{ScopeType: "tenant", PermissionKey: "manager.data_profile.execute"},
		{ScopeType: "tenant", PermissionKey: "manager.data_item.read"}}
	if !managerProfilePermissions(rows) {
		t.Fatal("exact functions rejected")
	}
	if managerProfilePermissions(rows[:1]) {
		t.Fatal("execute alone became source read permission")
	}
	rows[1].ScopeType = "department"
	if managerProfilePermissions(rows) {
		t.Fatal("organization scope became Tenant function permission")
	}
}

func TestManagerProfileUserCredentialRejectsUnrelatedCredentialsAndScopes(t *testing.T) {
	ordinary := func() authorization.AuthContext {
		tenant := "12"
		return authorization.AuthContext{
			Principal: authorization.AuthPrincipal{Type: "user"},
			Context:   authorization.AuthSessionContext{Type: "tenant", TenantID: &tenant},
			Token:     authorization.TokenFacts{Type: "first_party_access_token"},
			Client:    authorization.ClientConstraints{Audiences: []string{"addp.api"}, ScopeMode: "unrestricted"},
			Authorization: authorization.AuthorizationFacts{RoleAssignments: []authorization.RoleAssignment{{
				Scope:       authorization.AssignmentScope{Type: "tenant", TenantID: &tenant},
				Permissions: []string{"manager.data_profile.execute", "manager.data_item.read"},
			}}},
		}
	}
	firstParty := ordinary()
	if !managerProfileUserCredential(&firstParty) {
		t.Fatal("ordinary API User rejected")
	}
	oauth := ordinary()
	oauth.Token.Type, oauth.Client.ScopeMode, oauth.Client.Scopes = "oauth_access_token", "restricted", []string{"addp.api"}
	if !managerProfileUserCredential(&oauth) {
		t.Fatal("OAuth API User rejected")
	}
	for name, mutate := range map[string]func(*authorization.AuthContext){
		"machine":         func(c *authorization.AuthContext) { c.Principal.Type = "service_principal" },
		"platform":        func(c *authorization.AuthContext) { c.Context.Type = "platform" },
		"resource ticket": func(c *authorization.AuthContext) { c.Token.Type = "resource_access_ticket" },
		"delegated":       func(c *authorization.AuthContext) { c.Delegation = &authorization.DelegationFacts{} },
		"tool only": func(c *authorization.AuthContext) {
			c.Token.Type, c.Client.ScopeMode, c.Client.Scopes = "oauth_access_token", "restricted", []string{"data.preview"}
		},
		"wrong audience": func(c *authorization.AuthContext) { c.Client.Audiences = []string{"manager"} },
		"extra audience": func(c *authorization.AuthContext) { c.Client.Audiences = []string{"addp.api", "manager"} },
		"missing read": func(c *authorization.AuthContext) {
			c.Authorization.RoleAssignments[0].Permissions = []string{"manager.data_profile.execute"}
		},
		"department scope": func(c *authorization.AuthContext) { c.Authorization.RoleAssignments[0].Scope.Type = "department" },
		"foreign tenant": func(c *authorization.AuthContext) {
			other := "13"
			c.Authorization.RoleAssignments[0].Scope.TenantID = &other
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := ordinary()
			mutate(&candidate)
			if managerProfileUserCredential(&candidate) {
				t.Fatal("unrelated credential or function scope accepted")
			}
		})
	}
	if managerProfileUserCredential(nil) {
		t.Fatal("missing credential accepted")
	}
}
