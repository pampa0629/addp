package iam

import (
	"context"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm/clause"
)

func (r *Repository) CreatePrivilegedChangeRequest(
	ctx context.Context,
	request *PrivilegedChangeRequest,
) error {
	if request == nil {
		return commonapi.ErrBadRequest
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(request).Error)
}

func (r *Repository) CreatePrivilegedChangeApproval(
	ctx context.Context,
	approval *PrivilegedChangeApproval,
) error {
	if approval == nil {
		return commonapi.ErrBadRequest
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(approval).Error)
}

func (r *Repository) GetPrivilegedChangeRequest(
	ctx context.Context,
	requestID int64,
) (*PrivilegedChangeRequest, error) {
	var request PrivilegedChangeRequest
	if err := r.db.WithContext(ctx).First(&request, requestID).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &request, nil
}

func (r *Repository) LockPrivilegedChangeRequest(
	ctx context.Context,
	requestID int64,
) (*PrivilegedChangeRequest, error) {
	var request PrivilegedChangeRequest
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&request, requestID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &request, nil
}

func (r *Repository) ListPrivilegedIdentityChangeRequests(
	ctx context.Context,
	page int,
	pageSize int,
	status *PrivilegedChangeStatus,
	targetPrincipalID *int64,
) ([]PrivilegedChangeRequest, int64, error) {
	query := r.db.WithContext(ctx).Model(&PrivilegedChangeRequest{}).
		Where("change_type IN ?", []PrivilegedChangeType{
			PrivilegedChangePlatformIdentitySuspend,
			PrivilegedChangePlatformIdentityReactivate,
			PrivilegedChangePlatformIdentityDeactivate,
		})
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	if targetPrincipalID != nil {
		query = query.Where("target_principal_id = ?", *targetPrincipalID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var requests []PrivilegedChangeRequest
	if err := query.Order("requested_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&requests).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	return requests, total, nil
}
