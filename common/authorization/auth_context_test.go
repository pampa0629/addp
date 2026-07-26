package authorization

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAuthContextSchemaIsValidJSONAndReturnsCopy(t *testing.T) {
	first := AuthContextSchema()
	if !json.Valid(first) {
		t.Fatal("AuthContextSchema() is not valid JSON")
	}
	first[0] = 'x'
	if second := AuthContextSchema(); !json.Valid(second) {
		t.Fatal("AuthContextSchema() returned shared mutable bytes")
	}
}

func TestValidateAuthContextAcceptsCanonicalTenantContext(t *testing.T) {
	authContext := validTenantAuthContext()
	if err := ValidateAuthContext(authContext); err != nil {
		t.Fatalf("ValidateAuthContext() error = %v", err)
	}

	encoded, err := json.Marshal(authContext)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAuthContext(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodeAuthContext() error = %v", err)
	}
	if decoded.SchemaVersion != AuthContextSchemaVersion || decoded.Context.TenantID == nil || *decoded.Context.TenantID != "3" {
		t.Fatalf("decoded AuthContext = %#v", decoded)
	}
}

func TestValidatePermissionKey(t *testing.T) {
	for _, permission := range []string{
		"manager.data_item.read",
		"iam.tenant_role_assignment.revoke",
	} {
		if err := ValidatePermissionKey(permission); err != nil {
			t.Fatalf("ValidatePermissionKey(%q) error = %v", permission, err)
		}
	}
	for _, permission := range []string{
		"",
		"manager.read",
		"Manager.data_item.read",
		"manager.data-item.read",
		"manager.data_item.*",
	} {
		if err := ValidatePermissionKey(permission); err == nil {
			t.Fatalf("ValidatePermissionKey(%q) error = nil", permission)
		}
	}
}

func TestValidateOwnerModuleName(t *testing.T) {
	for _, owner := range []string{"manager", "common_frontend", "module2"} {
		if err := ValidateOwnerModuleName(owner); err != nil {
			t.Fatalf("ValidateOwnerModuleName(%q) error = %v", owner, err)
		}
	}
	for _, owner := range []string{"", "Manager", "manager-api", " manager"} {
		if err := ValidateOwnerModuleName(owner); err == nil {
			t.Fatalf("ValidateOwnerModuleName(%q) accepted invalid owner", owner)
		}
	}
}

func TestValidateToolScope(t *testing.T) {
	for _, scope := range []string{"workflow.run", "resource.ancestors.get", "engine2.list_all"} {
		if err := ValidateToolScope(scope); err != nil {
			t.Fatalf("ValidateToolScope(%q) error = %v", scope, err)
		}
	}
	for _, scope := range []string{"", "workflow", "Workflow.run", "workflow-run", " workflow.run"} {
		if err := ValidateToolScope(scope); err == nil {
			t.Fatalf("ValidateToolScope(%q) accepted invalid scope", scope)
		}
	}
}

func TestCloneAuthContextReturnsDetachedCopy(t *testing.T) {
	source := validTenantAuthContext()
	validUntil := source.Authorization.RoleAssignments[0].ValidFrom.Add(time.Hour)
	source.Authentication.StepUpExpiresAt = &validUntil
	source.Authorization.RoleAssignments[0].ValidUntil = &validUntil
	source.Delegation = &DelegationFacts{
		DelegatedByClientID: "addp-cli",
		AgentRunID:          "run-1",
		ToolCallID:          "call-1",
	}

	clone := CloneAuthContext(source)
	clone.Authentication.Methods[0] = "external_idp"
	*clone.Authentication.StepUpExpiresAt = clone.Authentication.StepUpExpiresAt.Add(time.Hour)
	*clone.Context.TenantID = "99"
	*clone.Client.ClientID = "changed-client"
	clone.Client.Audiences[0] = "changed-audience"
	clone.Organization.Departments[0].AncestorIDs[0] = "99"
	clone.Authorization.RoleAssignments[0].Permissions[0] = "changed.permission.read"
	*clone.Authorization.RoleAssignments[0].Scope.TenantID = "99"
	*clone.Authorization.RoleAssignments[0].ValidUntil = clone.Authorization.RoleAssignments[0].ValidUntil.Add(time.Hour)
	clone.Delegation.AgentRunID = "changed-run"

	if source.Authentication.Methods[0] != "password" ||
		*source.Context.TenantID != "3" || *source.Client.ClientID != "addp-web" ||
		source.Client.Audiences[0] != "addp.api" ||
		source.Organization.Departments[0].AncestorIDs[0] != "4" ||
		source.Authorization.RoleAssignments[0].Permissions[0] != "manager.content.read" ||
		*source.Authorization.RoleAssignments[0].Scope.TenantID != "3" ||
		source.Delegation.AgentRunID != "run-1" ||
		source.Authentication.StepUpExpiresAt.Equal(*clone.Authentication.StepUpExpiresAt) ||
		source.Authorization.RoleAssignments[0].ValidUntil.Equal(*clone.Authorization.RoleAssignments[0].ValidUntil) {
		t.Fatalf("source AuthContext was mutated: %#v", source)
	}
}

