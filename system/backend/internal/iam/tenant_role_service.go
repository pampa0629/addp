package iam

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/lib/pq"
)

const tenantAdministratorRoleKey = "tenant.administrator"

var tenantCustomRoleKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

var ErrTenantRoleAssignmentAlreadyExists = fmt.Errorf(
	"%w: tenant role assignment already exists in the requested scope",
	commonapi.ErrConflict,
)

type TenantRoleAssignmentFilter struct {
	MembershipID   *int64
	Status         *string
	ScopeType      *string
	DepartmentID   *int64
	ProjectGroupID *int64
}

type CreateTenantRoleInput struct {
	TenantID         int64
	RoleKey          string
	Name             string
	Description      string
	ScopeTypes       []string
	PermissionKeys   []string
	ActorPrincipalID int64
	Audit            AuditMetadata
}

type UpdateTenantRoleInput struct {
	TenantID         int64
	RoleID           int64
	Name             string
	Description      string
	ScopeTypes       []string
	PermissionKeys   []string
	ActorPrincipalID int64
	Audit            AuditMetadata
}

type DeleteTenantRoleInput struct {
	TenantID         int64
	RoleID           int64
	Reason           string
	ActorPrincipalID int64
	Audit            AuditMetadata
}

type CreateTenantRoleAssignmentInput struct {
	TenantID         int64
	MembershipID     int64
	RoleID           int64
	ScopeType        string
	DepartmentID     *int64
	ProjectGroupID   *int64
	ValidUntil       *time.Time
	Reason           string
	ActorPrincipalID int64
	AssuranceLevel   AssuranceLevel
	StepUpExpiresAt  *time.Time
	Audit            AuditMetadata
}

type RevokeTenantRoleAssignmentInput struct {
	TenantID         int64
	AssignmentID     int64
	Reason           string
	ActorPrincipalID int64
	Audit            AuditMetadata
}

type TenantRoleService struct {
	repository *Repository
	now        func() time.Time
}

func NewTenantRoleService(repository *Repository, now func() time.Time) *TenantRoleService {
	if now == nil {
		now = time.Now
	}
	return &TenantRoleService{repository: repository, now: now}
}

func (s *TenantRoleService) ListRoles(ctx context.Context, tenantID int64) ([]TenantRole, error) {
	if err := s.validateTenant(tenantID); err != nil {
		return nil, err
	}
	return s.repository.ListTenantRoles(ctx, tenantID)
}

func (s *TenantRoleService) ListAssignablePermissions(ctx context.Context, tenantID int64) ([]TenantAssignablePermission, error) {
	if err := s.validateTenant(tenantID); err != nil {
		return nil, err
	}
	return s.repository.ListTenantAssignablePermissions(ctx)
}

func (s *TenantRoleService) CreateRole(ctx context.Context, input CreateTenantRoleInput) (*TenantRole, error) {
	roleKey, name, scopes, permissions, err := validateTenantRoleDefinition(
		input.RoleKey, input.Name, input.ScopeTypes, input.PermissionKeys,
	)
	if err != nil || input.TenantID <= 0 || input.ActorPrincipalID <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: tenant and actor are required", commonapi.ErrBadRequest)
	}
	description := strings.TrimSpace(input.Description)
	role := &Role{
		TenantID: &input.TenantID, RoleKey: roleKey, Name: &name, Description: &description,
		RoleType: "tenant_custom", AllowedScopeTypes: pq.StringArray(scopes),
		AllowedPrincipalTypes: pq.StringArray{"user"}, Immutable: false, Status: "active",
		CreatedByPrincipalID: &input.ActorPrincipalID,
	}
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.LockPrincipal(ctx, input.ActorPrincipalID); err != nil {
			return err
		}
		if _, err := tx.LockTenantForUpdate(ctx, input.TenantID); err != nil {
			return err
		}
		conflicts, err := tx.TenantCustomRoleKeyConflictsWithBuiltin(ctx, roleKey)
		if err != nil {
			return err
		}
		if conflicts {
			return fmt.Errorf("%w: tenant custom role key conflicts with a built-in role", commonapi.ErrConflict)
		}
		if err := validateCustomizablePermissions(ctx, tx, scopes, permissions); err != nil {
			return err
		}
		if err := tx.CreateTenantCustomRole(ctx, role, permissions); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.tenant_role.created", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskMedium, ModuleName: "system", EntityType: "role", EntityID: strconv.FormatInt(role.ID, 10),
			Details: map[string]any{"tenant_id": input.TenantID, "role_key": role.RoleKey, "scope_types": scopes, "permission_keys": permissions},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.findRole(ctx, input.TenantID, role.ID)
}

