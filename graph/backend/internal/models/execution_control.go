package models

import (
	"github.com/addp/common/engine/plugin"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TenantAdmissionLock is Graph's short-lived transaction lock, not a queue.
// The durable closed scope is the individual KnowledgeGraph.
type TenantAdmissionLock struct {
	TenantID uint `gorm:"primaryKey;autoIncrement:false"`
}

type GraphAdmissionView struct {
	Closed                  bool `json:"closed"`
	HasUnresolvedOperations bool `json:"has_unresolved_operations"`
}

// Snapshot and target descriptors retain exact JSON bytes in TEXT, because
// PostgreSQL jsonb normalization would invalidate their byte digests.
// BuildExecutionControl contains only owner-private control facts. Scheduling
// and attempt ownership remain exclusively in common.task_executions.
type BuildExecutionControl struct {
	ExecutionID       string `gorm:"primaryKey;size:64"`
	TenantID          uint   `gorm:"not null;index"`
	GraphID           uint   `gorm:"not null;index"`
	TaskID            uint   `gorm:"not null;index"`
	CancelReason      string `json:"-"`
	CancelRequested   bool   `gorm:"not null;default:false"`
	CancelRequestedBy uint
	CancelRequestedAt *time.Time
	Snapshot          string `gorm:"type:text;not null"`
}

// GraphOperation records an external-call boundary before dispatch. A missing
// response is uncertain; time, process exit, or lease expiry never clears it.
type GraphOperation struct {
	RunnerID           string `gorm:"not null;size:128"`
	LeaseTokenDigest   string `gorm:"size:64"`
	ID                 string `gorm:"primaryKey;size:64"`
	TenantID           uint   `gorm:"not null;index:idx_graph_operation_scope"`
	GraphID            uint   `gorm:"not null;index:idx_graph_operation_scope"`
	ExecutionID        string `gorm:"index;size:64"`
	Attempt            int
	ReviewItemID       uint   `gorm:"index"`
	CommandID          string `gorm:"size:64;index"`
	Kind               string `gorm:"not null"`
	TargetDigest       string `gorm:"not null;size:64"`
	TargetDescriptor   string `gorm:"type:text;not null"`
	RequestDigest      string `gorm:"not null;size:64"`
	State              string `gorm:"not null;index"`
	CreatedAt          time.Time
	SettledAt          *time.Time
	EvidenceDigest     string `gorm:"size:64"`
	LateEvidenceDigest string `gorm:"size:64"`
}

const (
	GraphOperationInFlight  = "in_flight"
	GraphOperationKnown     = "known"
	GraphOperationUncertain = "uncertain"
)

func (g *KnowledgeGraph) AfterCreate(tx *gorm.DB) error {
	return tx.Session(&gorm.Session{NewDB: true}).Clauses(clause.OnConflict{DoNothing: true}).Create(&TenantAdmissionLock{TenantID: g.TenantID}).Error
}

// GraphOperationTarget identifies the exact configured target observed for one
// external operation. It is not proof of a native database's physical identity.
type GraphOperationTarget struct {
	EngineID        uint                     `json:"engine_id"`
	EngineVersion   int64                    `json:"engine_version"`
	CatalogPath     plugin.EngineCatalogPath `json:"catalog_path"`
	IndexManagement string                   `json:"index_management"`
}
