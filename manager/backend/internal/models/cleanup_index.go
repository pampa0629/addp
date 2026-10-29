package models

import "time"

// CleanupIndexOperation is Manager-private control evidence, never a queue.
// An unresolved operation outlives the execution lease and process generation.
type CleanupIndexOperation struct {
	ID              string `gorm:"primaryKey;size:64"`
	IndexName       string `gorm:"not null;size:128;index"`
	TenantID        int64  `gorm:"not null"`
	ExecutionID     string `gorm:"not null;size:64;uniqueIndex"`
	Attempt         int    `gorm:"not null"`
	RequestDigest   string `gorm:"not null;size:64"`
	Epoch           string `gorm:"not null;size:64"`
	EndpointID      string `gorm:"not null;size:64"`
	InventoryDigest string `gorm:"not null;size:64"`
	State           string `gorm:"not null;size:16"`
	CreatedAt       time.Time
	SettledAt       *time.Time
}

func (CleanupIndexOperation) TableName() string { return "manager.cleanup_index_operations" }

// CleanupIndexTarget blocks new registrations for a reclaimed Engine scope.
// Neither an expired lease nor restarting search clears this domain fact.
type CleanupIndexTarget struct {
	IndexName   string `gorm:"primaryKey;size:128"`
	TenantID    int64  `gorm:"primaryKey;autoIncrement:false"`
	EngineID    uint   `gorm:"primaryKey;autoIncrement:false"`
	OperationID string `gorm:"not null;size:64"`
}

func (CleanupIndexTarget) TableName() string { return "manager.cleanup_index_targets" }

// CleanupIndexAssessment freezes Manager-owned inventory for one accepted scan.
// It is private evidence, never returned by the professional statistics endpoint.
type CleanupIndexAssessment struct {
	DomainDigest    string `gorm:"not null;size:64"`
	RequestID       string `gorm:"primaryKey;size:64"`
	TenantID        uint   `gorm:"not null"`
	RequestedBy     uint   `gorm:"not null"`
	ScopeEngineID   uint   `gorm:"not null"`
	IndexName       string `gorm:"not null"`
	Epoch           string `gorm:"not null"`
	EndpointID      string `gorm:"not null"`
	InventoryDigest string `gorm:"not null;size:64"`
	EngineIDs       []uint `gorm:"serializer:json;type:jsonb;not null"`
	CandidateCount  int    `gorm:"not null"`
	CreatedAt       time.Time
}

func (CleanupIndexAssessment) TableName() string { return "manager.cleanup_index_assessments" }
