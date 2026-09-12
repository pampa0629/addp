package iam

import (
	"context"
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ManagedTenant struct {
	Tenant
	Initialized bool `gorm:"column:initialized"`
}

type TenantAdministratorCandidate struct {
	PrincipalID  int64
	DisplayName  string
	PrimaryEmail *string
	Username     *string
}

type TenantRole struct {
	Role
	PermissionKeys pq.StringArray `gorm:"column:permission_keys;type:text[]"`
}

type TenantAssignablePermission struct {
	PermissionKey      string
	OwnerModule        string
	Action             string
	RiskLevel          string
	AllowedScopeTypes  pq.StringArray `gorm:"column:allowed_scope_types;type:text[]"`
	NameI18nKey        string
	DescriptionI18nKey string
}

type ManagedTenantRoleAssignment struct {
	RoleAssignment
	MembershipID                int64
	PrincipalType               PrincipalType
	DisplayName                 string
	Username                    *string
	ServicePrincipalName        *string
	RoleKey                     string
	RoleName                    *string
	RoleNameI18nKey             *string
	EffectiveState              string
	SameScopeActiveAssignmentID *int64
	GrantedByPrincipalType      *PrincipalType
	GrantedByDisplayName        *string
	GrantedByIdentifier         *string
	GrantedByStatus             *PrincipalStatus
	RevokedByPrincipalType      *PrincipalType
	RevokedByDisplayName        *string
	RevokedByIdentifier         *string
	RevokedByStatus             *PrincipalStatus
}

type BuiltinServiceRuntimeBinding struct {
	PrincipalID int64  `gorm:"column:principal_id"`
	ServiceName string `gorm:"column:service_name"`
	RoleID      int64  `gorm:"column:role_id"`
	RoleKey     string `gorm:"column:role_key"`
}

func (r *Repository) CurrentDatabaseTime(ctx context.Context) (time.Time, error) {
	var current time.Time
	if err := r.db.WithContext(ctx).Raw("SELECT now()").Scan(&current).Error; err != nil {
		return time.Time{}, wrapRepositoryError(err)
	}
	return current.UTC(), nil
}

func (r *Repository) ListManagedTenantViews(
	ctx context.Context, page, pageSize int, search string, status *TenantStatus,
) ([]ManagedTenant, int64, error) {
	base := r.db.WithContext(ctx).Table("system.tenants tenant")
	if normalized := strings.TrimSpace(search); normalized != "" {
		pattern := "%" + normalized + "%"
		base = base.Where("tenant.code ILIKE ? OR tenant.name ILIKE ?", pattern, pattern)
	}
	if status != nil {
		base = base.Where("tenant.status = ?", *status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var tenants []ManagedTenant
	err := base.Select(`tenant.*,
		tenant.initialized_at IS NOT NULL AND EXISTS (
			SELECT 1 FROM system.role_assignments assignment
			JOIN system.roles role ON role.id = assignment.role_id
			JOIN system.tenant_memberships membership
			  ON membership.tenant_id = assignment.tenant_id
			 AND membership.principal_id = assignment.principal_id
			JOIN system.principals principal ON principal.id = assignment.principal_id
			WHERE assignment.tenant_id = tenant.id
			  AND assignment.scope_type = 'tenant'
			  AND assignment.status = 'active'
			  AND assignment.valid_from <= now()
			  AND assignment.valid_until IS NULL
			  AND role.role_key = 'tenant.administrator'
			  AND role.status = 'active'
			  AND membership.status = 'active'
			  AND (membership.expires_at IS NULL OR membership.expires_at > now())
			  AND principal.status = 'active'
		) AS initialized`).Order("tenant.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&tenants).Error
	return tenants, total, wrapRepositoryError(err)
}

func (r *Repository) GetManagedTenantView(ctx context.Context, tenantID int64) (*ManagedTenant, error) {
	var tenant ManagedTenant
	err := r.db.WithContext(ctx).Raw(`
		SELECT tenant.*,
		       tenant.initialized_at IS NOT NULL AND EXISTS (
		           SELECT 1 FROM system.role_assignments assignment
		           JOIN system.roles role ON role.id = assignment.role_id
		           JOIN system.tenant_memberships membership
		             ON membership.tenant_id = assignment.tenant_id
		            AND membership.principal_id = assignment.principal_id
		           JOIN system.principals principal ON principal.id = assignment.principal_id
		           WHERE assignment.tenant_id = tenant.id
		             AND assignment.scope_type = 'tenant'
		             AND assignment.status = 'active'
		             AND assignment.valid_from <= now()
		             AND assignment.valid_until IS NULL
		             AND role.role_key = 'tenant.administrator'
		             AND role.status = 'active'
		             AND membership.status = 'active'
		             AND (membership.expires_at IS NULL OR membership.expires_at > now())
		             AND principal.status = 'active'
		       ) AS initialized
		FROM system.tenants tenant WHERE tenant.id = ?
	`, tenantID).Scan(&tenant).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	if tenant.ID == 0 {
		return nil, commonapi.ErrNotFound
	}
	return &tenant, nil
}

func (r *Repository) ListTenantAdministratorCandidates(ctx context.Context, search string, limit int) ([]TenantAdministratorCandidate, error) {
	query := r.db.WithContext(ctx).Table("system.principals principal").
		Joins("JOIN system.users user_profile ON user_profile.id = principal.id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = principal.id").
		Where("principal.principal_type = 'user' AND principal.status = 'active'").
		Where(`NOT EXISTS (
			SELECT 1 FROM system.role_assignments assignment
			WHERE assignment.principal_id = principal.id
			  AND assignment.scope_type = 'platform'
			  AND assignment.status = 'active'
			  AND assignment.valid_from <= now()
			  AND (assignment.valid_until IS NULL OR assignment.valid_until > now())
		)`)
	if normalized := strings.TrimSpace(search); normalized != "" {
		pattern := "%" + normalized + "%"
		query = query.Where("user_profile.display_name ILIKE ? OR user_profile.primary_email ILIKE ? OR account.username ILIKE ?", pattern, pattern, pattern)
	}
	var candidates []TenantAdministratorCandidate
	err := query.Select("principal.id AS principal_id, user_profile.display_name, user_profile.primary_email, account.username").
		Order("user_profile.display_name ASC, principal.id ASC").Limit(limit).Scan(&candidates).Error
	return candidates, wrapRepositoryError(err)
}

func (r *Repository) TenantHasMembershipOrAssignment(ctx context.Context, tenantID int64) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (
		SELECT 1 FROM system.tenant_memberships WHERE tenant_id = ?
		UNION ALL
		SELECT 1 FROM system.role_assignments WHERE tenant_id = ?
	)`, tenantID, tenantID).Scan(&exists).Error
	return exists, wrapRepositoryError(err)
}

func (r *Repository) MarkTenantInitialized(
	ctx context.Context,
	tenantID int64,
	actorPrincipalID int64,
	initializedAt time.Time,
) error {
	result := r.db.WithContext(ctx).Model(&Tenant{}).
		Where("id = ? AND initialized_at IS NULL AND initialized_by_principal_id IS NULL", tenantID).
		Updates(map[string]any{
			"initialized_at":              initializedAt,
			"initialized_by_principal_id": actorPrincipalID,
		})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) GetActiveBuiltinRoleByKey(ctx context.Context, roleKey string) (*Role, error) {
	var role Role
	err := r.db.WithContext(ctx).Where("tenant_id IS NULL AND role_key = ? AND status = 'active'", roleKey).Take(&role).Error
	return &role, wrapRepositoryError(err)
}

func (r *Repository) ListBuiltinServiceRuntimeBindings(ctx context.Context) ([]BuiltinServiceRuntimeBinding, error) {
	var bindings []BuiltinServiceRuntimeBinding
	err := r.db.WithContext(ctx).Raw(`
		SELECT service_principal.id AS principal_id,
		       service_principal.name AS service_name,
		       role.id AS role_id,
		       role.role_key
		FROM system.service_principals service_principal
		JOIN system.principals principal
		  ON principal.id = service_principal.id
		 AND principal.principal_type = 'service_principal'
		 AND principal.status = 'active'
		JOIN system.roles role
		  ON role.tenant_id IS NULL
		 AND role.role_key = 'tenant.' || replace(service_principal.name, 'addp-', '') || '_runtime'
		 AND role.role_type = 'tenant_builtin'
		 AND role.status = 'active'
		WHERE service_principal.owner_scope = 'platform'
		  AND service_principal.name IN (
		      'addp-agent', 'addp-asset', 'addp-catalog', 'addp-copilot', 'addp-develop', 'addp-document', 'addp-duckdb', 'addp-geopython', 'addp-graph', 'addp-manager', 'addp-meta', 'addp-model', 'addp-model3d', 'addp-monitor',
		      'addp-orchestrator', 'addp-pointcloud', 'addp-quality', 'addp-security', 'addp-service', 'addp-spark', 'addp-standard', 'addp-transfer'
		  )
		ORDER BY service_principal.name
	`).Scan(&bindings).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	if len(bindings) != len(builtinTenantRuntimeServiceClientIDs) {
		return nil, commonapi.ErrConflict
	}
	return bindings, nil
}

func (r *Repository) CreateTenantRoleAssignment(ctx context.Context, assignment *RoleAssignment) error {
	if assignment == nil {
		return commonapi.ErrBadRequest
	}
	err := r.db.WithContext(ctx).Create(assignment).Error
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" &&
		postgresError.ConstraintName == "uq_role_assignments_active_scope" {
		return ErrTenantRoleAssignmentAlreadyExists
	}
	return wrapRepositoryError(err)
}

func (r *Repository) ListTenantRoles(ctx context.Context, tenantID int64) ([]TenantRole, error) {
	var roles []TenantRole
	err := r.db.WithContext(ctx).Raw(`
		SELECT role.*, COALESCE(array_agg(permission.permission_key ORDER BY permission.permission_key)
		       FILTER (WHERE permission.id IS NOT NULL), ARRAY[]::text[]) AS permission_keys
		FROM system.roles role
		LEFT JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		LEFT JOIN system.permissions permission ON permission.id = role_permission.permission_id AND permission.status = 'active'
		WHERE role.status = 'active'
		  AND ((role.role_type = 'tenant_builtin' AND role.tenant_id IS NULL) OR
		       (role.role_type = 'tenant_custom' AND role.tenant_id = ?))
		GROUP BY role.id
		ORDER BY role.role_type, role.role_key
	`, tenantID).Scan(&roles).Error
	return roles, wrapRepositoryError(err)
}

func (r *Repository) ListTenantAssignablePermissions(ctx context.Context) ([]TenantAssignablePermission, error) {
	var permissions []TenantAssignablePermission
	err := r.db.WithContext(ctx).Table("system.permissions").
		Select("permission_key, owner_module, action, risk_level, allowed_scope_types, name_i18n_key, description_i18n_key").
		Where("status = 'active' AND tenant_customizable = true").
		Order("permission_key ASC").Scan(&permissions).Error
	return permissions, wrapRepositoryError(err)
}

func (r *Repository) TenantCustomRoleKeyConflictsWithBuiltin(ctx context.Context, roleKey string) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM system.roles
			WHERE tenant_id IS NULL AND role_key = ?
		)
	`, roleKey).Scan(&exists).Error
	return exists, wrapRepositoryError(err)
}

func (r *Repository) LockTenantRole(ctx context.Context, tenantID, roleID int64) (*Role, error) {
	var role Role
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", roleID, tenantID).Take(&role).Error
	return &role, wrapRepositoryError(err)
}

func (r *Repository) TenantRoleHasHighRiskPermission(ctx context.Context, roleID int64) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM system.role_permissions role_permission
			JOIN system.permissions permission ON permission.id = role_permission.permission_id
			WHERE role_permission.role_id = ?
			  AND permission.status = 'active'
			  AND permission.risk_level IN ('high', 'critical')
		)
	`, roleID).Scan(&exists).Error
	return exists, wrapRepositoryError(err)
}

func (r *Repository) CreateTenantCustomRole(ctx context.Context, role *Role, permissionKeys []string) error {
	if err := r.db.WithContext(ctx).Create(role).Error; err != nil {
		return wrapRepositoryError(err)
	}
	return r.replaceTenantRolePermissions(ctx, role.ID, permissionKeys, *role.CreatedByPrincipalID)
}

func (r *Repository) UpdateTenantCustomRole(ctx context.Context, roleID int64, name, description string, scopes []string, permissionKeys []string, actorID int64) error {
	result := r.db.WithContext(ctx).Model(&Role{}).Where("id = ?", roleID).Updates(map[string]any{
		"name": name, "description": description, "allowed_scope_types": pq.Array(scopes),
	})
	if result.Error != nil || result.RowsAffected != 1 {
		if result.Error != nil {
			return wrapRepositoryError(result.Error)
		}
		return commonapi.ErrNotFound
	}
	return r.replaceTenantRolePermissions(ctx, roleID, permissionKeys, actorID)
}

func (r *Repository) replaceTenantRolePermissions(ctx context.Context, roleID int64, permissionKeys []string, actorID int64) error {
	if err := r.db.WithContext(ctx).Exec("DELETE FROM system.role_permissions WHERE role_id = ?", roleID).Error; err != nil {
		return wrapRepositoryError(err)
	}
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
		SELECT ?, permission.id, 'tenant', ?
		FROM system.permissions permission
		WHERE permission.permission_key IN ? AND permission.status = 'active' AND permission.tenant_customizable = true
	`, roleID, actorID, permissionKeys)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != int64(len(permissionKeys)) {
		return commonapi.ErrBadRequest
	}
	return nil
}

