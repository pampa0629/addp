package iam

import (
	"context"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm/clause"
)

func (r *Repository) CreateTenantInvitation(ctx context.Context, invitation *TenantInvitation) error {
	if invitation == nil {
		return fmt.Errorf("%w: tenant invitation is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(invitation).Error)
}

func (r *Repository) ListTenantInvitations(
	ctx context.Context,
	tenantID int64,
	page int,
	pageSize int,
	search string,
	status *TenantInvitationStatus,
) ([]TenantInvitation, int64, error) {
	query := r.db.WithContext(ctx).Model(&TenantInvitation{}).Where("tenant_id = ?", tenantID)
	if normalized := strings.TrimSpace(search); normalized != "" {
		query = query.Where("email ILIKE ?", "%"+normalized+"%")
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var invitations []TenantInvitation
	if err := query.Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&invitations).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	return invitations, total, nil
}

func (r *Repository) GetTenantInvitation(
	ctx context.Context,
	tenantID int64,
	invitationID int64,
) (*TenantInvitation, error) {
	var invitation TenantInvitation
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, invitationID).
		Take(&invitation).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &invitation, nil
}

func (r *Repository) LockTenantInvitationByID(
	ctx context.Context,
	tenantID int64,
	invitationID int64,
) (*TenantInvitation, error) {
	var invitation TenantInvitation
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id = ?", tenantID, invitationID).
		Take(&invitation).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &invitation, nil
}

func (r *Repository) LockTenantInvitationBySecretHash(
	ctx context.Context,
	secretHash string,
) (*TenantInvitation, error) {
	var invitation TenantInvitation
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("secret_hash = ?", secretHash).
		Take(&invitation).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &invitation, nil
}

func (r *Repository) ExpirePendingTenantInvitations(
	ctx context.Context,
	tenantID int64,
	now time.Time,
) ([]TenantInvitation, error) {
	var invitations []TenantInvitation
	err := r.db.WithContext(ctx).Model(&invitations).
		Clauses(clause.Returning{}).
		Where("tenant_id = ? AND status = ? AND expires_at <= ?", tenantID, TenantInvitationStatusPending, now).
		Updates(map[string]any{"status": TenantInvitationStatusExpired, "expired_at": now}).Error
	return invitations, wrapRepositoryError(err)
}

func (r *Repository) TransitionTenantInvitation(
	ctx context.Context,
	invitationID int64,
	status TenantInvitationStatus,
	principalID int64,
	at time.Time,
) error {
	updates := map[string]any{"status": status}
	switch status {
	case TenantInvitationStatusAccepted:
		updates["accepted_at"] = at
		updates["accepted_by_principal_id"] = principalID
	case TenantInvitationStatusRevoked:
		updates["revoked_at"] = at
		updates["revoked_by_principal_id"] = principalID
	case TenantInvitationStatusExpired:
		updates["expired_at"] = at
	default:
		return fmt.Errorf("%w: unsupported invitation transition", commonapi.ErrBadRequest)
	}
	result := r.db.WithContext(ctx).Model(&TenantInvitation{}).
		Where("id = ? AND status = ?", invitationID, TenantInvitationStatusPending).
		Updates(updates)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: tenant invitation is no longer pending", commonapi.ErrConflict)
	}
	return nil
}
