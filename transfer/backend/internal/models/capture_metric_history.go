package models

import "time"

const CaptureMetricRetention = 7 * 24 * time.Hour

// CaptureMetricHistory contains only the public task observation projection.
// It deliberately has no foreign key to a deletable capture resource.
type CaptureMetricHistory struct {
	ID            uint      `gorm:"primaryKey"`
	TenantID      uint      `gorm:"not null;uniqueIndex:uq_capture_metric_minute,priority:1"`
	TaskID        uint      `gorm:"not null"`
	ApplyIdentity string    `gorm:"type:uuid;not null;uniqueIndex:uq_capture_metric_minute,priority:2"`
	Generation    uint64    `gorm:"not null;uniqueIndex:uq_capture_metric_minute,priority:3"`
	Minute        time.Time `gorm:"not null;uniqueIndex:uq_capture_metric_minute,priority:4;index:idx_capture_metric_expiry"`
	Point         JSONMap   `gorm:"type:jsonb;not null"`
}

func (CaptureMetricHistory) TableName() string { return "transfer.capture_metric_history" }

// CaptureMetricPoint preserves independent source timestamps and validity.
// Timestamp is a display interval, RecordedAt is the actual owner observation.
type CaptureMetricPoint struct {
	Timestamp             time.Time                     `json:"timestamp"`
	RecordedAt            *time.Time                    `json:"recorded_at"`
	ConsumptionState      string                        `json:"consumption_state"`
	BufferObservation     *CaptureBufferObservation     `json:"buffer_observation"`
	PostgreSQLObservation *CapturePostgreSQLObservation `json:"postgresql_observation"`
	DelayObservation      *CaptureDelayObservation      `json:"delay_observation"`
}

type CaptureMetricHistoryResponse struct {
	TaskID      uint                 `json:"task_id"`
	Generation  uint64               `json:"generation"`
	Start       time.Time            `json:"start"`
	End         time.Time            `json:"end"`
	StepSeconds int64                `json:"step_seconds"`
	QueriedAt   time.Time            `json:"queried_at"`
	Points      []CaptureMetricPoint `json:"points"`
}
