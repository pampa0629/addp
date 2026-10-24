package models

import (
	"math"
	"time"
)

type ProcessThresholdPolicy struct {
	TriggerValue    float64 `json:"trigger_value"`
	RecoveryValue   float64 `json:"recovery_value"`
	TriggerSeconds  int     `json:"trigger_seconds"`
	RecoverySeconds int     `json:"recovery_seconds"`
}

func (p ProcessThresholdPolicy) Valid(metric string) bool {
	if math.IsNaN(p.TriggerValue) || math.IsInf(p.TriggerValue, 0) || math.IsNaN(p.RecoveryValue) || math.IsInf(p.RecoveryValue, 0) || p.RecoveryValue <= 0 || p.RecoveryValue >= p.TriggerValue || p.TriggerValue > 9007199254740991 || p.TriggerSeconds < 15 || p.TriggerSeconds > 300 || p.TriggerSeconds%15 != 0 || p.RecoverySeconds < 15 || p.RecoverySeconds > 300 || p.RecoverySeconds%15 != 0 {
		return false
	}
	return metric == "process.cpu.core_equivalents" || metric == "process.memory.resident_bytes"
}

type ProcessAlertRule struct {
	ID                     string `gorm:"primaryKey;size:36" json:"id"`
	SubjectKey             string `gorm:"not null;size:64;uniqueIndex" json:"-"`
	AnchorInstanceID       uint   `gorm:"not null" json:"anchor_instance_id"`
	Version                uint64 `gorm:"not null" json:"version"`
	Name                   string `gorm:"not null;size:100" json:"name"`
	ModuleName             string `gorm:"not null;size:50" json:"module_name"`
	Role                   string `gorm:"not null;size:20" json:"role"`
	RuntimeInstanceID      string `gorm:"not null;size:100" json:"runtime_instance_id"`
	ProcessStartedAt       string `gorm:"not null;size:40" json:"process_started_at"`
	MetricKey              string `gorm:"not null;size:100" json:"metric_key"`
	ProcessThresholdPolicy `gorm:"embedded"`
	Enabled                bool       `gorm:"not null" json:"enabled"`
	WindowSignature        string     `gorm:"size:64" json:"-"`
	WindowSince            *time.Time `json:"window_since"`
	LastEvaluatedAt        *time.Time `json:"last_evaluated_at"`
	LastVerdict            string     `gorm:"size:20" json:"last_verdict"`
	LastError              string     `gorm:"size:40" json:"last_error"`
	LastValue              *float64   `json:"last_value"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func (ProcessAlertRule) TableName() string { return "monitor.process_alert_rules" }

type ProcessAlertEvidence struct {
	RuleName           string                 `json:"rule_name,omitempty"`
	RuleID             string                 `json:"rule_id"`
	RuleVersion        uint64                 `json:"rule_version"`
	ModuleName         string                 `json:"module_name"`
	Role               string                 `json:"role"`
	AnchorInstanceID   uint                   `json:"anchor_instance_id"`
	RuntimeInstanceID  string                 `json:"runtime_instance_id"`
	ProcessStartedAt   string                 `json:"process_started_at"`
	MetricKey          string                 `json:"metric_key"`
	Policy             ProcessThresholdPolicy `json:"policy"`
	QueryPolicyVersion uint64                 `json:"query_policy_version"`
	EvaluatedAt        time.Time              `json:"evaluated_at"`
	SampledFrom        time.Time              `json:"sampled_from"`
	SampledTo          time.Time              `json:"sampled_to"`
	Samples            int                    `json:"samples"`
	Minimum            float64                `json:"minimum"`
	Maximum            float64                `json:"maximum"`
	Latest             float64                `json:"latest"`
}
