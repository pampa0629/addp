package models

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/addp/monitor/internal/resourcequery"
)

type ResourcePressurePolicy struct {
	TriggerPercent  float64 `json:"trigger_percent"`
	RecoveryPercent float64 `json:"recovery_percent"`
	TriggerSeconds  int     `json:"trigger_seconds"`
	RecoverySeconds int     `json:"recovery_seconds"`
}

func (p ResourcePressurePolicy) Valid() bool {
	return !math.IsNaN(p.TriggerPercent) && !math.IsInf(p.TriggerPercent, 0) &&
		!math.IsNaN(p.RecoveryPercent) && !math.IsInf(p.RecoveryPercent, 0) &&
		p.RecoveryPercent > 0 && p.RecoveryPercent < p.TriggerPercent && p.TriggerPercent <= 100 &&
		p.TriggerSeconds >= int(resourcequery.MinimumStepSeconds) && p.TriggerSeconds <= 300 && p.TriggerSeconds%int(resourcequery.MinimumStepSeconds) == 0 &&
		p.RecoverySeconds >= int(resourcequery.MinimumStepSeconds) && p.RecoverySeconds <= 300 && p.RecoverySeconds%int(resourcequery.MinimumStepSeconds) == 0
}

type ResourceAlertRule struct {
	ID                     string                   `gorm:"primaryKey;size:36" json:"id"`
	Version                uint64                   `gorm:"not null" json:"version"`
	Name                   string                   `gorm:"not null;size:100" json:"name"`
	NodeID                 string                   `gorm:"not null;size:36;uniqueIndex:uq_resource_rule_mount,priority:1" json:"node_id"`
	MetricKey              string                   `gorm:"not null;size:100;uniqueIndex:uq_resource_rule_mount,priority:2" json:"metric_key"`
	Dimensions             resourcequery.Dimensions `gorm:"serializer:json;type:jsonb;not null;default:'{}'" json:"dimensions"`
	MountKey               string                   `gorm:"not null;size:64;default:'';uniqueIndex:uq_resource_rule_mount,priority:3" json:"-"`
	ResourcePressurePolicy `gorm:"embedded"`
	Enabled                bool       `gorm:"not null" json:"enabled"`
	WindowSignature        string     `gorm:"size:64" json:"-"`
	WindowSince            *time.Time `json:"window_since,omitempty"`
	LastEvaluatedAt        *time.Time `json:"last_evaluated_at,omitempty"`
	LastVerdict            string     `gorm:"size:20" json:"last_verdict"`
	LastError              string     `gorm:"size:40" json:"last_error"`
	LastSampleAt           *time.Time `json:"last_sample_at,omitempty"`
	LastValue              *float64   `json:"last_value,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func (ResourceAlertRule) TableName() string { return "monitor.resource_alert_rules" }

// Immutable evidence belongs to the lifecycle event; it contains no transport
// endpoint, credential or mutable reference needed to interpret old thresholds.
const FilesystemCapacityMetric = "node.filesystem.used_percent"

// A selector hash is an observation key, never a physical-volume identity.
func ResourceMountKey(d resourcequery.Dimensions) string {
	if len(d) == 0 {
		return ""
	}
	raw, _ := json.Marshal(d)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func ResourceAlertSignal(metric string, d resourcequery.Dimensions) string {
	if metric == FilesystemCapacityMetric {
		return metric + ":" + ResourceMountKey(d)
	}
	return metric
}

type FilesystemCapacityEvidence struct {
	Dimensions     resourcequery.Dimensions `json:"dimensions"`
	TotalBytes     float64                  `json:"total_bytes"`
	UsedBytes      float64                  `json:"used_bytes"`
	AvailableBytes float64                  `json:"available_bytes"`
}
type ResourceAlertEvidence struct {
	Filesystem         *FilesystemCapacityEvidence `json:"filesystem,omitempty"`
	RuleName           string                      `json:"rule_name,omitempty"`
	RuleID             string                      `json:"rule_id"`
	RuleVersion        uint64                      `json:"rule_version"`
	Policy             ResourcePressurePolicy      `json:"policy"`
	NodeVersion        int64                       `json:"node_version"`
	TargetID           string                      `json:"target_id"`
	TargetVersion      int64                       `json:"target_version"`
	QueryPolicyVersion uint64                      `json:"query_policy_version"`
	EvaluatedAt        time.Time                   `json:"evaluated_at"`
	SampledFrom        time.Time                   `json:"sampled_from"`
	SampledTo          time.Time                   `json:"sampled_to"`
	Samples            int                         `json:"samples"`
	Minimum            float64                     `json:"minimum"`
	Maximum            float64                     `json:"maximum"`
	Latest             float64                     `json:"latest"`
}
