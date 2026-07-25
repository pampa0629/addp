package iam

import (
	"context"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/lib/pq"
)

type SessionCredentialAuthSnapshot struct {
	CredentialID                  int64
	CredentialFamilyID            int64
	CredentialExpiresAt           time.Time
	CredentialRevokedAt           *time.Time
	CredentialCreatedAt           time.Time
	CredentialOwner               *string
	CredentialScopes              pq.StringArray `gorm:"type:text[]"`
	CredentialAgentRunID          *string
	CredentialToolCallID          *string
	SourceAccessTokenID           *int64
	SourceAccessTokenExpiresAt    *time.Time
	SourceAccessTokenRevokedAt    *time.Time
	SourceAccessTokenCreatedAt    *time.Time
	FamilyPrincipalID             int64
	FamilyContextType             ContextType
	FamilyTenantMembershipID      *int64
	FamilyAuthorizationVersion    int64
	FamilyClientID                string
	FamilyAuthType                string
	FamilyAudiences               pq.StringArray `gorm:"type:text[]"`
	FamilyScopes                  pq.StringArray `gorm:"type:text[]"`
	FamilyAuthenticationMethods   pq.StringArray `gorm:"type:text[]"`
	FamilyAssuranceLevel          AssuranceLevel
	FamilyAuthenticatedAt         time.Time
	FamilyStepUpExpiresAt         *time.Time
	FamilyExpiresAt               time.Time
	FamilyRevokedAt               *time.Time
	PrincipalType                 PrincipalType
	PrincipalStatus               PrincipalStatus
	PrincipalAuthorizationVersion int64
	TenantMembershipID            *int64
	TenantID                      *int64
	TenantMembershipStatus        *TenantMembershipStatus
	TenantMembershipJoinedAt      *time.Time
	TenantMembershipExpiresAt     *time.Time
	TenantStatus                  *TenantStatus
	DatabaseTime                  time.Time
}

type DepartmentMembershipProjection struct {
	MembershipID   int64
	DepartmentID   int64
	MembershipType string
	RelationRole   string
	AncestorIDs    pq.Int64Array `gorm:"type:bigint[]"`
}

type ProjectGroupMembershipProjection struct {
	MembershipID   int64
	ProjectGroupID int64
	RelationRole   string
}

type RoleAssignmentPermissionProjection struct {
	AssignmentID   int64
	RoleKey        string
	ScopeType      string
	TenantID       *int64
	DepartmentID   *int64
	ProjectGroupID *int64
	SourceType     string
	ValidFrom      time.Time
	ValidUntil     *time.Time
	PermissionKey  string
}

func (r *Repository) GetAccessTokenAuthSnapshot(
	ctx context.Context,
	tokenHash string,
) (*SessionCredentialAuthSnapshot, error) {
	var snapshot SessionCredentialAuthSnapshot
	result := r.db.WithContext(ctx).Raw(`
		SELECT
			access_token.id AS credential_id,
			access_token.family_id AS credential_family_id,
			access_token.expires_at AS credential_expires_at,
			access_token.revoked_at AS credential_revoked_at,
			access_token.created_at AS credential_created_at,
			NULL::text AS credential_owner,
			family.principal_id AS family_principal_id,
			family.context_type AS family_context_type,
			family.tenant_membership_id AS family_tenant_membership_id,
			family.issued_authorization_version AS family_authorization_version,
			family.client_id AS family_client_id,
			family.auth_type AS family_auth_type,
			family.audiences AS family_audiences,
			family.scopes AS family_scopes,
			family.authentication_methods AS family_authentication_methods,
			family.assurance_level AS family_assurance_level,
			family.authenticated_at AS family_authenticated_at,
			family.step_up_expires_at AS family_step_up_expires_at,
			family.expires_at AS family_expires_at,
			family.revoked_at AS family_revoked_at,
			principal.principal_type,
			principal.status AS principal_status,
			principal.authorization_version AS principal_authorization_version,
			membership.id AS tenant_membership_id,
			membership.tenant_id,
			membership.status AS tenant_membership_status,
			membership.joined_at AS tenant_membership_joined_at,
			membership.expires_at AS tenant_membership_expires_at,
			tenant.status AS tenant_status,
			transaction_timestamp() AS database_time
		FROM system.access_tokens access_token
		JOIN system.refresh_token_families family ON family.id = access_token.family_id
		JOIN system.principals principal ON principal.id = family.principal_id
		LEFT JOIN system.tenant_memberships membership ON membership.id = family.tenant_membership_id
		LEFT JOIN system.tenants tenant ON tenant.id = membership.tenant_id
		WHERE access_token.token_hash = ?
	`, tokenHash).Scan(&snapshot)
	if result.Error != nil {
		return nil, wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, commonapi.ErrNotFound
	}
	return &snapshot, nil
}

