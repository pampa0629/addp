package models

import (
	"github.com/addp/common/runtimelog"
	"time"
)

// ModuleLogSource is a trusted receiver identity, not a registered service instance.
type ModuleLogSource struct {
	InstanceID       string    `gorm:"primaryKey;size:100" json:"instance_id"`
	ModuleName       string    `gorm:"size:100;not null" json:"module_name"`
	Role             string    `gorm:"size:30;not null" json:"role"`
	HostNodeName     string    `gorm:"size:100;not null" json:"host_node_name"`
	CaptureStartedAt time.Time `gorm:"not null" json:"capture_started_at"`
	ObservedAt       time.Time `gorm:"not null" json:"observed_at"`
	ExpiresAt        time.Time `gorm:"not null;index" json:"-"`
}

func (ModuleLogSource) TableName() string { return "module_log_sources" }

type ModuleLogSourceNode struct {
	Node        string                   `gorm:"primaryKey;size:100" json:"node"`
	BootID      string                   `gorm:"size:100;not null" json:"-"`
	Sequence    uint64                   `gorm:"not null" json:"-"`
	SampledAt   time.Time                `gorm:"not null" json:"sampled_at"`
	ReceivedAt  time.Time                `gorm:"not null" json:"received_at"`
	Complete    bool                     `gorm:"not null" json:"complete"`
	ScanIssues  []runtimelog.SourceIssue `gorm:"serializer:json;type:jsonb" json:"scan_issues"`
	PayloadHash string                   `gorm:"size:64;not null" json:"-"`
}

func (ModuleLogSourceNode) TableName() string { return "module_log_source_nodes" }

type ModuleLogSourceBoot struct {
	BootID    string    `gorm:"primaryKey;size:100"`
	ExpiresAt time.Time `gorm:"not null;index"`
}

func (ModuleLogSourceBoot) TableName() string { return "module_log_source_boots" }

type ModuleLogSourceFilter struct {
	Module, Node, Role string
	From, To           time.Time
	Page, PageSize     int
}
type ModuleLogSourcePage struct {
	Data            []ModuleLogSource        `json:"data"`
	Total           int64                    `json:"total"`
	Page            int                      `json:"page"`
	PageSize        int                      `json:"page_size"`
	TotalPages      int                      `json:"total_pages"`
	Observation     *ModuleLogSourceNode     `json:"observation"`
	DiscoveryState  string                   `json:"discovery_state"`
	DiscoveryIssues []runtimelog.SourceIssue `json:"discovery_issues"`
}
