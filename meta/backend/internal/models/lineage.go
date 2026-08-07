package models

import "time"

// LineageItemRelation 是 data item 到 data item 的当前关系投影。
type LineageItemRelation struct {
	ID                    uint       `gorm:"primaryKey" json:"id"`
	TenantID              uint       `gorm:"not null;index" json:"tenant_id"`
	SourceItemID          uint       `gorm:"not null;index" json:"source_item_id"`
	TargetItemID          uint       `gorm:"not null;index" json:"target_item_id"`
	RelationKind          string     `gorm:"size:32;not null" json:"relation_kind"`
	Granularity           string     `gorm:"size:32;not null;default:item" json:"granularity"`
	WriteMode             *string    `gorm:"size:32" json:"write_mode,omitempty"`
	Status                string     `gorm:"size:32;not null;default:active;index" json:"status"`
	FirstObservedAt       time.Time  `json:"first_observed_at"`
	LastObservedAt        time.Time  `json:"last_observed_at"`
	ClosedAt              *time.Time `json:"closed_at,omitempty"`
	ClosedByObservationID *uint      `json:"closed_by_observation_id,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (LineageItemRelation) TableName() string { return "meta.lineage_item_relations" }

// LineageServiceDependency 是 data item 到已发布服务版本的当前依赖投影。
type LineageServiceDependency struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	TenantID          uint       `gorm:"not null;index" json:"tenant_id"`
	SourceItemID      uint       `gorm:"not null;index" json:"source_item_id"`
	ServiceID         uint       `gorm:"not null;index" json:"service_id"`
	PublishedRevision string     `gorm:"size:128;not null" json:"published_revision"`
	DependencyHash    *string    `gorm:"size:128" json:"dependency_hash,omitempty"`
	DependencyKind    string     `gorm:"size:32;not null" json:"dependency_kind"`
	Granularity       string     `gorm:"size:32;not null;default:item" json:"granularity"`
	DependencyFields  JSONMap    `gorm:"type:jsonb" json:"dependency_fields,omitempty"`
	Status            string     `gorm:"size:32;not null;default:active;index" json:"status"`
	FirstObservedAt   time.Time  `json:"first_observed_at"`
	LastObservedAt    time.Time  `json:"last_observed_at"`
	ClosedAt          *time.Time `json:"closed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (LineageServiceDependency) TableName() string { return "meta.lineage_service_dependencies" }

// LineageObservation 是由执行事实或服务发布事实解析出的不可变证据。
type LineageObservation struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	TenantID          uint      `gorm:"not null;index" json:"tenant_id"`
	RelationKind      string    `gorm:"size:32;not null" json:"relation_kind"`
	Granularity       string    `gorm:"size:32;not null;default:item" json:"granularity"`
	SourceItemID      *uint     `gorm:"index" json:"source_item_id,omitempty"`
	TargetItemID      *uint     `gorm:"index" json:"target_item_id,omitempty"`
	ServiceID         *uint     `gorm:"index" json:"service_id,omitempty"`
	PublishedRevision *string   `gorm:"size:128" json:"published_revision,omitempty"`
	ExecutionID       *string   `gorm:"size:64;index" json:"execution_id,omitempty"`
	ProducerModule    string    `gorm:"size:64;not null" json:"producer_module"`
	CaptureMethod     string    `gorm:"size:32;not null" json:"capture_method"`
	SourceSnapshot    JSONMap   `gorm:"type:jsonb;not null" json:"source_snapshot"`
	TargetSnapshot    JSONMap   `gorm:"type:jsonb" json:"target_snapshot,omitempty"`
	Evidence          JSONMap   `gorm:"type:jsonb;not null" json:"evidence"`
	ObservedAt        time.Time `json:"observed_at"`
	CreatedAt         time.Time `json:"created_at"`
}

func (LineageObservation) TableName() string { return "meta.lineage_observations" }