func (r *Repository) GetResourceAccessTicketAuthSnapshot(
	ctx context.Context,
	tokenHash string,
) (*SessionCredentialAuthSnapshot, error) {
	var snapshot SessionCredentialAuthSnapshot
	result := r.db.WithContext(ctx).Raw(`
		SELECT
			ticket.id AS credential_id,
			ticket.family_id AS credential_family_id,
			ticket.expires_at AS credential_expires_at,
			ticket.revoked_at AS credential_revoked_at,
			ticket.created_at AS credential_created_at,
			ticket.owner AS credential_owner,
			family.principal_id AS family_principal_id,
			family.context_type AS family_context_type,
			family.tenant_membership_id AS family_tenant_membership_id,
			family.issued_authorization_version AS family_authorization_version,
			family.client_id AS family_client_id,
			family.auth_type AS family_auth_type,
			family.audiences AS family_audiences,
			family.scopes AS family_scopes,
			family.authentication_methods AS family_authentication_methods,
			family.assurance_level AS family_assurance_level,
			family.authenticated_at AS family_authenticated_at,
			family.step_up_expires_at AS family_step_up_expires_at,
			family.expires_at AS family_expires_at,
			family.revoked_at AS family_revoked_at,
			principal.principal_type,
			principal.status AS principal_status,
			principal.authorization_version AS principal_authorization_version,
			membership.id AS tenant_membership_id,
			membership.tenant_id,
			membership.status AS tenant_membership_status,
			membership.joined_at AS tenant_membership_joined_at,
			membership.expires_at AS tenant_membership_expires_at,
			tenant.status AS tenant_status,
			transaction_timestamp() AS database_time
		FROM system.resource_access_tickets ticket
		JOIN system.refresh_token_families family ON family.id = ticket.family_id
		JOIN system.principals principal ON principal.id = family.principal_id
		LEFT JOIN system.tenant_memberships membership ON membership.id = family.tenant_membership_id
		LEFT JOIN system.tenants tenant ON tenant.id = membership.tenant_id
		WHERE ticket.token_hash = ?
	`, tokenHash).Scan(&snapshot)
	if result.Error != nil {
		return nil, wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, commonapi.ErrNotFound
	}
	return &snapshot, nil
}

