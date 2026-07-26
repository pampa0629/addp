package authorization

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	AuthContextSchemaVersion   = "addp.auth_context/v1"
	BrowserResourceAccessScope = "resource:read"
)

//go:embed schemas/auth-context-v1.schema.json
var authContextSchemaFS embed.FS

var (
	authContextIDPattern         = regexp.MustCompile(`^[1-9][0-9]*$`)
	authContextRoleKeyPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	authContextPermissionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){2}$`)
	authContextToolScopePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

type AuthContext struct {
	SchemaVersion  string              `json:"schema_version"`
	Principal      AuthPrincipal       `json:"principal"`
	Context        AuthSessionContext  `json:"context"`
	Authentication AuthenticationFacts `json:"authentication"`
	Client         ClientConstraints   `json:"client"`
	Organization   OrganizationContext `json:"organization"`
	Authorization  AuthorizationFacts  `json:"authorization"`
	Token          TokenFacts          `json:"token"`
	Delegation     *DelegationFacts    `json:"delegation"`
}

type AuthPrincipal struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type AuthSessionContext struct {
	Type               string  `json:"type"`
	TenantID           *string `json:"tenant_id,omitempty"`
	TenantMembershipID *string `json:"tenant_membership_id,omitempty"`
}

type AuthenticationFacts struct {
	Methods         []string   `json:"methods"`
	AssuranceLevel  string     `json:"assurance_level"`
	AuthenticatedAt time.Time  `json:"authenticated_at"`
	StepUpExpiresAt *time.Time `json:"step_up_expires_at"`
}

type ClientConstraints struct {
	ClientID  *string  `json:"client_id"`
	Audiences []string `json:"audiences"`
	ScopeMode string   `json:"scope_mode"`
	Scopes    []string `json:"scopes"`
}

type OrganizationContext struct {
	Departments   []DepartmentMembership   `json:"departments"`
	ProjectGroups []ProjectGroupMembership `json:"project_groups"`
}

type DepartmentMembership struct {
	MembershipID   string   `json:"membership_id"`
	DepartmentID   string   `json:"department_id"`
	MembershipType string   `json:"membership_type"`
	RelationRole   string   `json:"relation_role"`
	AncestorIDs    []string `json:"ancestor_ids"`
}

type ProjectGroupMembership struct {
	MembershipID   string `json:"membership_id"`
	ProjectGroupID string `json:"project_group_id"`
	RelationRole   string `json:"relation_role"`
}

type AuthorizationFacts struct {
	AuthorizationVersion string           `json:"authorization_version"`
	RoleAssignments      []RoleAssignment `json:"role_assignments"`
}

type RoleAssignment struct {
	AssignmentID string          `json:"assignment_id"`
	RoleKey      string          `json:"role_key"`
	Scope        AssignmentScope `json:"scope"`
	Permissions  []string        `json:"permissions"`
	SourceType   string          `json:"source_type"`
	ValidFrom    time.Time       `json:"valid_from"`
	ValidUntil   *time.Time      `json:"valid_until"`
}

type AssignmentScope struct {
	Type           string  `json:"type"`
	TenantID       *string `json:"tenant_id,omitempty"`
	DepartmentID   *string `json:"department_id,omitempty"`
	ProjectGroupID *string `json:"project_group_id,omitempty"`
}

type TokenFacts struct {
	Type      string    `json:"type"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type DelegationFacts struct {
	DelegatedByClientID string `json:"delegated_by_client_id"`
	AgentRunID          string `json:"agent_run_id"`
	ToolCallID          string `json:"tool_call_id"`
}

func AuthContextSchema() []byte {
	data, err := authContextSchemaFS.ReadFile("schemas/auth-context-v1.schema.json")
	if err != nil {
		panic(fmt.Sprintf("read embedded auth context schema: %v", err))
	}
	return bytes.Clone(data)
}