func (s *TenantRoleService) UpdateRole(ctx context.Context, input UpdateTenantRoleInput) (*TenantRole, error) {
	_, name, scopes, permissions, err := validateTenantRoleDefinition("custom.placeholder", input.Name, input.ScopeTypes, input.PermissionKeys)
	if err != nil || input.TenantID <= 0 || input.RoleID <= 0 || input.ActorPrincipalID <= 0 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: tenant, role and actor are required", commonapi.ErrBadRequest)
	}
	description := strings.TrimSpace(input.Description)
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.LockPrincipal(ctx, input.ActorPrincipalID); err != nil {
			return err
		}
		if _, err := tx.LockTenantForUpdate(ctx, input.TenantID); err != nil {
			return err
		}
		role, err := tx.LockTenantRole(ctx, input.TenantID, input.RoleID)
		if err != nil {
			return err
		}
		if role.Immutable || role.RoleType != "tenant_custom" || role.Status != "active" {
			return commonapi.ErrForbidden
		}
		if err := validateCustomizablePermissions(ctx, tx, scopes, permissions); err != nil {
			return err
		}
		holders, err := tx.ListActiveRoleHolderPrincipalIDs(ctx, role.ID)
		if err != nil {
			return err
		}
		if err := tx.UpdateTenantCustomRole(ctx, role.ID, name, description, scopes, permissions, input.ActorPrincipalID); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.tenant_role.updated", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskMedium, ModuleName: "system", EntityType: "role", EntityID: strconv.FormatInt(role.ID, 10),
			Details: map[string]any{"tenant_id": input.TenantID, "role_key": role.RoleKey, "scope_types": scopes, "permission_keys": permissions, "affected_principals": len(holders), "authorization_version_changed": len(holders) > 0},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.findRole(ctx, input.TenantID, input.RoleID)
}

func (s *TenantRoleService) DeleteRole(ctx context.Context, input DeleteTenantRoleInput) error {
	if input.TenantID <= 0 || input.RoleID <= 0 || input.ActorPrincipalID <= 0 || strings.TrimSpace(input.Reason) == "" {
		return fmt.Errorf("%w: tenant, role, actor and reason are required", commonapi.ErrBadRequest)
	}
	now := s.now().UTC()
	return s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.LockPrincipal(ctx, input.ActorPrincipalID); err != nil {
			return err
		}
		if _, err := tx.LockTenantForUpdate(ctx, input.TenantID); err != nil {
			return err
		}
		role, err := tx.LockTenantRole(ctx, input.TenantID, input.RoleID)
		if err != nil {
			return err
		}
		if role.Immutable || role.RoleType != "tenant_custom" || role.Status != "active" {
			return commonapi.ErrForbidden
		}
		holders, err := tx.ListActiveRoleHolderPrincipalIDs(ctx, role.ID)
		if err != nil {
			return err
		}
		if err := tx.DisableTenantCustomRole(ctx, role.ID, input.ActorPrincipalID, now); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.tenant_role.deleted", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "role", EntityID: strconv.FormatInt(role.ID, 10),
			Details: map[string]any{"tenant_id": input.TenantID, "role_key": role.RoleKey, "reason": strings.TrimSpace(input.Reason), "affected_principals": len(holders), "authorization_version_changed": len(holders) > 0},
		})
	})
}

func (s *TenantRoleService) ListAssignments(ctx context.Context, tenantID int64, filter TenantRoleAssignmentFilter, page, pageSize int) ([]ManagedTenantRoleAssignment, int64, error) {
	if err := s.validateTenant(tenantID); err != nil {
		return nil, 0, err
	}
	if err := validateManagementPagination(page, pageSize); err != nil {
		return nil, 0, err
	}
	if err := validateTenantRoleAssignmentFilter(filter); err != nil {
		return nil, 0, err
	}
	return s.repository.ListTenantRoleAssignments(ctx, tenantID, filter, page, pageSize)
}