func TestCloneAuthContextPreservesRequiredEmptyArrays(t *testing.T) {
	source := validTenantAuthContext()
	source.Context = AuthSessionContext{Type: "platform"}
	source.Organization.Departments = []DepartmentMembership{}
	source.Organization.ProjectGroups = []ProjectGroupMembership{}
	source.Authorization.RoleAssignments[0].Scope = AssignmentScope{Type: "platform"}

	clone := CloneAuthContext(source)
	if clone.Client.Scopes == nil || clone.Organization.Departments == nil ||
		clone.Organization.ProjectGroups == nil {
		t.Fatalf("CloneAuthContext() converted required empty arrays to nil: %#v", clone)
	}
	if err := ValidateAuthContext(clone); err != nil {
		t.Fatalf("ValidateAuthContext(CloneAuthContext()) error = %v", err)
	}
}

func TestValidateAuthContextRejectsCrossConstraintAndOrderingViolations(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*AuthContext)
	}{
		{
			name: "platform organization",
			mutate: func(authContext *AuthContext) {
				authContext.Context = AuthSessionContext{Type: "platform"}
				authContext.Authentication.AssuranceLevel = "aal2"
				authContext.Authorization.RoleAssignments = nil
			},
		},
		{
			name: "unsorted permissions",
			mutate: func(authContext *AuthContext) {
				authContext.Authorization.RoleAssignments[0].Permissions = []string{
					"manager.data_item.read", "manager.content.read",
				}
			},
		},
		{
			name: "scope tenant mismatch",
			mutate: func(authContext *AuthContext) {
				otherTenantID := "4"
				authContext.Authorization.RoleAssignments[0].Scope.TenantID = &otherTenantID
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			authContext := validTenantAuthContext()
			testCase.mutate(&authContext)
			if err := ValidateAuthContext(authContext); err == nil {
				t.Fatal("ValidateAuthContext() error = nil")
			}
		})
	}
}

func TestValidateAuthContextResourceTicketConstraints(t *testing.T) {
	authContext := validTenantAuthContext()
	authContext.Token.Type = "resource_access_ticket"
	authContext.Client.Audiences = []string{"manager"}
	authContext.Client.ScopeMode = "restricted"
	authContext.Client.Scopes = []string{BrowserResourceAccessScope}
	if err := ValidateAuthContext(authContext); err != nil {
		t.Fatalf("ValidateAuthContext(resource ticket) error = %v", err)
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*AuthContext)
	}{
		{name: "multiple audiences", mutate: func(value *AuthContext) { value.Client.Audiences = []string{"manager", "meta"} }},
		{name: "platform audience", mutate: func(value *AuthContext) { value.Client.Audiences = []string{"addp.api"} }},
		{name: "wrong scope", mutate: func(value *AuthContext) { value.Client.Scopes = []string{"manager.content.read"} }},
		{name: "unrestricted", mutate: func(value *AuthContext) { value.Client.ScopeMode = "unrestricted"; value.Client.Scopes = nil }},
		{name: "service principal", mutate: func(value *AuthContext) {
			value.Principal.Type = "service_principal"
			value.Organization.Departments = []DepartmentMembership{}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := CloneAuthContext(authContext)
			testCase.mutate(&candidate)
			if err := ValidateAuthContext(candidate); err == nil {
				t.Fatal("ValidateAuthContext() accepted invalid Resource Ticket constraints")
			}
		})
	}
}

