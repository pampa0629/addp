package iam

import "context"

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