func (s *TenantRoleService) CreateAssignment(ctx context.Context, input CreateTenantRoleAssignmentInput) (*ManagedTenantRoleAssignment, error) {
	if input.TenantID <= 0 || input.MembershipID <= 0 || input.RoleID <= 0 || input.ActorPrincipalID <= 0 {
		return nil, fmt.Errorf("%w: tenant, membership, role and actor are required", commonapi.ErrBadRequest)
	}
	if input.ScopeType == "" {
		input.ScopeType = "tenant"
	}
	if err := validateAssignmentScope(input.ScopeType, input.DepartmentID, input.ProjectGroupID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if input.ValidUntil != nil && !input.ValidUntil.After(now) {
		return nil, fmt.Errorf("%w: assignment expiry must be in the future", commonapi.ErrBadRequest)
	}
	var assignment *RoleAssignment
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.LockPrincipal(ctx, input.ActorPrincipalID); err != nil {
			return err
		}
		if _, err := tx.LockTenantForUpdate(ctx, input.TenantID); err != nil {
			return err
		}
		membership, err := tx.LockTenantMembershipByID(ctx, input.MembershipID)
		if err != nil {
			return err
		}
		if membership.TenantID != input.TenantID || membership.Status != TenantMembershipStatusActive || (membership.ExpiresAt != nil && !membership.ExpiresAt.After(now)) {
			return commonapi.ErrForbidden
		}
		if _, err := tx.LockPrincipal(ctx, membership.PrincipalID); err != nil {
			return err
		}
		roles, err := tx.ListTenantRoles(ctx, input.TenantID)
		if err != nil {
			return err
		}
		var role *TenantRole
		for index := range roles {
			if roles[index].ID == input.RoleID {
				role = &roles[index]
				break
			}
		}
		if role == nil {
			return commonapi.ErrNotFound
		}
		if !containsString(role.AllowedScopeTypes, input.ScopeType) {
			return commonapi.ErrForbidden
		}
		if membership.PrincipalID == input.ActorPrincipalID {
			highRisk, err := tx.TenantRoleHasHighRiskPermission(ctx, role.ID)
			if err != nil {
				return err
			}
			if highRisk && ((input.AssuranceLevel != AssuranceLevelAAL2 && input.AssuranceLevel != AssuranceLevelAAL3) ||
				input.StepUpExpiresAt == nil || !input.StepUpExpiresAt.After(now)) {
				return ErrStepUpRequired
			}
		}
		if role.RoleKey == tenantAdministratorRoleKey && input.ValidUntil != nil {
			return fmt.Errorf("%w: tenant administrator assignment cannot expire", commonapi.ErrBadRequest)
		}
		tenantID := input.TenantID
		assignment = &RoleAssignment{
			PrincipalID: membership.PrincipalID, RoleID: role.ID, ScopeType: input.ScopeType, TenantID: &tenantID,
			DepartmentID: input.DepartmentID, ProjectGroupID: input.ProjectGroupID, Status: "active", ValidFrom: now,
			ValidUntil: input.ValidUntil, SourceType: "manual", CreatedByPrincipalID: &input.ActorPrincipalID, Reason: strings.TrimSpace(input.Reason),
		}
		if err := tx.CreateTenantRoleAssignment(ctx, assignment); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.tenant_role_assignment.created", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskMedium, ModuleName: "system", EntityType: "role_assignment", EntityID: strconv.FormatInt(assignment.ID, 10),
			Details: map[string]any{"tenant_id": input.TenantID, "membership_id": membership.ID, "principal_id": membership.PrincipalID, "role_key": role.RoleKey, "scope_type": input.ScopeType, "authorization_version_changed": true},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetManagedTenantRoleAssignment(ctx, input.TenantID, assignment.ID)
}

func (s *TenantRoleService) RevokeAssignment(ctx context.Context, input RevokeTenantRoleAssignmentInput) (*ManagedTenantRoleAssignment, error) {
	if input.TenantID <= 0 || input.AssignmentID <= 0 || input.ActorPrincipalID <= 0 || strings.TrimSpace(input.Reason) == "" {
		return nil, fmt.Errorf("%w: tenant, assignment, actor and reason are required", commonapi.ErrBadRequest)
	}
	now := s.now().UTC()
	var assignment *RoleAssignment
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.LockPrincipal(ctx, input.ActorPrincipalID); err != nil {
			return err
		}
		if _, err := tx.LockTenantForUpdate(ctx, input.TenantID); err != nil {
			return err
		}
		var err error
		assignment, err = tx.LockTenantRoleAssignment(ctx, input.TenantID, input.AssignmentID)
		if err != nil {
			return err
		}
		if assignment.Status != "active" {
			return commonapi.ErrConflict
		}
		if _, err := tx.LockPrincipal(ctx, assignment.PrincipalID); err != nil {
			return err
		}
		if err := tx.RevokeTenantRoleAssignment(ctx, assignment.ID, input.ActorPrincipalID, now); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.tenant_role_assignment.revoked", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "role_assignment", EntityID: strconv.FormatInt(assignment.ID, 10),
			Details: map[string]any{"tenant_id": input.TenantID, "principal_id": assignment.PrincipalID, "reason": strings.TrimSpace(input.Reason), "authorization_version_changed": true},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetManagedTenantRoleAssignment(ctx, input.TenantID, assignment.ID)
}

func validateTenantRoleAssignmentFilter(filter TenantRoleAssignmentFilter) error {
	if filter.MembershipID != nil && *filter.MembershipID <= 0 {
		return fmt.Errorf("%w: membership filter must be positive", commonapi.ErrBadRequest)
	}
	if filter.Status != nil && *filter.Status != "active" && *filter.Status != "revoked" {
		return fmt.Errorf("%w: invalid role assignment status filter", commonapi.ErrBadRequest)
	}
	if filter.ScopeType == nil {
		if filter.DepartmentID != nil || filter.ProjectGroupID != nil {
			return fmt.Errorf("%w: scoped identifiers require scope_type", commonapi.ErrBadRequest)
		}
		return nil
	}
	return validateAssignmentScope(*filter.ScopeType, filter.DepartmentID, filter.ProjectGroupID)
}

func (s *TenantRoleService) findRole(ctx context.Context, tenantID, roleID int64) (*TenantRole, error) {
	roles, err := s.repository.ListTenantRoles(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for index := range roles {
		if roles[index].ID == roleID {
			return &roles[index], nil
		}
	}
	return nil, commonapi.ErrNotFound
}

func (s *TenantRoleService) validateTenant(tenantID int64) error {
	if s == nil || s.repository == nil || tenantID <= 0 {
		return fmt.Errorf("%w: IAM repository and tenant are required", commonapi.ErrBadRequest)
	}
	return nil
}

func validateTenantRoleDefinition(roleKey, name string, scopeTypes, permissionKeys []string) (string, string, []string, []string, error) {
	roleKey, name = strings.TrimSpace(roleKey), strings.TrimSpace(name)
	if !tenantCustomRoleKeyPattern.MatchString(roleKey) || name == "" {
		return "", "", nil, nil, fmt.Errorf("%w: valid role key and name are required", commonapi.ErrBadRequest)
	}
	scopes := uniqueSorted(scopeTypes)
	permissions := uniqueSorted(permissionKeys)
	if len(scopes) == 0 || len(permissions) == 0 {
		return "", "", nil, nil, fmt.Errorf("%w: role scopes and permissions are required", commonapi.ErrBadRequest)
	}
	for _, scope := range scopes {
		if scope != "tenant" && scope != "department" && scope != "project_group" {
			return "", "", nil, nil, fmt.Errorf("%w: invalid role scope", commonapi.ErrBadRequest)
		}
	}
	return roleKey, name, scopes, permissions, nil
}

func validateCustomizablePermissions(ctx context.Context, repository *Repository, scopes, permissionKeys []string) error {
	available, err := repository.ListTenantAssignablePermissions(ctx)
	if err != nil {
		return err
	}
	byKey := make(map[string]TenantAssignablePermission, len(available))
	for _, permission := range available {
		byKey[permission.PermissionKey] = permission
	}
	for _, key := range permissionKeys {
		permission, exists := byKey[key]
		if !exists {
			return fmt.Errorf("%w: permission is not tenant customizable", commonapi.ErrBadRequest)
		}
		for _, scope := range scopes {
			if !containsString(permission.AllowedScopeTypes, scope) {
				return fmt.Errorf("%w: role scope exceeds permission scope", commonapi.ErrBadRequest)
			}
		}
	}
	return nil
}

func validateAssignmentScope(scope string, departmentID, projectGroupID *int64) error {
	valid := (scope == "tenant" && departmentID == nil && projectGroupID == nil) ||
		(scope == "department" && departmentID != nil && *departmentID > 0 && projectGroupID == nil) ||
		(scope == "project_group" && departmentID == nil && projectGroupID != nil && *projectGroupID > 0)
	if !valid {
		return fmt.Errorf("%w: invalid assignment scope", commonapi.ErrBadRequest)
	}
	return nil
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	sort.Strings(result)
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
