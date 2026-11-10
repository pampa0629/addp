package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// ApplySchemaChangeRequestTx returns the stored approval facts for publishing the
// origin in the same transaction, including the database precision of applied_at.
func ApplySchemaChangeRequestTx(tx *gorm.DB, request *models.SchemaChangeRequest, mappings models.JSONMap, userID uint, now time.Time) error {
	return tx.Model(request).Clauses(clause.Returning{}).Updates(map[string]interface{}{
		"approved_mappings": mappings, "status": models.SchemaChangeRequestApplied,
		"applied_by": userID, "applied_at": now,
		"metadata_scan_status":      models.SchemaChangeMetadataScanPending,
		"metadata_scan_claim_token": "", "metadata_scan_lease_until": nil,
		"metadata_scan_attempt": 0, "metadata_scan_execution_id": "", "metadata_scan_error": "",
		"updated_at": now,
	}).Error
}

func (r *SchemaChangeRequestRepository) ClaimMetadataScan(
	ctx context.Context,
	requestID uint,
	now time.Time,
	claimTTL time.Duration,
) (*models.SchemaChangeRequest, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, fmt.Errorf("schema change request repository is not configured")
	}
	if claimTTL <= 0 {
		return nil, false, fmt.Errorf("schema change Meta scan claim TTL must be greater than zero")
	}
	var request models.SchemaChangeRequest
	owned := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		token := uuid.NewString()
		leaseUntil := now.Add(claimTTL)
		result := tx.Model(&models.SchemaChangeRequest{}).
			Where("id = ? AND status = ?", requestID, models.SchemaChangeRequestApplied).
			Where("metadata_scan_status = ? OR (metadata_scan_status = ? AND metadata_scan_lease_until <= ?)",
				models.SchemaChangeMetadataScanPending, models.SchemaChangeMetadataScanRunning, now).
			Updates(map[string]interface{}{
				"metadata_scan_status":       models.SchemaChangeMetadataScanRunning,
				"metadata_scan_claim_token":  token,
				"metadata_scan_lease_until":  leaseUntil,
				"metadata_scan_attempt":      gorm.Expr("metadata_scan_attempt + 1"),
				"metadata_scan_execution_id": "",
				"metadata_scan_error":        "",
				"updated_at":                 now,
			})
		if result.Error != nil {
			return result.Error
		}
		if err := tx.Where("id = ?", requestID).First(&request).Error; err != nil {
			return err
		}
		owned = result.RowsAffected == 1
		if !owned {
			return nil
		}
		return UpdateSchemaChangeExecutionProjectionTx(tx, &request)
	})
	return &request, owned, err
}

func (r *SchemaChangeRequestRepository) CompleteMetadataScan(
	ctx context.Context,
	requestID uint,
	claimToken string,
	status models.SchemaChangeMetadataScanStatus,
	executionID, errorMessage string,
	now time.Time,
) (*models.SchemaChangeRequest, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, fmt.Errorf("schema change request repository is not configured")
	}
	if claimToken == "" {
		return nil, false, fmt.Errorf("schema change Meta scan claim token is required")
	}
	if status != models.SchemaChangeMetadataScanSuccess && status != models.SchemaChangeMetadataScanFailed {
		return nil, false, fmt.Errorf("schema change Meta scan completion status %q is invalid", status)
	}
	var request models.SchemaChangeRequest
	owned := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.SchemaChangeRequest{}).
			Where("id = ? AND status = ? AND metadata_scan_status = ? AND metadata_scan_claim_token = ?",
				requestID, models.SchemaChangeRequestApplied, models.SchemaChangeMetadataScanRunning, claimToken).
			Updates(map[string]interface{}{
				"metadata_scan_status":       status,
				"metadata_scan_claim_token":  "",
				"metadata_scan_lease_until":  nil,
				"metadata_scan_execution_id": executionID,
				"metadata_scan_error":        errorMessage,
				"updated_at":                 now,
			})
		if result.Error != nil {
			return result.Error
		}
		if err := tx.Where("id = ?", requestID).First(&request).Error; err != nil {
			return err
		}
		owned = result.RowsAffected == 1
		if !owned {
			return nil
		}
		return UpdateSchemaChangeExecutionProjectionTx(tx, &request)
	})
	return &request, owned, err
}

func (r *SchemaChangeRequestRepository) getByID(ctx context.Context, requestID uint) (*models.SchemaChangeRequest, error) {
	var request models.SchemaChangeRequest
	err := r.db.WithContext(ctx).Where("id = ?", requestID).First(&request).Error
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
