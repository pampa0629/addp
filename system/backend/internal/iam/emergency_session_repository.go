package iam

import (
	"context"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func (r *Repository) emergencyTime(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := r.db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now.UTC(), wrapRepositoryError(err)
}

func (r *Repository) emergencyRequest(ctx context.Context, id uuid.UUID, lock bool) (*EmergencySessionRequest, error) {
	var row EmergencySessionRequest
	query := r.db.WithContext(ctx).Where("id = ?", id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Take(&row).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	if err := r.db.WithContext(ctx).Where("request_id = ?", id).Order("role_key").Find(&row.Approvals).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &row, nil
}

func (r *Repository) decorateEmergencyRequest(ctx context.Context, row *EmergencySessionRequest) error {
	now, err := r.emergencyTime(ctx)
	if err != nil {
		return err
	}
	row.EffectiveState = row.effectiveState(now)
	if err := r.db.WithContext(ctx).Table("system.tenants").Select("name").Where("id = ?", row.TenantID).Scan(&row.TenantName).Error; err != nil {
		return wrapRepositoryError(err)
	}
	for id, dst := range map[int64]*string{row.ExecutorPrincipalID: &row.ExecutorName, row.TargetPrincipalID: &row.TargetName} {
		if err := r.db.WithContext(ctx).Table("system.users").Select("display_name").Where("id = ?", id).Scan(dst).Error; err != nil {
			return wrapRepositoryError(err)
		}
	}
	for i := range row.Approvals {
		if err := r.db.WithContext(ctx).Table("system.users").Select("display_name").Where("id = ?", row.Approvals[i].PrincipalID).Scan(&row.Approvals[i].DisplayName).Error; err != nil {
			return wrapRepositoryError(err)
		}
	}
	return nil
}

func (r *Repository) updateEmergencyRequest(ctx context.Context, row *EmergencySessionRequest, values map[string]any) error {
	values["version"] = row.Version + 1
	result := r.db.WithContext(ctx).Model(&EmergencySessionRequest{}).Where("id = ? AND version = ?", row.ID, row.Version).Updates(values)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) hasEmergencyPlatformRole(ctx context.Context, id int64, role string, now time.Time) (bool, error) {
	q := r.db.WithContext(ctx).Table("system.role_assignments a").Joins("JOIN system.roles r ON r.id=a.role_id").
		Where("a.principal_id=? AND a.scope_type='platform' AND a.status='active' AND a.valid_from<=? AND (a.valid_until IS NULL OR a.valid_until>?) AND r.status='active'", id, now, now)
	if role != "" {
		q = q.Where("r.role_key=?", role)
	}
	var count int64
	err := q.Count(&count).Error
	return count > 0, wrapRepositoryError(err)
}
