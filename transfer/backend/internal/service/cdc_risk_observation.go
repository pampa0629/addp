package service

import (
	"context"
	commonClient "github.com/addp/common/client"
	"github.com/addp/transfer/internal/models"
	"time"
)

// ReadCDCRisks publishes the current CDC inventory, independently of execution retention.
func (s *TaskService) ReadCDCRisks(ctx context.Context, afterID uint) (*commonClient.TransferCDCRisks, error) {
	var tasks []models.TransferTask
	if err := s.db.WithContext(ctx).Where("id > ? AND config -> 'runtime' ->> 'boundary' = ? AND config -> 'load' -> 'change_detection' ->> 'type' = ?", afterID, "continuous", "cdc").Order("id ASC").Limit(commonClient.TransferCDCRiskBatchSize + 1).Find(&tasks).Error; err != nil {
		return nil, err
	}
	more := len(tasks) > commonClient.TransferCDCRiskBatchSize
	if more {
		tasks = tasks[:commonClient.TransferCDCRiskBatchSize]
	}
	now := time.Now().UTC()
	result := &commonClient.TransferCDCRisks{SchemaVersion: commonClient.TransferCDCRiskSchema, ObservedAt: now, Items: make([]commonClient.TransferCDCRiskItem, 0, len(tasks)), NextAfterID: afterID, HasMore: more}
	for i := range tasks {
		task := &tasks[i]
		row := commonClient.TransferCDCRiskItem{TenantID: task.TenantID, TaskID: task.ID, State: "unknown", Source: commonClient.CDCRiskObservation{State: "unknown"}, Buffer: commonClient.CDCRiskObservation{State: "unknown"}}
		if s.captureControl != nil {
			capture, err := s.captureControl.Get(ctx, task.ID, task.TenantID)
			if err == nil && capture != nil {
				row = cdcTaskRisks(task, capture, now)
			}
		}
		result.Items = append(result.Items, row)
		result.NextAfterID = task.ID
	}
	return result, nil
}

func cdcTaskRisks(task *models.TransferTask, capture *models.CaptureResource, now time.Time) commonClient.TransferCDCRiskItem {
	unknown := commonClient.CDCRiskObservation{State: "unknown"}
	result := commonClient.TransferCDCRiskItem{TenantID: task.TenantID, TaskID: task.ID, Generation: capture.Generation, State: "unknown", Source: unknown, Buffer: unknown}
	if capture.Status == models.CaptureStatusStopped {
		result.State = "closed"
		return result
	}
	if capture.Status != models.CaptureStatusRunning && capture.Status != models.CaptureStatusFailed || task.LastExecutionID == nil {
		return result
	}
	result.ExecutionID = *task.LastExecutionID
	result.State = "observing"
	summary := models.NewCaptureSummary(capture)
	if value := summary.PostgreSQLObservation; value != nil && !value.SampledAt.IsZero() && !value.SampledAt.After(now) && value.ExpiresAt.After(now) {
		risk := commonClient.CDCRiskObservation{State: "unknown", SampledAt: &value.SampledAt, ExpiresAt: &value.ExpiresAt}
		if value.Status == "missing" {
			risk.State = "active"
			risk.Reason = "slot_missing"
		}
		if value.Status == "available" {
			switch value.RiskState {
			case "lost":
				risk.State = "active"
				risk.Reason = "wal_lost"
			case "reserved", "extended", "unreserved":
				risk.State = "clear"
			}
		}
		result.Source = risk
	}
	if value := summary.BufferObservation; value != nil && value.Status == "available" && !value.SampledAt.IsZero() && !value.SampledAt.After(now) && value.ExpiresAt.After(now) {
		risk := commonClient.CDCRiskObservation{State: "unknown", SampledAt: &value.SampledAt, ExpiresAt: &value.ExpiresAt}
		switch value.RecoveryState {
		case "lost":
			risk.State = "active"
			risk.Reason = "buffer_position_lost"
		case "available":
			risk.State = "clear"
		}
		result.Buffer = risk
	}
	return result
}