func DecodeAuthContext(r io.Reader) (AuthContext, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return AuthContext{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var authContext AuthContext
	if err := decoder.Decode(&authContext); err != nil {
		return AuthContext{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return AuthContext{}, fmt.Errorf("multiple JSON documents are not allowed")
		}
		return AuthContext{}, err
	}
	if err := validateAuthContextRequiredJSONFields(data); err != nil {
		return AuthContext{}, err
	}
	if err := ValidateAuthContext(authContext); err != nil {
		return AuthContext{}, err
	}
	return authContext, nil
}

func validateAuthContextRequiredJSONFields(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	if err := requireJSONFields("AuthContext", root,
		"schema_version", "principal", "context", "authentication", "client",
		"organization", "authorization", "token", "delegation"); err != nil {
		return err
	}

	authentication, err := decodeJSONObject(root["authentication"], "authentication")
	if err != nil {
		return err
	}
	if err := requireJSONFields("authentication", authentication,
		"methods", "assurance_level", "authenticated_at", "step_up_expires_at"); err != nil {
		return err
	}
	client, err := decodeJSONObject(root["client"], "client")
	if err != nil {
		return err
	}
	if err := requireJSONFields("client", client, "client_id", "audiences", "scope_mode", "scopes"); err != nil {
		return err
	}

	var assignments []json.RawMessage
	var authorization map[string]json.RawMessage
	if authorization, err = decodeJSONObject(root["authorization"], "authorization"); err != nil {
		return err
	}
	if err := requireJSONFields("authorization", authorization, "authorization_version", "role_assignments"); err != nil {
		return err
	}
	if err := json.Unmarshal(authorization["role_assignments"], &assignments); err != nil {
		return fmt.Errorf("authorization.role_assignments must be an array: %w", err)
	}
	for index, rawAssignment := range assignments {
		assignment, err := decodeJSONObject(rawAssignment, fmt.Sprintf("authorization.role_assignments[%d]", index))
		if err != nil {
			return err
		}
		if err := requireJSONFields(fmt.Sprintf("authorization.role_assignments[%d]", index), assignment,
			"assignment_id", "role_key", "scope", "permissions", "source_type", "valid_from", "valid_until"); err != nil {
			return err
		}
	}

	if string(root["delegation"]) != "null" {
		delegation, err := decodeJSONObject(root["delegation"], "delegation")
		if err != nil {
			return err
		}
		if err := requireJSONFields("delegation", delegation, "delegated_by_client_id", "agent_run_id", "tool_call_id"); err != nil {
			return err
		}
	}
	return nil
}

func decodeJSONObject(raw json.RawMessage, field string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("%s must be an object: %w", field, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%s must be an object", field)
	}
	return object, nil
}

func requireJSONFields(field string, object map[string]json.RawMessage, required ...string) error {
	for _, name := range required {
		if _, exists := object[name]; !exists {
			return fmt.Errorf("%s.%s is required", field, name)
		}
	}
	return nil
}

func ValidateAuthContext(authContext AuthContext) error {
	if authContext.SchemaVersion != AuthContextSchemaVersion {
		return fmt.Errorf("schema_version must be %q", AuthContextSchemaVersion)
	}
	if authContext.Principal.Type != "user" && authContext.Principal.Type != "service_principal" {
		return fmt.Errorf("principal.type is invalid")
	}
	if err := validateAuthContextID("principal.id", authContext.Principal.ID); err != nil {
		return err
	}
	if err := validateAuthSessionContext(authContext.Context); err != nil {
		return err
	}
	if err := validateAuthenticationFacts(authContext.Authentication); err != nil {
		return err
	}
	if err := validateClientConstraints(authContext.Client); err != nil {
		return err
	}
	if err := validateOrganizationContext(authContext.Organization); err != nil {
		return err
	}
	if err := validateAuthorizationFacts(authContext.Authorization, authContext.Context); err != nil {
		return err
	}
	if err := validateTokenFacts(authContext.Token); err != nil {
		return err
	}
	if err := validateAuthContextCrossConstraints(authContext); err != nil {
		return err
	}
	return nil
}

// ValidatePermissionKey validates one canonical Permission key without
// requiring callers to duplicate the AuthContext schema rule.
func ValidatePermissionKey(permission string) error {
	if !authContextPermissionPattern.MatchString(permission) {
		return fmt.Errorf("permission %q is invalid", permission)
	}
	return nil
}

// ValidateOwnerModuleName validates the canonical module owner identifier used
// by audiences, permission manifests, and browser resource access tickets.
func ValidateOwnerModuleName(owner string) error {
	if !moduleNamePattern.MatchString(owner) {
		return fmt.Errorf("owner module %q is invalid", owner)
	}
	return nil
}

// ValidateToolScope validates the stable Tool name used as a delegated scope.
func ValidateToolScope(scope string) error {
	if !authContextToolScopePattern.MatchString(scope) {
		return fmt.Errorf("tool scope %q is invalid", scope)
	}
	return nil
}

// CloneAuthContext returns a deep copy of the shared AuthContext contract.
func CloneAuthContext(source AuthContext) AuthContext {
	clone := source
	clone.Authentication.Methods = cloneSlice(source.Authentication.Methods)
	clone.Authentication.StepUpExpiresAt = cloneTime(source.Authentication.StepUpExpiresAt)
	clone.Client.ClientID = cloneString(source.Client.ClientID)
	clone.Client.Audiences = cloneSlice(source.Client.Audiences)
	clone.Client.Scopes = cloneSlice(source.Client.Scopes)
	clone.Context.TenantID = cloneString(source.Context.TenantID)
	clone.Context.TenantMembershipID = cloneString(source.Context.TenantMembershipID)
	clone.Organization.Departments = cloneSlice(source.Organization.Departments)
	for index := range clone.Organization.Departments {
		clone.Organization.Departments[index].AncestorIDs = cloneSlice(
			source.Organization.Departments[index].AncestorIDs,
		)
	}
	clone.Organization.ProjectGroups = cloneSlice(source.Organization.ProjectGroups)
	clone.Authorization.RoleAssignments = cloneSlice(source.Authorization.RoleAssignments)
	for index := range clone.Authorization.RoleAssignments {
		sourceAssignment := source.Authorization.RoleAssignments[index]
		assignment := &clone.Authorization.RoleAssignments[index]
		assignment.Scope.TenantID = cloneString(sourceAssignment.Scope.TenantID)
		assignment.Scope.DepartmentID = cloneString(sourceAssignment.Scope.DepartmentID)
		assignment.Scope.ProjectGroupID = cloneString(sourceAssignment.Scope.ProjectGroupID)
		assignment.Permissions = cloneSlice(sourceAssignment.Permissions)
		assignment.ValidUntil = cloneTime(sourceAssignment.ValidUntil)
	}
	if source.Delegation != nil {
		delegation := *source.Delegation
		clone.Delegation = &delegation
	}
	return clone
}

func cloneSlice[T any](source []T) []T {
	if source == nil {
		return nil
	}
	return append(make([]T, 0, len(source)), source...)
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func validateAuthSessionContext(sessionContext AuthSessionContext) error {
	switch sessionContext.Type {
	case "platform":
		if sessionContext.TenantID != nil || sessionContext.TenantMembershipID != nil {
			return fmt.Errorf("platform context cannot contain tenant fields")
		}
	case "tenant":
		if sessionContext.TenantID == nil || sessionContext.TenantMembershipID == nil {
			return fmt.Errorf("tenant context requires tenant_id and tenant_membership_id")
		}
		if err := validateAuthContextID("context.tenant_id", *sessionContext.TenantID); err != nil {
			return err
		}
		if err := validateAuthContextID("context.tenant_membership_id", *sessionContext.TenantMembershipID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("context.type is invalid")
	}
	return nil
}

func validateAuthenticationFacts(authentication AuthenticationFacts) error {
	allowedMethods := map[string]struct{}{
		"password": {}, "totp": {}, "webauthn": {}, "external_idp": {},
		"recovery_code": {}, "service_secret": {}, "private_key_jwt": {}, "mtls": {},
	}
	if err := validateSortedStrings("authentication.methods", authentication.Methods, true, allowedMethods); err != nil {
		return err
	}
	switch authentication.AssuranceLevel {
	case "aal1", "aal2", "aal3", "not_applicable":
	default:
		return fmt.Errorf("authentication.assurance_level is invalid")
	}
	if authentication.AuthenticatedAt.IsZero() {
		return fmt.Errorf("authentication.authenticated_at is required")
	}
	if authentication.StepUpExpiresAt != nil && authentication.StepUpExpiresAt.Before(authentication.AuthenticatedAt) {
		return fmt.Errorf("authentication.step_up_expires_at cannot precede authenticated_at")
	}
	return nil
}

func validateClientConstraints(client ClientConstraints) error {
	if client.ClientID != nil && strings.TrimSpace(*client.ClientID) == "" {
		return fmt.Errorf("client.client_id cannot be empty")
	}
	if err := validateSortedStrings("client.audiences", client.Audiences, true, nil); err != nil {
		return err
	}
	if err := validateSortedStrings("client.scopes", client.Scopes, false, nil); err != nil {
		return err
	}
	switch client.ScopeMode {
	case "unrestricted":
		if len(client.Scopes) != 0 {
			return fmt.Errorf("unrestricted client must have empty scopes")
		}
	case "restricted":
		if len(client.Scopes) == 0 {
			return fmt.Errorf("restricted client requires scopes")
		}
	default:
		return fmt.Errorf("client.scope_mode is invalid")
	}
	return nil
}

func validateOrganizationContext(organization OrganizationContext) error {
	if organization.Departments == nil || organization.ProjectGroups == nil {
		return fmt.Errorf("organization arrays must not be null")
	}
	previousDepartmentID := int64(0)
	for index, membership := range organization.Departments {
		if err := validateAuthContextID("organization.departments.membership_id", membership.MembershipID); err != nil {
			return err
		}
		departmentID, err := parseAuthContextID("organization.departments.department_id", membership.DepartmentID)
		if err != nil {
			return err
		}
		if index > 0 && departmentID <= previousDepartmentID {
			return fmt.Errorf("organization.departments must be sorted by department_id")
		}
		previousDepartmentID = departmentID
		if membership.MembershipType != "primary" && membership.MembershipType != "additional" {
			return fmt.Errorf("organization department membership_type is invalid")
		}
		if membership.RelationRole != "member" && membership.RelationRole != "leader" {
			return fmt.Errorf("organization department relation_role is invalid")
		}
		if membership.AncestorIDs == nil {
			return fmt.Errorf("organization department ancestor_ids must not be null")
		}
		seenAncestors := make(map[string]struct{}, len(membership.AncestorIDs))
		for _, ancestorID := range membership.AncestorIDs {
			if err := validateAuthContextID("organization.departments.ancestor_ids", ancestorID); err != nil {
				return err
			}
			if _, exists := seenAncestors[ancestorID]; exists {
				return fmt.Errorf("organization department ancestor_ids contains duplicates")
			}
			seenAncestors[ancestorID] = struct{}{}
		}
	}

	previousProjectGroupID := int64(0)
	for index, membership := range organization.ProjectGroups {
		if err := validateAuthContextID("organization.project_groups.membership_id", membership.MembershipID); err != nil {
			return err
		}
		projectGroupID, err := parseAuthContextID("organization.project_groups.project_group_id", membership.ProjectGroupID)
		if err != nil {
			return err
		}
		if index > 0 && projectGroupID <= previousProjectGroupID {
			return fmt.Errorf("organization.project_groups must be sorted by project_group_id")
		}
		previousProjectGroupID = projectGroupID
		switch membership.RelationRole {
		case "member", "leader", "coordinator":
		default:
			return fmt.Errorf("organization project group relation_role is invalid")
		}
	}
	return nil
}

func validateAuthorizationFacts(authorization AuthorizationFacts, sessionContext AuthSessionContext) error {
	if err := validateAuthContextID("authorization.authorization_version", authorization.AuthorizationVersion); err != nil {
		return err
	}
	if authorization.RoleAssignments == nil {
		return fmt.Errorf("authorization.role_assignments must not be null")
	}
	previousSortKey := ""
	for index, assignment := range authorization.RoleAssignments {
		if err := validateRoleAssignment(assignment, sessionContext); err != nil {
			return fmt.Errorf("authorization.role_assignments[%d]: %w", index, err)
		}
		sortKey := roleAssignmentSortKey(assignment)
		if previousSortKey != "" && sortKey <= previousSortKey {
			return fmt.Errorf("authorization.role_assignments are not in stable order")
		}
		previousSortKey = sortKey
	}
	return nil
}

func validateRoleAssignment(assignment RoleAssignment, sessionContext AuthSessionContext) error {
	if err := validateAuthContextID("assignment_id", assignment.AssignmentID); err != nil {
		return err
	}
	if !authContextRoleKeyPattern.MatchString(assignment.RoleKey) {
		return fmt.Errorf("role_key is invalid")
	}
	if err := validateAssignmentScope(assignment.Scope, sessionContext); err != nil {
		return err
	}
	if len(assignment.Permissions) == 0 {
		return fmt.Errorf("permissions must not be empty")
	}
	if err := validateSortedStrings("permissions", assignment.Permissions, true, nil); err != nil {
		return err
	}
	for _, permission := range assignment.Permissions {
		if err := ValidatePermissionKey(permission); err != nil {
			return err
		}
	}
	switch assignment.SourceType {
	case "manual", "idp_mapping", "bootstrap", "break_glass":
	default:
		return fmt.Errorf("source_type is invalid")
	}
	if assignment.ValidFrom.IsZero() {
		return fmt.Errorf("valid_from is required")
	}
	if assignment.ValidUntil != nil && !assignment.ValidUntil.After(assignment.ValidFrom) {
		return fmt.Errorf("valid_until must be after valid_from")
	}
	return nil
}

func validateAssignmentScope(scope AssignmentScope, sessionContext AuthSessionContext) error {
	if sessionContext.Type == "platform" && scope.Type != "platform" {
		return fmt.Errorf("platform context can only contain platform assignments")
	}
	if sessionContext.Type == "tenant" && scope.Type == "platform" {
		return fmt.Errorf("tenant context cannot contain platform assignments")
	}
	switch scope.Type {
	case "platform":
		if scope.TenantID != nil || scope.DepartmentID != nil || scope.ProjectGroupID != nil {
			return fmt.Errorf("platform scope cannot contain tenant fields")
		}
	case "tenant":
		if scope.TenantID == nil || scope.DepartmentID != nil || scope.ProjectGroupID != nil {
			return fmt.Errorf("tenant scope fields are invalid")
		}
	case "department":
		if scope.TenantID == nil || scope.DepartmentID == nil || scope.ProjectGroupID != nil {
			return fmt.Errorf("department scope fields are invalid")
		}
		if err := validateAuthContextID("scope.department_id", *scope.DepartmentID); err != nil {
			return err
		}
	case "project_group":
		if scope.TenantID == nil || scope.DepartmentID != nil || scope.ProjectGroupID == nil {
			return fmt.Errorf("project_group scope fields are invalid")
		}
		if err := validateAuthContextID("scope.project_group_id", *scope.ProjectGroupID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("scope.type is invalid")
	}
	if scope.TenantID != nil {
		if err := validateAuthContextID("scope.tenant_id", *scope.TenantID); err != nil {
			return err
		}
		if sessionContext.TenantID == nil || *scope.TenantID != *sessionContext.TenantID {
			return fmt.Errorf("assignment scope tenant does not match current context")
		}
	}
	return nil
}

func validateTokenFacts(token TokenFacts) error {
	switch token.Type {
	case "first_party_access_token", "oauth_access_token", "service_access_token", "resource_access_ticket", "delegated_access_token":
	default:
		return fmt.Errorf("token.type is invalid")
	}
	if token.IssuedAt.IsZero() || !token.ExpiresAt.After(token.IssuedAt) {
		return fmt.Errorf("token expiry must be after issue time")
	}
	return nil
}

func validateAuthContextCrossConstraints(authContext AuthContext) error {
	if authContext.Context.Type == "platform" {
		if len(authContext.Organization.Departments) != 0 || len(authContext.Organization.ProjectGroups) != 0 {
			return fmt.Errorf("platform context organization must be empty")
		}
		if authContext.Principal.Type == "user" && authContext.Authentication.AssuranceLevel != "aal2" && authContext.Authentication.AssuranceLevel != "aal3" {
			return fmt.Errorf("platform user requires aal2 or aal3")
		}
	}
	if authContext.Principal.Type == "service_principal" && len(authContext.Organization.Departments) != 0 {
		return fmt.Errorf("service principal cannot have department memberships")
	}

	switch authContext.Token.Type {
	case "first_party_access_token":
		if authContext.Client.ClientID == nil || *authContext.Client.ClientID != "addp-web" || authContext.Client.ScopeMode != "unrestricted" {
			return fmt.Errorf("first-party access token requires addp-web unrestricted client")
		}
	case "resource_access_ticket":
		if authContext.Principal.Type != "user" || authContext.Client.ClientID == nil ||
			*authContext.Client.ClientID != "addp-web" || authContext.Client.ScopeMode != "restricted" ||
			len(authContext.Client.Audiences) != 1 || len(authContext.Client.Scopes) != 1 ||
			authContext.Client.Scopes[0] != BrowserResourceAccessScope ||
			ValidateOwnerModuleName(authContext.Client.Audiences[0]) != nil {
			return fmt.Errorf("resource access ticket requires a user, addp-web, one owner audience, and resource:read scope")
		}
	case "oauth_access_token":
		if authContext.Client.ClientID == nil || authContext.Client.ScopeMode != "restricted" {
			return fmt.Errorf("oauth access token requires a restricted client")
		}
	case "delegated_access_token":
		if authContext.Principal.Type != "user" || authContext.Client.ClientID == nil ||
			authContext.Client.ScopeMode != "restricted" || len(authContext.Client.Audiences) != 1 ||
			ValidateOwnerModuleName(authContext.Client.Audiences[0]) != nil || authContext.Delegation == nil ||
			authContext.Delegation.DelegatedByClientID != *authContext.Client.ClientID {
			return fmt.Errorf("delegated access token requires a user, source client, one owner audience, restricted scopes, and delegation")
		}
		for _, scope := range authContext.Client.Scopes {
			if ValidateToolScope(scope) != nil {
				return fmt.Errorf("delegated access token contains an invalid Tool scope")
			}
		}
	}
	if authContext.Token.Type != "delegated_access_token" && authContext.Delegation != nil {
		return fmt.Errorf("non-delegated token cannot contain delegation")
	}
	if authContext.Delegation != nil && (strings.TrimSpace(authContext.Delegation.DelegatedByClientID) == "" ||
		strings.TrimSpace(authContext.Delegation.AgentRunID) == "" || strings.TrimSpace(authContext.Delegation.ToolCallID) == "") {
		return fmt.Errorf("delegation fields cannot be empty")
	}
	return nil
}

func validateAuthContextID(field string, value string) error {
	_, err := parseAuthContextID(field, value)
	return err
}

func parseAuthContextID(field string, value string) (int64, error) {
	if !authContextIDPattern.MatchString(value) {
		return 0, fmt.Errorf("%s must be a non-zero decimal string", field)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s is outside the IAM bigint range", field)
	}
	return parsed, nil
}

func validateSortedStrings(field string, values []string, requireNonEmpty bool, allowed map[string]struct{}) error {
	if values == nil {
		return fmt.Errorf("%s must not be null", field)
	}
	if requireNonEmpty && len(values) == 0 {
		return fmt.Errorf("%s must not be empty", field)
	}
	if !sort.StringsAreSorted(values) {
		return fmt.Errorf("%s must be sorted", field)
	}
	previous := ""
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s cannot contain empty values", field)
		}
		if value == previous {
			return fmt.Errorf("%s cannot contain duplicates", field)
		}
		if allowed != nil {
			if _, exists := allowed[value]; !exists {
				return fmt.Errorf("%s contains unsupported value %q", field, value)
			}
		}
		previous = value
	}
	return nil
}

func roleAssignmentSortKey(assignment RoleAssignment) string {
	scopeOrder := map[string]int{"platform": 0, "tenant": 1, "department": 2, "project_group": 3}
	scopeID := int64(0)
	for _, candidate := range []*string{
		assignment.Scope.TenantID,
		assignment.Scope.DepartmentID,
		assignment.Scope.ProjectGroupID,
	} {
		if candidate != nil {
			scopeID, _ = strconv.ParseInt(*candidate, 10, 64)
		}
	}
	assignmentID, _ := strconv.ParseInt(assignment.AssignmentID, 10, 64)
	return fmt.Sprintf("%d:%020d:%s:%020d", scopeOrder[assignment.Scope.Type], scopeID, assignment.RoleKey, assignmentID)
}
