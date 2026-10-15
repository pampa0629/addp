package models

import (
	"github.com/addp/common/logpipeline"
	"time"
)

type LogPipelinePolicy struct {
	ID              uint      `gorm:"primaryKey" json:"-"`
	Version         uint64    `gorm:"not null;default:1" json:"version"`
	FailureSamples  int       `gorm:"not null" json:"failure_samples"`
	RecoverySamples int       `gorm:"not null" json:"recovery_samples"`
	StaleSeconds    int       `gorm:"not null" json:"stale_seconds"`
	DelayMS         int64     `gorm:"not null" json:"delay_ms"`
	CapacityPercent int       `gorm:"not null" json:"capacity_percent"`
	RecoveryPercent int       `gorm:"not null" json:"recovery_percent"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (LogPipelinePolicy) TableName() string { return "monitor.log_pipeline_policy" }

type LogSignalState struct {
	Failures   int    `json:"failures"`
	Successes  int    `json:"successes"`
	Active     bool   `json:"active"`
	Severity   string `json:"severity"`
	InstanceID string `json:"instance_id,omitempty"`
}
type LogPipelineNode struct {
	Node          string                    `gorm:"primaryKey;size:100" json:"node"`
	ReceivedAt    *time.Time                `json:"received_at,omitempty"`
	StartedAt     time.Time                 `json:"started_at"`
	Observation   logpipeline.Observation   `gorm:"serializer:json;type:jsonb;not null" json:"observation"`
	Signals       map[string]LogSignalState `gorm:"serializer:json;type:jsonb;not null" json:"-"`
	RegistryValid bool                      `json:"registry_valid"`
}

func (LogPipelineNode) TableName() string { return "monitor.log_pipeline_nodes" }

type LogObserverBoot struct {
	BootID string `gorm:"primaryKey;size:100"`
	Node   string `gorm:"not null;size:100;index"`
}

func (LogObserverBoot) TableName() string { return "monitor.log_observer_boots" }
