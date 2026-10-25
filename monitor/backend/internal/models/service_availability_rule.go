package models

import "time"

type ServiceAvailabilityPolicy struct {
	TriggerSeconds  int `json:"trigger_seconds"`
	RecoverySeconds int `json:"recovery_seconds"`
}

func (p ServiceAvailabilityPolicy) Valid() bool {
	return p.TriggerSeconds >= 15 && p.TriggerSeconds <= 300 && p.TriggerSeconds%15 == 0 && p.RecoverySeconds >= 15 && p.RecoverySeconds <= 300 && p.RecoverySeconds%15 == 0
}

type ServiceAvailabilityRule struct {
	ID                        string `gorm:"primaryKey;size:36" json:"id"`
	SubjectKey                string `gorm:"not null;size:64;uniqueIndex" json:"group_id"`
	Version                   uint64 `gorm:"not null" json:"version"`
	Name                      string `gorm:"not null;size:100" json:"name"`
	DeploymentID              string `gorm:"not null;size:64" json:"deployment_id"`
	ModuleName                string `gorm:"not null;size:50" json:"module_name"`
	Role                      string `gorm:"not null;size:20" json:"role"`
	RuntimeName               string `gorm:"not null;size:50" json:"runtime_name"`
	ServiceAvailabilityPolicy `gorm:"embedded"`
	Enabled                   bool       `gorm:"not null" json:"enabled"`
	ConfirmedSignature        string     `gorm:"size:64" json:"-"`
	ConfirmedAt               *time.Time `json:"confirmed_at"`
	WindowSignature           string     `gorm:"size:64" json:"-"`
	WindowSince               *time.Time `json:"window_since"`
	WindowSamples             int        `gorm:"not null;default:0" json:"-"`
	LastEvaluatedAt           *time.Time `json:"last_evaluated_at"`
	LastObservedAt            *time.Time `json:"last_observed_at"`
	LastVerdict               string     `gorm:"size:20" json:"last_verdict"`
	LastError                 string     `gorm:"size:40" json:"last_error"`
	OnlineReplicas            *int       `json:"online_replicas"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

func (ServiceAvailabilityRule) TableName() string { return "monitor.service_availability_rules" }

type ServiceAvailabilityEvidence struct {
	RuleName         string                    `json:"rule_name"`
	RuleID           string                    `json:"rule_id"`
	RuleVersion      uint64                    `json:"rule_version"`
	DeploymentID     string                    `json:"deployment_id"`
	ModuleName       string                    `json:"module_name"`
	Role             string                    `json:"role"`
	RuntimeName      string                    `json:"runtime_name"`
	ExpectedReplicas int                       `json:"expected_replicas"`
	IntentChangedAt  time.Time                 `json:"intent_changed_at"`
	Policy           ServiceAvailabilityPolicy `json:"policy"`
	ConfirmedAt      time.Time                 `json:"confirmed_at"`
	ObservedFrom     time.Time                 `json:"observed_from"`
	ObservedTo       time.Time                 `json:"observed_to"`
	OnlineReplicas   int                       `json:"online_replicas"`
	Samples          int                       `json:"samples"`
}