func (r *Repository) GetDelegatedAccessTokenAuthSnapshot(
	ctx context.Context,
	tokenHash string,
) (*SessionCredentialAuthSnapshot, error) {
	var snapshot SessionCredentialAuthSnapshot
	result := r.db.WithContext(ctx).Raw(`
		SELECT
			delegated.id AS credential_id,
			source.family_id AS credential_family_id,
			delegated.expires_at AS credential_expires_at,
			delegated.revoked_at AS credential_revoked_at,
			delegated.created_at AS credential_created_at,
			delegated.audience AS credential_owner,
			delegated.scopes AS credential_scopes,
			delegated.agent_run_id AS credential_agent_run_id,
			delegated.tool_call_id AS credential_tool_call_id,
			source.id AS source_access_token_id,
			source.expires_at AS source_access_token_expires_at,
			source.revoked_at AS source_access_token_revoked_at,
			source.created_at AS source_access_token_created_at,
			family.principal_id AS family_principal_id,
			family.context_type AS family_context_type,
			family.tenant_membership_id AS family_tenant_membership_id,
			family.issued_authorization_version AS family_authorization_version,
			family.client_id AS family_client_id,
			family.auth_type AS family_auth_type,
			family.audiences AS family_audiences,
			family.scopes AS family_scopes,
			family.authentication_methods AS family_authentication_methods,
			family.assurance_level AS family_assurance_level,
			family.authenticated_at AS family_authenticated_at,
			family.step_up_expires_at AS family_step_up_expires_at,
			family.expires_at AS family_expires_at,
			family.revoked_at AS family_revoked_at,
			principal.principal_type,
			principal.status AS principal_status,
			principal.authorization_version AS principal_authorization_version,
			membership.id AS tenant_membership_id,
			membership.tenant_id,
			membership.status AS tenant_membership_status,
			membership.joined_at AS tenant_membership_joined_at,
			membership.expires_at AS tenant_membership_expires_at,
			tenant.status AS tenant_status,
			transaction_timestamp() AS database_time
		FROM system.delegated_access_tokens delegated
		JOIN system.access_tokens source ON source.id = delegated.source_access_token_id
		JOIN system.refresh_token_families family ON family.id = source.family_id
		JOIN system.principals principal ON principal.id = family.principal_id
		LEFT JOIN system.tenant_memberships membership ON membership.id = family.tenant_membership_id
		LEFT JOIN system.tenants tenant ON tenant.id = membership.tenant_id
		WHERE delegated.token_hash = ?
	`, tokenHash).Scan(&snapshot)
	if result.Error != nil {
		return nil, wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, commonapi.ErrNotFound
	}
	return &snapshot, nil
}

func (r *Repository) ListEffectiveDepartmentMemberships(
	ctx context.Context,
	tenantMembershipID int64,
	tenantID int64,
) ([]DepartmentMembershipProjection, error) {
	var memberships []DepartmentMembershipProjection
	err := r.db.WithContext(ctx).Raw(`
		WITH RECURSIVE direct_memberships AS (
			SELECT
				membership.id AS membership_id,
				membership.department_id,
				membership.membership_type,
				membership.relation_role,
				department.parent_id
			FROM system.department_memberships membership
			JOIN system.departments department
			  ON department.id = membership.department_id
			 AND department.tenant_id = membership.tenant_id
			WHERE membership.tenant_membership_id = ?
			  AND membership.tenant_id = ?
			  AND membership.status = 'active'
			  AND department.status = 'active'
		), ancestors AS (
			SELECT direct.department_id AS origin_id, parent.id AS ancestor_id, parent.parent_id, 1 AS depth
			FROM direct_memberships direct
			JOIN system.departments parent ON parent.id = direct.parent_id AND parent.tenant_id = ?
			UNION ALL
			SELECT ancestor.origin_id, parent.id, parent.parent_id, ancestor.depth + 1
			FROM ancestors ancestor
			JOIN system.departments parent ON parent.id = ancestor.parent_id AND parent.tenant_id = ?
		)
		SELECT
			direct.membership_id,
			direct.department_id,
			direct.membership_type,
			direct.relation_role,
			COALESCE(
				array_agg(ancestor.ancestor_id ORDER BY ancestor.depth DESC)
					FILTER (WHERE ancestor.ancestor_id IS NOT NULL),
				ARRAY[]::bigint[]
			) AS ancestor_ids
		FROM direct_memberships direct
		LEFT JOIN ancestors ancestor ON ancestor.origin_id = direct.department_id
		GROUP BY direct.membership_id, direct.department_id, direct.membership_type, direct.relation_role
		ORDER BY direct.department_id ASC
	`, tenantMembershipID, tenantID, tenantID, tenantID).Scan(&memberships).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return memberships, nil
}

