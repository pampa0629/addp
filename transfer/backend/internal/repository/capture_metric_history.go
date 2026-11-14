package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordMetricHistory reads the committed generation, never a detached control
// snapshot. A shared row lock keeps irreversible Stop outside this short write.
func (r *CaptureRepository) RecordMetricHistory(ctx context.Context, id uint, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var resource models.CaptureResource
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Preload("PostgreSQL").
			Where("id = ? AND status IN ?", id, []models.CaptureStatus{models.CaptureStatusRunning, models.CaptureStatusFailed}).First(&resource).Error
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		var task models.TransferTask
		if err = tx.Where("id = ? AND tenant_id = ?", resource.TaskID, resource.TenantID).First(&task).Error; err == gorm.ErrRecordNotFound {
			return nil
		} else if err != nil {
			return err
		}
		summary := models.NewCaptureSummary(&resource)
		validAtRecording := func(sampled, expires time.Time) bool { return !sampled.After(now) && expires.After(now) }
		if value := summary.BufferObservation; value != nil && !validAtRecording(value.SampledAt, value.ExpiresAt) {
			summary.BufferObservation = nil
		}
		if value := summary.PostgreSQLObservation; value != nil && !validAtRecording(value.SampledAt, value.ExpiresAt) {
			summary.PostgreSQLObservation = nil
		}
		if value := summary.DelayObservation; value != nil && !validAtRecording(value.SampledAt, value.ExpiresAt) {
			summary.DelayObservation = nil
		}

		point := models.CaptureMetricPoint{Timestamp: now.UTC().Truncate(time.Minute), RecordedAt: &now, ConsumptionState: string(task.DesiredState),
			BufferObservation: summary.BufferObservation, PostgreSQLObservation: summary.PostgreSQLObservation, DelayObservation: summary.DelayObservation}
		data, err := json.Marshal(point)
		if err != nil {
			return err
		}
		var payload models.JSONMap
		if err = json.Unmarshal(data, &payload); err != nil {
			return err
		}
		row := models.CaptureMetricHistory{TenantID: task.TenantID, TaskID: task.ID, ApplyIdentity: task.ApplyIdentity,
			Generation: resource.Generation, Minute: now.UTC().Truncate(time.Minute), Point: payload}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	})
}

func (r *CaptureRepository) PruneMetricHistory(ctx context.Context, now time.Time) error {
	// The indexed subquery bounds one maintenance transaction, including when no
	// active task remains. Reads enforce retention independently of this batch.
	ids := r.db.Model(&models.CaptureMetricHistory{}).Select("id").Where("minute < ?", now.Add(-models.CaptureMetricRetention)).Order("minute ASC").Limit(1000)
	return r.db.WithContext(ctx).Where("id IN (?)", ids).Delete(&models.CaptureMetricHistory{}).Error
}

func (r *CaptureRepository) MetricHistory(ctx context.Context, task *models.TransferTask, generation uint64, start, end time.Time) ([]models.CaptureMetricHistory, error) {
	rows := []models.CaptureMetricHistory{}
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND task_id = ? AND apply_identity = ? AND generation = ? AND minute >= ? AND minute < ?",
		task.TenantID, task.ID, task.ApplyIdentity, generation, start, end).
		Order("minute ASC").Find(&rows).Error
	return rows, err
}