func (r *Repository) ListActiveRoleHolderPrincipalIDs(ctx context.Context, roleID int64) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Raw(`SELECT principal.id FROM system.principals principal
		WHERE EXISTS (SELECT 1 FROM system.role_assignments assignment
		WHERE assignment.role_id = ? AND assignment.principal_id = principal.id AND assignment.status = 'active')
		ORDER BY principal.id FOR UPDATE OF principal`, roleID).Scan(&ids).Error
	return ids, wrapRepositoryError(err)
}

func (r *Repository) DisableTenantCustomRole(ctx context.Context, roleID, actorID int64, reason string, at time.Time) error {
	if err := r.db.WithContext(ctx).Model(&RoleAssignment{}).
		Where("role_id = ? AND status = 'active'", roleID).
		Updates(map[string]any{"status": "revoked", "revoked_by_principal_id": actorID, "revoked_at": at, "revoked_reason": reason}).Error; err != nil {
		return wrapRepositoryError(err)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Model(&Role{}).Where("id = ?", roleID).Update("status", "disabled").Error)
}

func (r *Repository) ListTenantRoleAssignments(ctx context.Context, tenantID int64, filter TenantRoleAssignmentFilter, page, pageSize int) ([]ManagedTenantRoleAssignment, int64, error) {
	base := r.db.WithContext(ctx).Table("system.role_assignments assignment").Where("assignment.tenant_id = ?", tenantID)
	if filter.MembershipID != nil {
		base = base.Where("EXISTS (SELECT 1 FROM system.tenant_memberships filtered_membership WHERE filtered_membership.id = ? AND filtered_membership.tenant_id = assignment.tenant_id AND filtered_membership.principal_id = assignment.principal_id)", *filter.MembershipID)
	}
	if filter.PrincipalType != nil {
		base = base.Where("EXISTS (SELECT 1 FROM system.principals filtered_principal WHERE filtered_principal.id = assignment.principal_id AND filtered_principal.principal_type = ?)", *filter.PrincipalType)
	}
	effectiveState := "effective"
	if filter.EffectiveState != nil {
		effectiveState = *filter.EffectiveState
	}
	switch effectiveState {
	case "scheduled":
		base = base.Where("assignment.status = 'active' AND assignment.valid_from > now()")
	case "effective":
		base = base.Where("assignment.status = 'active' AND assignment.valid_from <= now() AND (assignment.valid_until IS NULL OR assignment.valid_until > now())")
	case "expired":
		base = base.Where("assignment.status = 'active' AND assignment.valid_until IS NOT NULL AND assignment.valid_until <= now()")
	case "revoked":
		base = base.Where("assignment.status = 'revoked'")
	}
	if filter.ScopeType != nil {
		base = base.Where("assignment.scope_type = ?", *filter.ScopeType)
	}
	if filter.DepartmentID != nil {
		base = base.Where("assignment.department_id = ?", *filter.DepartmentID)
	}
	if filter.ProjectGroupID != nil {
		base = base.Where("assignment.project_group_id = ?", *filter.ProjectGroupID)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var assignments []ManagedTenantRoleAssignment
	err := withManagedTenantRoleAssignmentProjection(base).
		Order("assignment.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&assignments).Error
	return assignments, total, wrapRepositoryError(err)
}

func (r *Repository) GetManagedTenantRoleAssignment(
	ctx context.Context,
	tenantID int64,
	assignmentID int64,
) (*ManagedTenantRoleAssignment, error) {
	var assignment ManagedTenantRoleAssignment
	err := withManagedTenantRoleAssignmentProjection(
		r.db.WithContext(ctx).Table("system.role_assignments assignment"),
	).
		Where("assignment.id = ? AND assignment.tenant_id = ?", assignmentID, tenantID).
		Limit(1).
		Scan(&assignment).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	if assignment.ID == 0 {
		return nil, commonapi.ErrNotFound
	}
	return &assignment, nil
}

func withManagedTenantRoleAssignmentProjection(query *gorm.DB) *gorm.DB {
	return query.Select(`assignment.*, membership.id AS membership_id, principal.principal_type,
			COALESCE(user_profile.display_name, service_principal.name) AS display_name, account.username,
			service_principal.name AS service_principal_name,
			role.role_key, role.name AS role_name, role.name_i18n_key AS role_name_i18n_key,
			CASE
				WHEN assignment.status = 'revoked' THEN 'revoked'
				WHEN assignment.valid_from > now() THEN 'scheduled'
				WHEN assignment.valid_until IS NOT NULL AND assignment.valid_until <= now() THEN 'expired'
				ELSE 'effective'
			END AS effective_state,
			CASE WHEN assignment.status = 'revoked' THEN (
				SELECT active_assignment.id
				FROM system.role_assignments active_assignment
				WHERE active_assignment.principal_id = assignment.principal_id
				  AND active_assignment.role_id = assignment.role_id
				  AND active_assignment.scope_type = assignment.scope_type
				  AND active_assignment.tenant_id IS NOT DISTINCT FROM assignment.tenant_id
				  AND active_assignment.department_id IS NOT DISTINCT FROM assignment.department_id
				  AND active_assignment.project_group_id IS NOT DISTINCT FROM assignment.project_group_id
				  AND active_assignment.status = 'active'
				  AND active_assignment.valid_from <= now()
				  AND (active_assignment.valid_until IS NULL OR active_assignment.valid_until > now())
				ORDER BY active_assignment.id DESC
				LIMIT 1
			) END AS same_scope_active_assignment_id,
			granted_actor.principal_type AS granted_by_principal_type,
			COALESCE(granted_user.display_name, granted_service.name) AS granted_by_display_name,
			COALESCE(granted_account.username, granted_service.name) AS granted_by_identifier,
			granted_actor.status AS granted_by_status,
			revoked_actor.principal_type AS revoked_by_principal_type,
			COALESCE(revoked_user.display_name, revoked_service.name) AS revoked_by_display_name,
			COALESCE(revoked_account.username, revoked_service.name) AS revoked_by_identifier,
			revoked_actor.status AS revoked_by_status`).
		Joins("JOIN system.roles role ON role.id = assignment.role_id").
		Joins("JOIN system.tenant_memberships membership ON membership.tenant_id = assignment.tenant_id AND membership.principal_id = assignment.principal_id").
		Joins("JOIN system.principals principal ON principal.id = assignment.principal_id").
		Joins("LEFT JOIN system.users user_profile ON user_profile.id = assignment.principal_id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = assignment.principal_id").
		Joins("LEFT JOIN system.service_principals service_principal ON service_principal.id = assignment.principal_id").
		Joins("LEFT JOIN system.principals granted_actor ON granted_actor.id = assignment.created_by_principal_id").
		Joins("LEFT JOIN system.users granted_user ON granted_user.id = granted_actor.id").
		Joins("LEFT JOIN system.local_accounts granted_account ON granted_account.user_id = granted_actor.id").
		Joins("LEFT JOIN system.service_principals granted_service ON granted_service.id = granted_actor.id").
		Joins("LEFT JOIN system.principals revoked_actor ON revoked_actor.id = assignment.revoked_by_principal_id").
		Joins("LEFT JOIN system.users revoked_user ON revoked_user.id = revoked_actor.id").
		Joins("LEFT JOIN system.local_accounts revoked_account ON revoked_account.user_id = revoked_actor.id").
		Joins("LEFT JOIN system.service_principals revoked_service ON revoked_service.id = revoked_actor.id")
}

func (r *Repository) LockTenantRoleAssignment(ctx context.Context, tenantID, assignmentID int64) (*RoleAssignment, error) {
	var assignment RoleAssignment
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", assignmentID, tenantID).Take(&assignment).Error
	return &assignment, wrapRepositoryError(err)
}

func (r *Repository) RevokeTenantRoleAssignment(ctx context.Context, assignmentID, actorID int64, reason string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&RoleAssignment{}).Where("id = ? AND status = 'active'", assignmentID).
		Updates(map[string]any{"status": "revoked", "revoked_by_principal_id": actorID, "revoked_at": at, "revoked_reason": reason})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}