func (r *Repository) ListEffectiveProjectGroupMemberships(
	ctx context.Context,
	tenantMembershipID int64,
	tenantID int64,
) ([]ProjectGroupMembershipProjection, error) {
	var memberships []ProjectGroupMembershipProjection
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			membership.id AS membership_id,
			membership.project_group_id,
			membership.relation_role
		FROM system.project_group_memberships membership
		JOIN system.project_groups project_group
		  ON project_group.id = membership.project_group_id
		 AND project_group.tenant_id = membership.tenant_id
		WHERE membership.tenant_membership_id = ?
		  AND membership.tenant_id = ?
		  AND membership.status = 'active'
		  AND project_group.status <> 'closed'
		ORDER BY membership.project_group_id ASC
	`, tenantMembershipID, tenantID).Scan(&memberships).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return memberships, nil
}

func (r *Repository) ListEffectiveRoleAssignmentPermissions(
	ctx context.Context,
	principalID int64,
	principalType PrincipalType,
	contextType ContextType,
	tenantID *int64,
	tenantMembershipID *int64,
	at time.Time,
) ([]RoleAssignmentPermissionProjection, error) {
	var rows []RoleAssignmentPermissionProjection
	query := `
		SELECT
			assignment.id AS assignment_id,
			role.role_key,
			assignment.scope_type,
			assignment.tenant_id,
			assignment.department_id,
			assignment.project_group_id,
			assignment.source_type,
			assignment.valid_from,
			assignment.valid_until,
			permission.permission_key
		FROM system.role_assignments assignment
		JOIN system.roles role ON role.id = assignment.role_id
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE assignment.principal_id = ?
		  AND assignment.status = 'active'
		  AND assignment.valid_from <= ?
		  AND (assignment.valid_until IS NULL OR assignment.valid_until > ?)
		  AND role.status = 'active'
		  AND ? = ANY(role.allowed_principal_types)
		  AND permission.status = 'active'
	`
	arguments := []any{principalID, at, at, principalType}
	if contextType == ContextTypePlatform {
		query += ` AND assignment.scope_type = 'platform' `
	} else if contextType == ContextTypeTenant && tenantID != nil && tenantMembershipID != nil {
		query += `
			AND assignment.tenant_id = ?
			AND (
				assignment.scope_type = 'tenant'
				OR (
					assignment.scope_type = 'department'
					AND EXISTS (
						SELECT 1
						FROM system.department_memberships membership
						JOIN system.departments department
						  ON department.id = membership.department_id
						 AND department.tenant_id = membership.tenant_id
						WHERE membership.tenant_membership_id = ?
						  AND membership.tenant_id = assignment.tenant_id
						  AND membership.department_id = assignment.department_id
						  AND membership.status = 'active'
						  AND department.status = 'active'
					)
				)
				OR (
					assignment.scope_type = 'project_group'
					AND EXISTS (
						SELECT 1
						FROM system.project_group_memberships membership
						JOIN system.project_groups project_group
						  ON project_group.id = membership.project_group_id
						 AND project_group.tenant_id = membership.tenant_id
						WHERE membership.tenant_membership_id = ?
						  AND membership.tenant_id = assignment.tenant_id
						  AND membership.project_group_id = assignment.project_group_id
						  AND membership.status = 'active'
						  AND project_group.status <> 'closed'
					)
				)
			)
		`
		arguments = append(arguments, *tenantID, *tenantMembershipID, *tenantMembershipID)
	} else {
		return nil, commonapi.ErrBadRequest
	}
	query += `
		ORDER BY
			CASE assignment.scope_type
				WHEN 'platform' THEN 0
				WHEN 'tenant' THEN 1
				WHEN 'department' THEN 2
				WHEN 'project_group' THEN 3
			END,
			CASE assignment.scope_type
				WHEN 'tenant' THEN assignment.tenant_id
				WHEN 'department' THEN assignment.department_id
				WHEN 'project_group' THEN assignment.project_group_id
				ELSE 0
			END,
			role.role_key,
			assignment.id,
			permission.permission_key
	`
	if err := r.db.WithContext(ctx).Raw(query, arguments...).Scan(&rows).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return rows, nil
}