func TestValidateAuthContextDelegatedTokenConstraints(t *testing.T) {
	authContext := validTenantAuthContext()
	authContext.Token.Type = "delegated_access_token"
	authContext.Client.Audiences = []string{"develop"}
	authContext.Client.ScopeMode = "restricted"
	authContext.Client.Scopes = []string{"workflow.run"}
	authContext.Delegation = &DelegationFacts{
		DelegatedByClientID: "addp-web",
		AgentRunID:          "run-1",
		ToolCallID:          "call-1",
	}
	if err := ValidateAuthContext(authContext); err != nil {
		t.Fatalf("ValidateAuthContext(delegated token) error = %v", err)
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*AuthContext)
	}{
		{name: "client mismatch", mutate: func(value *AuthContext) { value.Delegation.DelegatedByClientID = "addp-cli" }},
		{name: "invalid audience", mutate: func(value *AuthContext) { value.Client.Audiences = []string{"addp.api"} }},
		{name: "invalid Tool scope", mutate: func(value *AuthContext) { value.Client.Scopes = []string{"workflow"} }},
		{name: "missing delegation", mutate: func(value *AuthContext) { value.Delegation = nil }},
		{name: "service principal", mutate: func(value *AuthContext) {
			value.Principal.Type = "service_principal"
			value.Organization.Departments = []DepartmentMembership{}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := CloneAuthContext(authContext)
			testCase.mutate(&candidate)
			if err := ValidateAuthContext(candidate); err == nil {
				t.Fatal("ValidateAuthContext() accepted invalid Delegated Token constraints")
			}
		})
	}
}

func TestDecodeAuthContextRejectsUnknownFieldsAndMultipleDocuments(t *testing.T) {
	encoded, err := json.Marshal(validTenantAuthContext())
	if err != nil {
		t.Fatal(err)
	}
	withUnknown := strings.Replace(string(encoded), `"schema_version":`, `"unknown":true,"schema_version":`, 1)
	if _, err := DecodeAuthContext(strings.NewReader(withUnknown)); err == nil {
		t.Fatal("DecodeAuthContext() accepted an unknown field")
	}
	if _, err := DecodeAuthContext(strings.NewReader(string(encoded) + string(encoded))); err == nil {
		t.Fatal("DecodeAuthContext() accepted multiple JSON documents")
	}
	withoutRequiredNull := strings.Replace(string(encoded), `,"step_up_expires_at":null`, "", 1)
	if _, err := DecodeAuthContext(strings.NewReader(withoutRequiredNull)); err == nil {
		t.Fatal("DecodeAuthContext() accepted a missing required nullable field")
	}
}

func validTenantAuthContext() AuthContext {
	tenantID := "3"
	membershipID := "28"
	clientID := "addp-web"
	validFrom := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	issuedAt := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	return AuthContext{
		SchemaVersion: AuthContextSchemaVersion,
		Principal: AuthPrincipal{
			Type: "user",
			ID:   "12",
		},
		Context: AuthSessionContext{
			Type:               "tenant",
			TenantID:           &tenantID,
			TenantMembershipID: &membershipID,
		},
		Authentication: AuthenticationFacts{
			Methods:         []string{"password", "totp"},
			AssuranceLevel:  "aal2",
			AuthenticatedAt: issuedAt.Add(-time.Minute),
		},
		Client: ClientConstraints{
			ClientID:  &clientID,
			Audiences: []string{"addp.api"},
			ScopeMode: "unrestricted",
			Scopes:    []string{},
		},
		Organization: OrganizationContext{
			Departments: []DepartmentMembership{{
				MembershipID:   "71",
				DepartmentID:   "9",
				MembershipType: "primary",
				RelationRole:   "member",
				AncestorIDs:    []string{"4"},
			}},
			ProjectGroups: []ProjectGroupMembership{},
		},
		Authorization: AuthorizationFacts{
			AuthorizationVersion: "42",
			RoleAssignments: []RoleAssignment{{
				AssignmentID: "402",
				RoleKey:      "tenant.data_viewer",
				Scope: AssignmentScope{
					Type:     "tenant",
					TenantID: &tenantID,
				},
				Permissions: []string{"manager.content.read", "manager.data_item.read"},
				SourceType:  "manual",
				ValidFrom:   validFrom,
			}},
		},
		Token: TokenFacts{
			Type:      "first_party_access_token",
			IssuedAt:  issuedAt,
			ExpiresAt: issuedAt.Add(15 * time.Minute),
		},
	}
}
