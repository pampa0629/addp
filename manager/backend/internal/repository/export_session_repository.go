package repository

import (
	"context"
	"errors"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type ExportSessionRepository struct {
	db *gorm.DB
}

func NewExportSessionRepository(db *gorm.DB) *ExportSessionRepository {
	return &ExportSessionRepository{db: db}
}

func (r *ExportSessionRepository) Create(ctx context.Context, session *models.ExportSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *ExportSessionRepository) Get(ctx context.Context, id uint, tenantID uint) (*models.ExportSession, error) {
	var session models.ExportSession
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &session, err
}

func (r *ExportSessionRepository) UpdateStatus(ctx context.Context, session *models.ExportSession) error {
	return r.db.WithContext(ctx).
		Model(&models.ExportSession{}).
		Where("id = ? AND tenant_id = ?", session.ID, session.TenantID).
		Updates(map[string]interface{}{
			"status":            session.Status,
			"error_message":     session.ErrorMessage,
			"artifact_manifest": session.ArtifactManifest,
		}).Error
}

func (r *ExportSessionRepository) MarkRunningExpired(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&models.ExportSession{}).
		Where("status IN ? AND created_at < ?", []string{models.ExportSessionStatusPending, models.ExportSessionStatusRunning}, before).
		Updates(map[string]interface{}{
			"status":        models.ExportSessionStatusFailed,
			"error_message": "export session expired before completion",
			"updated_at":    time.Now(),
		})
	return result.RowsAffected, result.Error
}

func (r *ExportSessionRepository) ListExpiredFinalSessions(ctx context.Context, successBefore, failedBefore time.Time, limit int) ([]*models.ExportSession, error) {
	if limit <= 0 {
		limit = 100
	}
	var sessions []*models.ExportSession
	err := r.db.WithContext(ctx).
		Where(
			"(status = ? AND updated_at < ?) OR (status = ? AND updated_at < ?)",
			models.ExportSessionStatusSuccess,
			successBefore,
			models.ExportSessionStatusFailed,
			failedBefore,
		).
		Order("updated_at ASC").
		Limit(limit).
		Find(&sessions).Error
	return sessions, err
}

func (r *ExportSessionRepository) Delete(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.ExportSession{}).Error
}
