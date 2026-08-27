package iam

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrOrganizationVersionConflict = errors.Join(commonapi.ErrConflict, errors.New("organization resource version conflict"))

type ManagedOrganizationMembership struct {
	ID                 int64
	TenantID           int64
	OrganizationID     int64
	TenantMembershipID int64
	PrincipalID        int64
	DisplayName        string
	Username           *string
	MembershipType     *DepartmentMembershipType
	DepartmentRole     *DepartmentRelationRole
	ProjectGroupRole   *ProjectGroupRelationRole
	Status             OrganizationMembershipStatus
	EndedAt            *time.Time
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (r *Repository) ListDepartments(ctx context.Context, tenantID int64, page, pageSize int, search string, status *DepartmentStatus) ([]Department, int64, error) {
	query := r.db.WithContext(ctx).Model(&Department{}).Where("tenant_id = ?", tenantID)
	if normalized := strings.TrimSpace(search); normalized != "" {
		pattern := "%" + normalized + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ?", pattern, pattern)
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var departments []Department
	err := query.Order("parent_id NULLS FIRST, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&departments).Error
	return departments, total, wrapRepositoryError(err)
}

func (r *Repository) GetDepartment(ctx context.Context, tenantID, departmentID int64) (*Department, error) {
	var department Department
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, departmentID).Take(&department).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &department, nil
}

func (r *Repository) LockDepartment(ctx context.Context, tenantID, departmentID int64) (*Department, error) {
	var department Department
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id = ?", tenantID, departmentID).Take(&department).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &department, nil
}

func (r *Repository) LockDepartmentStructure(ctx context.Context, tenantID int64) error {
	lockKey := "addp.system.department_structure:" + strconv.FormatInt(tenantID, 10)
	return wrapRepositoryError(r.db.WithContext(ctx).
		Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error)
}

func (r *Repository) IsDepartmentDescendant(ctx context.Context, tenantID, rootID, candidateID int64) (bool, error) {
	var descendant bool
	err := r.db.WithContext(ctx).Raw(`
		WITH RECURSIVE descendants(id) AS (
			SELECT id
			FROM system.departments
			WHERE tenant_id = ? AND parent_id = ?
			UNION
			SELECT child.id
			FROM system.departments child
			JOIN descendants parent ON child.parent_id = parent.id
			WHERE child.tenant_id = ?
		)
		SELECT EXISTS (SELECT 1 FROM descendants WHERE id = ?)
	`, tenantID, rootID, tenantID, candidateID).Scan(&descendant).Error
	return descendant, wrapRepositoryError(err)
}

func (r *Repository) CreateDepartment(ctx context.Context, department *Department) error {
	return wrapRepositoryError(r.db.WithContext(ctx).Create(department).Error)
}

func (r *Repository) UpdateDepartment(ctx context.Context, tenantID, departmentID, version int64, parentID *int64, name string) error {
	result := r.db.WithContext(ctx).Model(&Department{}).
		Where("tenant_id = ? AND id = ? AND version = ?", tenantID, departmentID, version).
		Updates(map[string]any{"parent_id": parentID, "name": name, "version": gorm.Expr("version + 1")})
	return r.organizationWriteResult(ctx, result, &Department{}, tenantID, departmentID)
}

func (r *Repository) UpdateDepartmentStatus(ctx context.Context, tenantID, departmentID, version int64, status DepartmentStatus) error {
	result := r.db.WithContext(ctx).Model(&Department{}).
		Where("tenant_id = ? AND id = ? AND version = ?", tenantID, departmentID, version).
		Updates(map[string]any{"status": status, "version": gorm.Expr("version + 1")})
	return r.organizationWriteResult(ctx, result, &Department{}, tenantID, departmentID)
}

func (r *Repository) CountActiveDepartmentChildren(ctx context.Context, tenantID, departmentID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&Department{}).
		Where("tenant_id = ? AND parent_id = ? AND status = ?", tenantID, departmentID, DepartmentStatusActive).
		Count(&count).Error
	return count, wrapRepositoryError(err)
}

func (r *Repository) ListDepartmentMemberships(ctx context.Context, tenantID, departmentID int64, page, pageSize int, status *OrganizationMembershipStatus) ([]ManagedOrganizationMembership, int64, error) {
	base := r.db.WithContext(ctx).Table("system.department_memberships membership").
		Where("membership.tenant_id = ? AND membership.department_id = ?", tenantID, departmentID)
	if status != nil {
		base = base.Where("membership.status = ?", *status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var memberships []ManagedOrganizationMembership
	err := base.Select(`
		membership.id, membership.tenant_id, membership.department_id AS organization_id,
		membership.tenant_membership_id, tenant_membership.principal_id,
		user_profile.display_name, account.username,
		membership.membership_type, membership.relation_role AS department_role,
		membership.status, membership.ended_at, membership.version,
		membership.created_at, membership.updated_at
	`).Joins("JOIN system.tenant_memberships tenant_membership ON tenant_membership.id = membership.tenant_membership_id AND tenant_membership.tenant_id = membership.tenant_id").
		Joins("JOIN system.users user_profile ON user_profile.id = tenant_membership.principal_id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = tenant_membership.principal_id").
		Order("membership.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&memberships).Error
	return memberships, total, wrapRepositoryError(err)
}

func (r *Repository) GetDepartmentMembership(ctx context.Context, tenantID, departmentID, membershipID int64) (*ManagedOrganizationMembership, error) {
	var membership ManagedOrganizationMembership
	err := r.db.WithContext(ctx).Table("system.department_memberships membership").Select(`
		membership.id, membership.tenant_id, membership.department_id AS organization_id,
		membership.tenant_membership_id, tenant_membership.principal_id,
		user_profile.display_name, account.username,
		membership.membership_type, membership.relation_role AS department_role,
		membership.status, membership.ended_at, membership.version,
		membership.created_at, membership.updated_at
	`).Joins("JOIN system.tenant_memberships tenant_membership ON tenant_membership.id = membership.tenant_membership_id AND tenant_membership.tenant_id = membership.tenant_id").
		Joins("JOIN system.users user_profile ON user_profile.id = tenant_membership.principal_id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = tenant_membership.principal_id").
		Where("membership.tenant_id = ? AND membership.department_id = ? AND membership.id = ?", tenantID, departmentID, membershipID).
		Take(&membership).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &membership, nil
}

func (r *Repository) CreateDepartmentMembership(ctx context.Context, membership *DepartmentMembership) error {
	return wrapRepositoryError(r.db.WithContext(ctx).Create(membership).Error)
}

func (r *Repository) UpdateDepartmentMembership(ctx context.Context, tenantID, departmentID, membershipID, version int64, membershipType DepartmentMembershipType, role DepartmentRelationRole) error {
	result := r.db.WithContext(ctx).Model(&DepartmentMembership{}).
		Where("tenant_id = ? AND department_id = ? AND id = ? AND version = ? AND status = ?", tenantID, departmentID, membershipID, version, OrganizationMembershipStatusActive).
		Updates(map[string]any{"membership_type": membershipType, "relation_role": role, "version": gorm.Expr("version + 1")})
	return r.organizationMembershipWriteResult(ctx, result, "system.department_memberships", tenantID, departmentID, membershipID)
}

func (r *Repository) CloseDepartmentMembership(ctx context.Context, tenantID, departmentID, membershipID, version int64, endedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&DepartmentMembership{}).
		Where("tenant_id = ? AND department_id = ? AND id = ? AND version = ? AND status = ?", tenantID, departmentID, membershipID, version, OrganizationMembershipStatusActive).
		Updates(map[string]any{"status": OrganizationMembershipStatusEnded, "ended_at": endedAt, "version": gorm.Expr("version + 1")})
	return r.organizationMembershipWriteResult(ctx, result, "system.department_memberships", tenantID, departmentID, membershipID)
}

func (r *Repository) ListProjectGroups(ctx context.Context, tenantID int64, page, pageSize int, search string, status *ProjectGroupStatus) ([]ProjectGroup, int64, error) {
	query := r.db.WithContext(ctx).Model(&ProjectGroup{}).Where("tenant_id = ?", tenantID)
	if normalized := strings.TrimSpace(search); normalized != "" {
		pattern := "%" + normalized + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ?", pattern, pattern)
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var groups []ProjectGroup
	err := query.Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&groups).Error
	return groups, total, wrapRepositoryError(err)
}

func (r *Repository) GetProjectGroup(ctx context.Context, tenantID, groupID int64) (*ProjectGroup, error) {
	var group ProjectGroup
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, groupID).Take(&group).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &group, nil
}

func (r *Repository) LockProjectGroup(ctx context.Context, tenantID, groupID int64) (*ProjectGroup, error) {
	var group ProjectGroup
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id = ?", tenantID, groupID).Take(&group).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &group, nil
}

func (r *Repository) CreateProjectGroup(ctx context.Context, group *ProjectGroup) error {
	return wrapRepositoryError(r.db.WithContext(ctx).Create(group).Error)
}

func (r *Repository) UpdateProjectGroup(ctx context.Context, tenantID, groupID, version int64, name, description string, status ProjectGroupStatus, startsAt, endsAt *time.Time) error {
	result := r.db.WithContext(ctx).Model(&ProjectGroup{}).
		Where("tenant_id = ? AND id = ? AND version = ?", tenantID, groupID, version).
		Updates(map[string]any{"name": name, "description": description, "status": status, "starts_at": startsAt, "ends_at": endsAt, "version": gorm.Expr("version + 1")})
	return r.organizationWriteResult(ctx, result, &ProjectGroup{}, tenantID, groupID)
}

func (r *Repository) CloseProjectGroup(ctx context.Context, tenantID, groupID, version int64) error {
	result := r.db.WithContext(ctx).Model(&ProjectGroup{}).
		Where("tenant_id = ? AND id = ? AND version = ? AND status <> ?", tenantID, groupID, version, ProjectGroupStatusClosed).
		Updates(map[string]any{"status": ProjectGroupStatusClosed, "version": gorm.Expr("version + 1")})
	return r.organizationWriteResult(ctx, result, &ProjectGroup{}, tenantID, groupID)
}

func (r *Repository) ListProjectGroupMemberships(ctx context.Context, tenantID, groupID int64, page, pageSize int, status *OrganizationMembershipStatus) ([]ManagedOrganizationMembership, int64, error) {
	base := r.db.WithContext(ctx).Table("system.project_group_memberships membership").
		Where("membership.tenant_id = ? AND membership.project_group_id = ?", tenantID, groupID)
	if status != nil {
		base = base.Where("membership.status = ?", *status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var memberships []ManagedOrganizationMembership
	err := base.Select(`
		membership.id, membership.tenant_id, membership.project_group_id AS organization_id,
		membership.tenant_membership_id, tenant_membership.principal_id,
		user_profile.display_name, account.username,
		membership.relation_role AS project_group_role,
		membership.status, membership.ended_at, membership.version,
		membership.created_at, membership.updated_at
	`).Joins("JOIN system.tenant_memberships tenant_membership ON tenant_membership.id = membership.tenant_membership_id AND tenant_membership.tenant_id = membership.tenant_id").
		Joins("JOIN system.users user_profile ON user_profile.id = tenant_membership.principal_id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = tenant_membership.principal_id").
		Order("membership.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&memberships).Error
	return memberships, total, wrapRepositoryError(err)
}

func (r *Repository) GetProjectGroupMembership(ctx context.Context, tenantID, groupID, membershipID int64) (*ManagedOrganizationMembership, error) {
	var membership ManagedOrganizationMembership
	err := r.db.WithContext(ctx).Table("system.project_group_memberships membership").Select(`
		membership.id, membership.tenant_id, membership.project_group_id AS organization_id,
		membership.tenant_membership_id, tenant_membership.principal_id,
		user_profile.display_name, account.username,
		membership.relation_role AS project_group_role,
		membership.status, membership.ended_at, membership.version,
		membership.created_at, membership.updated_at
	`).Joins("JOIN system.tenant_memberships tenant_membership ON tenant_membership.id = membership.tenant_membership_id AND tenant_membership.tenant_id = membership.tenant_id").
		Joins("JOIN system.users user_profile ON user_profile.id = tenant_membership.principal_id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = tenant_membership.principal_id").
		Where("membership.tenant_id = ? AND membership.project_group_id = ? AND membership.id = ?", tenantID, groupID, membershipID).
		Take(&membership).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &membership, nil
}

func (r *Repository) CreateProjectGroupMembership(ctx context.Context, membership *ProjectGroupMembership) error {
	return wrapRepositoryError(r.db.WithContext(ctx).Create(membership).Error)
}

func (r *Repository) UpdateProjectGroupMembership(ctx context.Context, tenantID, groupID, membershipID, version int64, role ProjectGroupRelationRole) error {
	result := r.db.WithContext(ctx).Model(&ProjectGroupMembership{}).
		Where("tenant_id = ? AND project_group_id = ? AND id = ? AND version = ? AND status = ?", tenantID, groupID, membershipID, version, OrganizationMembershipStatusActive).
		Updates(map[string]any{"relation_role": role, "version": gorm.Expr("version + 1")})
	return r.organizationMembershipWriteResult(ctx, result, "system.project_group_memberships", tenantID, groupID, membershipID)
}

func (r *Repository) CloseProjectGroupMembership(ctx context.Context, tenantID, groupID, membershipID, version int64, endedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&ProjectGroupMembership{}).
		Where("tenant_id = ? AND project_group_id = ? AND id = ? AND version = ? AND status = ?", tenantID, groupID, membershipID, version, OrganizationMembershipStatusActive).
		Updates(map[string]any{"status": OrganizationMembershipStatusEnded, "ended_at": endedAt, "version": gorm.Expr("version + 1")})
	return r.organizationMembershipWriteResult(ctx, result, "system.project_group_memberships", tenantID, groupID, membershipID)
}

func (r *Repository) organizationWriteResult(ctx context.Context, result *gorm.DB, model any, tenantID, id int64) error {
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(model).Where("tenant_id = ? AND id = ?", tenantID, id).Count(&count).Error; err != nil {
		return wrapRepositoryError(err)
	}
	if count == 0 {
		return commonapi.ErrNotFound
	}
	return ErrOrganizationVersionConflict
}

func (r *Repository) organizationMembershipWriteResult(ctx context.Context, result *gorm.DB, table string, tenantID, organizationID, membershipID int64) error {
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected == 1 {
		return nil
	}
	organizationColumn := "department_id"
	if table == "system.project_group_memberships" {
		organizationColumn = "project_group_id"
	}
	var count int64
	if err := r.db.WithContext(ctx).Table(table).
		Where("tenant_id = ? AND "+organizationColumn+" = ? AND id = ?", tenantID, organizationID, membershipID).
		Count(&count).Error; err != nil {
		return wrapRepositoryError(err)
	}
	if count == 0 {
		return commonapi.ErrNotFound
	}
	return ErrOrganizationVersionConflict
}
