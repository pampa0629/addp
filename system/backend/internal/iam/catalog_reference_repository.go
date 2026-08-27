package iam

import (
	"context"
	"strings"
)

func (r *Repository) ResolveCatalogDepartments(
	ctx context.Context,
	tenantID int64,
	ids []int64,
) ([]CatalogDepartmentProjection, error) {
	result := make([]CatalogDepartmentProjection, 0)
	if len(ids) == 0 {
		return result, nil
	}
	err := r.db.WithContext(ctx).
		Table("system.departments").
		Select("id, name, code, status").
		Where("tenant_id = ? AND id IN ?", tenantID, ids).
		Scan(&result).Error
	return result, wrapRepositoryError(err)
}

func (r *Repository) ResolveCatalogUsers(
	ctx context.Context,
	tenantID int64,
	ids []int64,
) ([]CatalogUserProjection, error) {
	result := make([]CatalogUserProjection, 0)
	if len(ids) == 0 {
		return result, nil
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT users.id,
		       users.display_name,
		       principals.status AS principal_status,
		       memberships.status AS membership_status,
		       (principals.status = 'active'
		        AND memberships.status = 'active'
		        AND (memberships.expires_at IS NULL OR memberships.expires_at > CURRENT_TIMESTAMP)) AS referenceable
		FROM system.users users
		JOIN system.principals principals
		  ON principals.id = users.id AND principals.principal_type = 'user'
		JOIN system.tenant_memberships memberships
		  ON memberships.principal_id = users.id AND memberships.tenant_id = ?
		WHERE users.id IN ?
		ORDER BY users.id,
		         CASE WHEN principals.status = 'active'
		                    AND memberships.status = 'active'
		                    AND (memberships.expires_at IS NULL OR memberships.expires_at > CURRENT_TIMESTAMP)
		              THEN 0 ELSE 1 END,
		         memberships.id DESC
	`, tenantID, ids).Scan(&result).Error
	return result, wrapRepositoryError(err)
}

func (r *Repository) ResolveCatalogProjectGroups(
	ctx context.Context,
	tenantID int64,
	ids []int64,
) ([]CatalogProjectGroupProjection, error) {
	result := make([]CatalogProjectGroupProjection, 0)
	if len(ids) == 0 {
		return result, nil
	}
	err := r.db.WithContext(ctx).
		Table("system.project_groups").
		Select("id, name, code, status").
		Where("tenant_id = ? AND id IN ?", tenantID, ids).
		Scan(&result).Error
	return result, wrapRepositoryError(err)
}

func (r *Repository) ListCatalogDepartmentCandidates(
	ctx context.Context,
	tenantID int64,
	search string,
	page, pageSize int,
) ([]CatalogReferenceCandidate, int64, error) {
	query := r.db.WithContext(ctx).Table("system.departments").
		Where("tenant_id = ? AND status = ?", tenantID, DepartmentStatusActive)
	if search = strings.TrimSpace(search); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("name ILIKE ? OR code ILIKE ?", pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	items := make([]CatalogReferenceCandidate, 0)
	err := query.Select("'department' AS subject_type, id, name, code, status").
		Order("LOWER(name) ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error
	return items, total, wrapRepositoryError(err)
}

func (r *Repository) ListCatalogUserCandidates(
	ctx context.Context,
	tenantID int64,
	search string,
	page, pageSize int,
) ([]CatalogReferenceCandidate, int64, error) {
	query := r.db.WithContext(ctx).Table("system.users AS user_profile").
		Joins("JOIN system.principals AS principal ON principal.id = user_profile.id AND principal.principal_type = 'user'").
		Joins("LEFT JOIN system.local_accounts AS account ON account.user_id = user_profile.id").
		Where("principal.status = 'active'").
		Where(`EXISTS (
			SELECT 1 FROM system.tenant_memberships AS membership
			WHERE membership.tenant_id = ?
			  AND membership.principal_id = user_profile.id
			  AND membership.status = 'active'
			  AND (membership.expires_at IS NULL OR membership.expires_at > CURRENT_TIMESTAMP)
		)`, tenantID)
	if search = strings.TrimSpace(search); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("user_profile.display_name ILIKE ? OR account.username ILIKE ?", pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	items := make([]CatalogReferenceCandidate, 0)
	err := query.Select("'user' AS subject_type, user_profile.id, user_profile.display_name AS name, COALESCE(account.username, '') AS code, 'active' AS status").
		Order("LOWER(user_profile.display_name) ASC, user_profile.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error
	return items, total, wrapRepositoryError(err)
}
