package iam

import (
	"encoding/json"
	"github.com/addp/common/authorization"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
)

func TestManagerSourceScopeClosedPurposes(t *testing.T) {
	paths, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(7, "schema", "public", "metrics"))
	profile := &execution.ManagerProfileReadScope{ConfigDigest: strings.Repeat("a", 64), ReadSet: *paths}
	composition, err := authorizationfacts.NewFileCompositionBoundary("3ds", []authorizationfacts.FileCompositionRef{
		{Target: plugin.FileItemPath(7, "models/scene.3ds"), Role: "model", Required: true, Primary: true},
		{Target: plugin.FileItemPath(7, "models/A.PNG"), Role: "texture", Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	model := &execution.ManagerModel3DReadScope{ConfigDigest: strings.Repeat("b", 64), OwnerTaskID: "1", OwnerTaskVersion: "2", EngineVersion: "3", FileComposition: *composition}
	for _, candidate := range []*ManagerSourceReadScope{NewManagerProfileSourceScope(profile), NewManagerModel3DSourceScope(model)} {
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		var decoded ManagerSourceReadScope
		if json.Unmarshal(raw, &decoded) != nil || !reflect.DeepEqual(candidate, &decoded) {
			t.Fatal("strict scope roundtrip failed")
		}
		clone := decoded.Clone()
		targets, err := clone.Targets()
		if err != nil || len(targets) != 1 || targets[0].EngineID != 7 {
			t.Fatal("wrong exact primary scope")
		}
		targets[0].EngineID = 8
		again, _ := decoded.Targets()
		if again[0].EngineID != 7 {
			t.Fatal("returned targets mutated stored scope")
		}
		for _, invalid := range []string{`null`, `{}`, strings.TrimSuffix(string(raw), "}") + `,"extra":true}`, strings.TrimSuffix(string(raw), "}") + `,"read_set":null}`, strings.TrimSuffix(string(raw), "}") + `,"config_digest":"` + strings.Repeat("f", 64) + `"}`} {
			if json.Unmarshal([]byte(invalid), new(ManagerSourceReadScope)) == nil {
				t.Fatalf("invalid scope accepted: %s", invalid)
			}
		}
	}
	if (&ManagerSourceReadScope{profile: profile, model3d: model}).Validate() == nil || NewManagerProfileSourceScope(profile).Model3D() != nil || NewManagerModel3DSourceScope(model).Profile() != nil {
		t.Fatal("cross-purpose source scope accepted")
	}
	expected, _ := json.Marshal(profile)
	actual, _ := json.Marshal(NewManagerProfileSourceScope(profile))
	if string(expected) != string(actual) {
		t.Fatal("profile canonical wire changed")
	}
}

func TestManagerSourcePurposesDoNotExchangeFunctionPermissions(t *testing.T) {
	tenant := "3"
	for _, purpose := range []managerSourcePurpose{managerSourceProfile, managerSourceModel3D} {
		auth := authorization.AuthContext{Principal: authorization.AuthPrincipal{Type: "user"}, Context: authorization.AuthSessionContext{Type: "tenant", TenantID: &tenant}, Token: authorization.TokenFacts{Type: "first_party_access_token"}, Client: authorization.ClientConstraints{Audiences: []string{"addp.api"}, ScopeMode: "unrestricted"}, Authorization: authorization.AuthorizationFacts{RoleAssignments: []authorization.RoleAssignment{{Scope: authorization.AssignmentScope{Type: "tenant", TenantID: &tenant}, Permissions: []string{purpose.permission(), "manager.data_item.read"}}}}}
		if !managerSourceUserCredential(&auth, purpose) {
			t.Fatal("exact purpose permission rejected")
		}
		other := managerSourceModel3D
		if purpose == other {
			other = managerSourceProfile
		}
		if managerSourceUserCredential(&auth, other) || managerSourceUserCredential(&auth, managerSourcePurpose("unknown")) {
			t.Fatal("another purpose reused function permission")
		}
		auth.Authorization.RoleAssignments[0].Permissions = []string{purpose.permission()}
		if managerSourceUserCredential(&auth, purpose) {
			t.Fatal("creation/execution alone became data read permission")
		}
	}
}
