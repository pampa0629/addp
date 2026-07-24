package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

var ErrSchemaChangeRequestNotFound = errors.New("schema change request not found")
var ErrSchemaChangeRequestConflict = errors.New("schema change request conflict")

type SchemaChangeRequestRepository struct {
	db *gorm.DB
}

func NewSchemaChangeRequestRepository(db *gorm.DB) *SchemaChangeRequestRepository {
	return &SchemaChangeRequestRepository{db: db}
}

func (r *SchemaChangeRequestRepository) GetPending(ctx context.Context, taskID, tenantID uint) (*models.SchemaChangeRequest, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("schema change request repository is not configured")
	}
	var request models.SchemaChangeRequest
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND tenant_id = ? AND status = ?", taskID, tenantID, models.SchemaChangeRequestPending).
		Order("detected_at DESC, id DESC").
		First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSchemaChangeRequestNotFound
	}
	return &request, err
}

func (r *SchemaChangeRequestRepository) GetLatest(ctx context.Context, taskID, tenantID uint) (*models.SchemaChangeRequest, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("schema change request repository is not configured")
	}
	var request models.SchemaChangeRequest
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
		Order("detected_at DESC, id DESC").
		First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSchemaChangeRequestNotFound
	}
	return &request, err
}

func schemaChangeDiff(change ContinuousSchemaChange) models.JSONMap {
	return models.JSONMap{
		"missing_fields":      append([]string(nil), change.MissingFields...),
		"unexpected_fields":   append([]string(nil), change.UnexpectedFields...),
		"incompatible_fields": append([]string(nil), change.IncompatibleFields...),
	}
}
