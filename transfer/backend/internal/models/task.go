package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LocalTime 本地时间类型，序列化为不带时区的本地时间字符串
type LocalTime struct {
	time.Time
}

// MarshalJSON 自定义 JSON 序列化，返回本地时间格式
func (t LocalTime) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte("null"), nil
	}
	// 格式化为本地时间字符串（不带时区标识）
	formatted := t.Time.Format(`"2006-01-02T15:04:05"`)
	return []byte(formatted), nil
}

// UnmarshalJSON 自定义 JSON 反序列化
func (t *LocalTime) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	str := string(data)
	if len(str) > 2 {
		str = str[1 : len(str)-1] // 去掉引号
	}
	parsed, err := time.Parse("2006-01-02T15:04:05", str)
	if err != nil {
		// 尝试其他格式
		parsed, err = time.Parse(time.RFC3339, str)
		if err != nil {
			return err
		}
	}
	t.Time = parsed
	return nil
}

// Scan 实现 sql.Scanner 接口，允许从数据库读取时间
func (t *LocalTime) Scan(value interface{}) error {
	if value == nil {
		t.Time = time.Time{}
		return nil
	}
	if v, ok := value.(time.Time); ok {
		t.Time = v
		return nil
	}
	return nil
}

// Value 实现 driver.Valuer 接口，允许写入数据库
func (t LocalTime) Value() (driver.Value, error) {
	if t.Time.IsZero() {
		return nil, nil
	}
	return t.Time, nil
}

// TaskStatus 任务状态（简化版）
type TaskStatus string

const (
	TaskStatusIdle    TaskStatus = "idle"    // 空闲（未执行或执行完成）
	TaskStatusRunning TaskStatus = "running" // 执行中
	TaskStatusBlocked TaskStatus = "blocked" // 运行被 schema change 阻塞，禁止直接启动或恢复
)

// TaskDesiredState 是 continuous runtime 的用户期望状态。
// bounded worker 不消费该字段；所有新任务都以 stopped 创建。
type TaskDesiredState string

type InitialMetadataScanStatus string

const (
	TaskDesiredStateRunning TaskDesiredState = "running"
	TaskDesiredStatePaused  TaskDesiredState = "paused"
	TaskDesiredStateStopped TaskDesiredState = "stopped"

	InitialMetadataScanRunning InitialMetadataScanStatus = "running"
	InitialMetadataScanSuccess InitialMetadataScanStatus = "success"
	InitialMetadataScanFailed  InitialMetadataScanStatus = "failed"
)

// JSONMap is now imported from common/models
// Use commonModels.JSONMap instead
type JSONMap = commonModels.JSONMap

// TransferTask 传输任务定义
type TransferTask struct {
	ID                             uint                           `gorm:"primaryKey" json:"id"`
	ExecutionContract              taskprovider.ExecutionContract `gorm:"-" json:"execution_contract"`
	ApplyIdentity                  string                         `gorm:"type:uuid;not null;uniqueIndex" json:"-"`
	TenantID                       uint                           `gorm:"not null;index" json:"tenant_id"`
	Name                           string                         `gorm:"type:varchar(255);not null" json:"name"`
	Description                    string                         `gorm:"type:text" json:"description"`
	TaskType                       string                         `gorm:"type:varchar(20);not null;default:'sync';index" json:"task_type"` // Transfer 当前统一任务类型，固定为 sync
	Config                         JSONMap                        `gorm:"type:jsonb;not null" json:"config"`                               // Reader-Transform-Writer 管道配置
	Schedule                       string                         `gorm:"type:varchar(100)" json:"schedule"`                               // Cron 表达式
	BatchSize                      int                            `gorm:"default:1000" json:"batch_size"`
	Enabled                        bool                           `gorm:"default:false;index" json:"enabled"`
	AutoScanMetadata               bool                           `json:"auto_scan_metadata"`
	InitialMetadataScanStatus      InitialMetadataScanStatus      `gorm:"type:varchar(20);not null;default:'';check:chk_transfer_initial_meta_scan_status,initial_metadata_scan_status IN ('','running','success','failed')" json:"-"`
	InitialMetadataScanClaimToken  string                         `gorm:"type:varchar(36);not null;default:'';check:chk_transfer_initial_meta_scan_claim,(initial_metadata_scan_status = 'running' AND initial_metadata_scan_claim_token <> '' AND initial_metadata_scan_lease_until IS NOT NULL) OR (initial_metadata_scan_status <> 'running' AND initial_metadata_scan_claim_token = '' AND initial_metadata_scan_lease_until IS NULL)" json:"-"`
	InitialMetadataScanLeaseUntil  *time.Time                     `json:"-"`
	InitialMetadataScanAttempt     uint64                         `gorm:"not null;default:0;check:chk_transfer_initial_meta_scan_attempt,initial_metadata_scan_attempt >= 0" json:"-"`
	InitialMetadataScanExecutionID string                         `gorm:"type:varchar(36);not null;default:''" json:"-"`
	InitialMetadataScanError       string                         `gorm:"type:text;not null;default:''" json:"-"`
	Status                         TaskStatus                     `gorm:"type:varchar(20);default:'idle';index" json:"status"`
	DesiredState                   TaskDesiredState               `gorm:"type:varchar(20);not null;default:'stopped';index" json:"desired_state"`
	Progress                       float64                        `gorm:"type:numeric(5,2);default:0" json:"progress"`
	CreatedBy                      *uint                          `json:"created_by,omitempty"`
	// BaseTask 基类字段
	LastExecutionID     *string         `gorm:"size:36" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string         `gorm:"size:20" json:"last_execution_status,omitempty"`
	LastRunAt           *time.Time      `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time      `json:"next_run_at,omitempty"`
	CreatedAt           time.Time       `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time       `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt           gorm.DeletedAt  `gorm:"index" json:"deleted_at,omitempty"`
	Capture             *CaptureSummary `gorm:"-" json:"capture,omitempty"`
}

// SyncState is the single committed-position fact for one Transfer task source partition.
type SyncState struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	TaskID              uint       `gorm:"not null;uniqueIndex:idx_transfer_sync_state_identity" json:"task_id"`
	SourceIdentity      string     `gorm:"type:text;not null;uniqueIndex:idx_transfer_sync_state_identity" json:"source_identity"`
	Partition           string     `gorm:"type:varchar(255);not null;default:'default';uniqueIndex:idx_transfer_sync_state_identity" json:"partition"`
	Position            JSONMap    `gorm:"type:jsonb" json:"position,omitempty"`
	PositionType        string     `gorm:"type:varchar(50);not null;default:'watermark'" json:"position_type"`
	PositionVersion     string     `gorm:"type:varchar(20);not null;default:'v1'" json:"position_version"`
	StateVersion        uint64     `gorm:"not null;default:0" json:"state_version"`
	FencingToken        uint64     `gorm:"not null;default:0" json:"fencing_token"`
	UpdatedExecutionID  string     `gorm:"type:varchar(36)" json:"updated_execution_id,omitempty"`
	PositionCommittedAt *time.Time `json:"position_committed_at,omitempty"`
	CreatedAt           time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SyncState) TableName() string { return "transfer.sync_states" }

// RuntimeLease 是 continuous execution 在 Infra PostgreSQL 中的唯一运行所有权事实。
type RuntimeLease struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	TaskID          uint      `gorm:"not null;unique" json:"task_id"`
	ExecutionID     string    `gorm:"type:varchar(255);not null;unique" json:"execution_id"`
	OwnerInstanceID string    `gorm:"type:varchar(255);not null;index:idx_transfer_runtime_leases_owner" json:"owner_instance_id"`
	LeaseUntil      time.Time `gorm:"not null;index:idx_transfer_runtime_leases_lease_until" json:"lease_until"`
	HeartbeatAt     time.Time `gorm:"not null" json:"heartbeat_at"`
	FencingToken    uint64    `gorm:"not null" json:"fencing_token"`
	ClaimedAt       time.Time `gorm:"not null" json:"claimed_at"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (RuntimeLease) TableName() string { return "transfer.runtime_leases" }

type CaptureStatus string

type CaptureSourceType string

const (
	CaptureStatusProvisioning  CaptureStatus = "provisioning"
	CaptureStatusRunning       CaptureStatus = "running"
	CaptureStatusFailed        CaptureStatus = "failed"
	CaptureStatusCleaning      CaptureStatus = "cleaning"
	CaptureStatusCleanupFailed CaptureStatus = "cleanup_failed"
	CaptureStatusStopped       CaptureStatus = "stopped"

	CaptureSourcePostgreSQL CaptureSourceType = "postgresql"
	CaptureSourceMySQL      CaptureSourceType = "mysql"
	CaptureSourceOracle     CaptureSourceType = "oracle"
)

// CaptureResource 是数据库 CDC task generation 的 engine-neutral 资源登记事实。
type CaptureResource struct {
	ID                          uint                       `gorm:"primaryKey" json:"id"`
	TaskID                      uint                       `gorm:"not null;uniqueIndex:uq_transfer_capture_generation" json:"task_id"`
	TenantID                    uint                       `gorm:"not null;index" json:"tenant_id"`
	Generation                  uint64                     `gorm:"not null;uniqueIndex:uq_transfer_capture_generation" json:"generation"`
	ConnectorName               string                     `gorm:"type:varchar(255);not null;uniqueIndex:uq_transfer_capture_connector" json:"connector_name"`
	TopicName                   string                     `gorm:"type:varchar(255);not null;uniqueIndex:uq_transfer_capture_topic" json:"topic_name"`
	ConsumerGroup               string                     `gorm:"type:varchar(255);not null;uniqueIndex:uq_transfer_capture_group" json:"consumer_group"`
	SourceType                  CaptureSourceType          `gorm:"type:varchar(32);not null;index:idx_transfer_capture_resources_source_type;check:chk_transfer_capture_source_type,source_type IN ('postgresql','mysql','oracle')" json:"source_type"`
	SourceIdentity              string                     `gorm:"type:text;not null" json:"source_identity"`
	SourceConnectionFingerprint string                     `gorm:"type:varchar(64);not null" json:"source_connection_fingerprint"`
	SourceEngineID              uint                       `gorm:"not null" json:"source_engine_id"`
	SourceDatabase              string                     `gorm:"type:varchar(255);not null" json:"source_database"`
	SourceSchema                string                     `gorm:"type:varchar(255);not null" json:"source_schema"`
	SourceTable                 string                     `gorm:"type:varchar(255);not null" json:"source_table"`
	SourceSpatialInfo           JSONMap                    `gorm:"type:jsonb;not null;default:'{}'" json:"source_spatial_info,omitempty"`
	Status                      CaptureStatus              `gorm:"type:varchar(32);not null;index" json:"status"`
	ConnectorStatus             string                     `gorm:"type:varchar(32)" json:"connector_status,omitempty"`
	ConnectorError              string                     `gorm:"type:text" json:"connector_error,omitempty"`
	SourceStatus                string                     `gorm:"type:varchar(32)" json:"source_status,omitempty"`
	SourceError                 string                     `gorm:"type:text" json:"source_error,omitempty"`
	SourceRecovery              JSONMap                    `gorm:"type:jsonb;not null;default:'{}'" json:"source_recovery,omitempty"`
	SourceRecoveryError         string                     `gorm:"type:text" json:"-"`
	SourceTransactions          JSONMap                    `gorm:"type:jsonb;not null;default:'{}'" json:"source_transactions,omitempty"`
	SourceTransactionsError     string                     `gorm:"type:text" json:"-"`
	TopicCreated                bool                       `gorm:"not null;default:false" json:"topic_created"`
	ConnectorCreated            bool                       `gorm:"not null;default:false" json:"connector_created"`
	ResourceVersion             uint64                     `gorm:"not null;default:1" json:"resource_version"`
	SchemaRevision              uint64                     `gorm:"not null;default:1;check:chk_transfer_capture_schema_revision,schema_revision >= 1" json:"schema_revision"`
	LastObservedAt              *time.Time                 `json:"last_observed_at,omitempty"`
	StoppedAt                   *time.Time                 `json:"stopped_at,omitempty"`
	CreatedAt                   time.Time                  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt                   time.Time                  `gorm:"autoUpdateTime" json:"updated_at"`
	PostgreSQL                  *PostgreSQLCaptureResource `gorm:"foreignKey:CaptureResourceID;constraint:OnDelete:CASCADE" json:"-"`
	MySQL                       *MySQLCaptureResource      `gorm:"foreignKey:CaptureResourceID;constraint:OnDelete:CASCADE" json:"-"`
	Oracle                      *OracleCaptureResource     `gorm:"foreignKey:CaptureResourceID;constraint:OnDelete:CASCADE" json:"-"`
}

func (CaptureResource) TableName() string { return "transfer.capture_resources" }

// PostgreSQLCaptureResource 保存 PostgreSQL capture generation 独有的 logical replication 资源。
type PostgreSQLCaptureResource struct {
	CaptureResourceID uint   `gorm:"primaryKey" json:"capture_resource_id"`
	SlotName          string `gorm:"type:varchar(63);not null;uniqueIndex:uq_transfer_postgresql_capture_slot" json:"slot_name"`
	PublicationName   string `gorm:"type:varchar(63);not null;uniqueIndex:uq_transfer_postgresql_capture_publication" json:"publication_name"`
	SlotOwned         bool   `gorm:"not null;default:true" json:"slot_owned"`
	PublicationOwned  bool   `gorm:"not null;default:true" json:"publication_owned"`
}

func (PostgreSQLCaptureResource) TableName() string {
	return "transfer.postgresql_capture_resources"
}

// MySQLCaptureResource 保存 MySQL capture generation 独有且必须全局唯一的 connector server id。
type MySQLCaptureResource struct {
	CaptureResourceID       uint   `gorm:"primaryKey" json:"capture_resource_id"`
	ConnectorServerID       uint32 `gorm:"not null;uniqueIndex:uq_transfer_mysql_capture_server_id;check:chk_transfer_mysql_capture_server_id,connector_server_id >= 1 AND connector_server_id <= 4294967295" json:"connector_server_id"`
	SchemaHistoryTopicName  string `gorm:"type:varchar(255);not null;uniqueIndex:uq_transfer_mysql_capture_schema_history_topic" json:"schema_history_topic_name"`
	SchemaHistoryTopicOwned bool   `gorm:"not null;default:true" json:"schema_history_topic_owned"`
}

func (MySQLCaptureResource) TableName() string { return "transfer.mysql_capture_resources" }

// OracleCaptureResource 保存 Oracle capture generation 独有的 schema history topic 与 Spatial 捕获对象。
// 表级 ALL COLUMN LOGGING 是共享 source readiness，不属于 task generation，Stop 时不得删除；
// Spatial mirror、row trigger 和 DDL guard 是 generation-owned 资源，Stop 时必须核对身份后删除。
type OracleCaptureResource struct {
	CaptureResourceID       uint   `gorm:"primaryKey" json:"capture_resource_id"`
	SchemaHistoryTopicName  string `gorm:"type:varchar(255);not null;uniqueIndex:uq_transfer_oracle_capture_schema_history_topic" json:"schema_history_topic_name"`
	SchemaHistoryTopicOwned bool   `gorm:"not null;default:true" json:"schema_history_topic_owned"`
	SpatialMirrorTableName  string `gorm:"type:varchar(30);not null;default:''" json:"spatial_mirror_table_name,omitempty"`
	SpatialRowTriggerName   string `gorm:"type:varchar(30);not null;default:''" json:"spatial_row_trigger_name,omitempty"`
	SpatialDDLGuardName     string `gorm:"type:varchar(30);not null;default:''" json:"spatial_ddl_guard_name,omitempty"`
	SpatialArtifactsOwned   bool   `gorm:"not null;default:false" json:"spatial_artifacts_owned"`
}

func (OracleCaptureResource) TableName() string { return "transfer.oracle_capture_resources" }

type SchemaChangeRequestStatus string

const (
	SchemaChangeRequestPending SchemaChangeRequestStatus = "pending"
	SchemaChangeRequestApplied SchemaChangeRequestStatus = "applied"
)

type SchemaChangeMetadataScanStatus string

const (
	SchemaChangeMetadataScanPending SchemaChangeMetadataScanStatus = "pending"
	SchemaChangeMetadataScanRunning SchemaChangeMetadataScanStatus = "running"
	SchemaChangeMetadataScanSuccess SchemaChangeMetadataScanStatus = "success"
	SchemaChangeMetadataScanFailed  SchemaChangeMetadataScanStatus = "failed"
)

// SchemaChangeRequest 是一个 capture generation 内人工 additive mapping revision 的私有控制事实。
type SchemaChangeRequest struct {
	ID                      uint                           `gorm:"primaryKey" json:"id"`
	TaskID                  uint                           `gorm:"not null;index" json:"task_id"`
	TenantID                uint                           `gorm:"not null;index" json:"tenant_id"`
	CaptureResourceID       uint                           `gorm:"not null;index;uniqueIndex:uq_transfer_schema_change_pending_generation,where:status = 'pending';uniqueIndex:uq_transfer_schema_change_revision" json:"-"`
	CaptureResource         CaptureResource                `gorm:"foreignKey:CaptureResourceID;references:ID;constraint:OnDelete:CASCADE" json:"-"`
	Generation              uint64                         `gorm:"not null" json:"generation"`
	ExecutionID             string                         `gorm:"type:varchar(36);not null" json:"execution_id"`
	SourcePartition         string                         `gorm:"type:varchar(255);not null" json:"source_partition"`
	SourceOffset            int64                          `gorm:"not null;check:chk_transfer_schema_change_source_offset,source_offset >= 0" json:"source_offset"`
	Scope                   string                         `gorm:"type:varchar(255);not null" json:"scope"`
	Diff                    JSONMap                        `gorm:"type:jsonb;not null" json:"diff"`
	ApprovedMappings        JSONMap                        `gorm:"type:jsonb;not null;default:'{}'" json:"approved_mappings,omitempty"`
	FromRevision            uint64                         `gorm:"not null;check:chk_transfer_schema_change_from_revision,from_revision >= 1" json:"from_revision"`
	ToRevision              uint64                         `gorm:"not null;uniqueIndex:uq_transfer_schema_change_revision;check:chk_transfer_schema_change_to_revision,to_revision = from_revision + 1" json:"to_revision"`
	Status                  SchemaChangeRequestStatus      `gorm:"type:varchar(20);not null;index;check:chk_transfer_schema_change_status,status IN ('pending','applied')" json:"status"`
	AppliedBy               *uint                          `json:"applied_by,omitempty"`
	DetectedAt              time.Time                      `gorm:"not null" json:"detected_at"`
	AppliedAt               *time.Time                     `json:"applied_at,omitempty"`
	MetadataScanStatus      SchemaChangeMetadataScanStatus `gorm:"type:varchar(20);not null;default:'';check:chk_transfer_schema_change_meta_scan_status,metadata_scan_status IN ('','pending','running','success','failed')" json:"metadata_scan_status,omitempty"`
	MetadataScanClaimToken  string                         `gorm:"type:varchar(36);not null;default:'';check:chk_transfer_schema_change_meta_scan_claim,(metadata_scan_status = 'running' AND metadata_scan_claim_token <> '' AND metadata_scan_lease_until IS NOT NULL) OR (metadata_scan_status <> 'running' AND metadata_scan_claim_token = '' AND metadata_scan_lease_until IS NULL)" json:"-"`
	MetadataScanLeaseUntil  *time.Time                     `json:"-"`
	MetadataScanAttempt     uint64                         `gorm:"not null;default:0;check:chk_transfer_schema_change_meta_scan_attempt,metadata_scan_attempt >= 0" json:"metadata_scan_attempt,omitempty"`
	MetadataScanExecutionID string                         `gorm:"type:varchar(36);not null;default:''" json:"metadata_scan_execution_id,omitempty"`
	MetadataScanError       string                         `gorm:"type:text;not null;default:''" json:"-"`
	CreatedAt               time.Time                      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt               time.Time                      `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SchemaChangeRequest) TableName() string { return "transfer.schema_change_requests" }

type SchemaChangeField struct {
	Source     string `json:"source" binding:"required"`
	Target     string `json:"target" binding:"required"`
	TargetType string `json:"target_type" binding:"required"`
	Nullable   bool   `json:"nullable"`
}

type ApproveSchemaChangeRequest struct {
	Fields []SchemaChangeField `json:"fields" binding:"required,min=1,dive"`
}

type SchemaChangeRequestView struct {
	ID                      uint                           `json:"id"`
	TaskID                  uint                           `json:"task_id"`
	Generation              uint64                         `json:"generation"`
	ExecutionID             string                         `json:"execution_id"`
	SourcePartition         string                         `json:"source_partition"`
	SourceOffset            int64                          `json:"source_offset"`
	Scope                   string                         `json:"scope"`
	Diff                    JSONMap                        `json:"diff"`
	SuggestedFields         []SchemaChangeField            `json:"suggested_fields,omitempty"`
	ApprovedMappings        []SchemaChangeField            `json:"approved_mappings,omitempty"`
	Approvable              bool                           `json:"approvable"`
	ApprovalBlockedReason   string                         `json:"approval_blocked_reason,omitempty"`
	FromRevision            uint64                         `json:"from_revision"`
	ToRevision              uint64                         `json:"to_revision"`
	Status                  SchemaChangeRequestStatus      `json:"status"`
	DetectedAt              time.Time                      `json:"detected_at"`
	AppliedAt               *time.Time                     `json:"applied_at,omitempty"`
	MetadataScanStatus      SchemaChangeMetadataScanStatus `json:"metadata_scan_status,omitempty"`
	MetadataScanAttempt     uint64                         `json:"metadata_scan_attempt,omitempty"`
	MetadataScanLeaseUntil  *time.Time                     `json:"metadata_scan_lease_until,omitempty"`
	MetadataScanExecutionID string                         `json:"metadata_scan_execution_id,omitempty"`
}

// CaptureSummary 是任务 API 可展示的捕获状态；内部资源名称和连接身份不对外暴露。
type CaptureSummary struct {
	Generation         uint64                     `json:"generation"`
	Status             CaptureStatus              `json:"status"`
	ConnectorStatus    string                     `json:"connector_status,omitempty"`
	SourceStatus       string                     `json:"source_status,omitempty"`
	SourceRecovery     *CaptureSourceRecovery     `json:"source_recovery,omitempty"`
	SourceTransactions *CaptureSourceTransactions `json:"source_transactions,omitempty"`
	LastObservedAt     *time.Time                 `json:"last_observed_at,omitempty"`
	StoppedAt          *time.Time                 `json:"stopped_at,omitempty"`
}

type CaptureSourceRecovery struct {
	SchemaVersion             string     `json:"schema_version"`
	Provider                  string     `json:"provider"`
	Health                    string     `json:"health"`
	CapturePosition           string     `json:"capture_position,omitempty"`
	CurrentPosition           string     `json:"current_position,omitempty"`
	EarliestAvailablePosition string     `json:"earliest_available_position,omitempty"`
	PositionHeadroom          string     `json:"position_headroom,omitempty"`
	EarliestAvailableAt       *time.Time `json:"earliest_available_at,omitempty"`
	WindowSeconds             *int64     `json:"window_seconds,omitempty"`
	FRAUsedPercent            *float64   `json:"fra_used_percent,omitempty"`
	FRAReclaimablePercent     *float64   `json:"fra_reclaimable_percent,omitempty"`
	SampledAt                 time.Time  `json:"sampled_at"`
}

type CaptureSourceTransactions struct {
	SchemaVersion         string    `json:"schema_version"`
	Provider              string    `json:"provider"`
	Status                string    `json:"status"`
	ActiveCount           uint64    `json:"active_count"`
	OldestStartPosition   string    `json:"oldest_start_position,omitempty"`
	OldestDurationSeconds *int64    `json:"oldest_duration_seconds,omitempty"`
	UsedUndoBlocks        string    `json:"used_undo_blocks,omitempty"`
	UsedUndoRecords       string    `json:"used_undo_records,omitempty"`
	SampledAt             time.Time `json:"sampled_at"`
}

func NewCaptureSummary(resource *CaptureResource) *CaptureSummary {
	if resource == nil {
		return nil
	}
	return &CaptureSummary{
		Generation: resource.Generation, Status: resource.Status,
		ConnectorStatus:    resource.ConnectorStatus,
		SourceStatus:       resource.SourceStatus,
		SourceRecovery:     captureSourceRecoverySummary(resource.SourceRecovery),
		SourceTransactions: captureSourceTransactionsSummary(resource.SourceTransactions),
		LastObservedAt:     resource.LastObservedAt, StoppedAt: resource.StoppedAt,
	}
}

func captureSourceTransactionsSummary(value JSONMap) *CaptureSourceTransactions {
	if len(value) == 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var summary CaptureSourceTransactions
	if err := json.Unmarshal(data, &summary); err != nil || summary.SchemaVersion == "" || summary.Status == "" {
		return nil
	}
	return &summary
}

func captureSourceRecoverySummary(value JSONMap) *CaptureSourceRecovery {
	if len(value) == 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var summary CaptureSourceRecovery
	if err := json.Unmarshal(data, &summary); err != nil || summary.SchemaVersion == "" || summary.Health == "" {
		return nil
	}
	return &summary
}

// TableName 指定表名（带 schema 前缀）
func (TransferTask) TableName() string {
	return "transfer.transfer_tasks"
}

func (task *TransferTask) BeforeCreate(_ *gorm.DB) error {
	if task.ApplyIdentity == "" {
		task.ApplyIdentity = uuid.NewString()
	}
	return nil
}

// ExecutionStatus 执行状态
type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"   // 待执行
	ExecutionStatusRunning   ExecutionStatus = "running"   // 运行中
	ExecutionStatusSuccess   ExecutionStatus = "success"   // 成功
	ExecutionStatusFailed    ExecutionStatus = "failed"    // 失败
	ExecutionStatusCancelled ExecutionStatus = "cancelled" // 已取消
)

// TaskExecution Transfer 执行记录 DTO（仅用于 API 响应，数据来自 common.task_executions）
type TaskExecution struct {
	ID               uint            `json:"id"`
	ExecutionID      string          `json:"execution_id"`
	TaskID           uint            `json:"task_id"`
	Status           ExecutionStatus `json:"status"`
	StartTime        LocalTime       `json:"start_time"`
	EndTime          *LocalTime      `json:"end_time,omitempty"`
	RecordsRead      int64           `json:"records_read"`
	RecordsWritten   int64           `json:"records_written"`
	BytesRead        int64           `json:"bytes_read"`
	BytesWritten     int64           `json:"bytes_written"`
	ErrorMsg         string          `json:"error_msg,omitempty"`
	Logs             string          `json:"logs,omitempty"`
	CheckpointOffset int64           `json:"checkpoint_offset"`
	CheckpointState  JSONMap         `json:"checkpoint_state,omitempty"`
	Metadata         JSONMap         `json:"metadata,omitempty"`
	TriggerType      string          `json:"trigger_type"`
	TriggerBy        *uint           `json:"trigger_by,omitempty"`
}

// Duration 返回执行时长
func (e *TaskExecution) Duration() time.Duration {
	if e.EndTime == nil {
		return time.Since(e.StartTime.Time)
	}
	return e.EndTime.Time.Sub(e.StartTime.Time)
}

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	Name             string                 `json:"name" binding:"required"`
	Description      string                 `json:"description"`
	TaskType         string                 `json:"task_type"`                 // Transfer 当前统一任务类型，固定为 sync
	Config           map[string]interface{} `json:"config" binding:"required"` // 包含 source locator 与 target parent_locator/name endpoint 配置；source locator item_id 可引用 Meta item
	Schedule         string                 `json:"schedule"`
	Enabled          *bool                  `json:"enabled"`
	BatchSize        int                    `json:"batch_size"`
	AutoScanMetadata *bool                  `json:"auto_scan_metadata"`
}

// UpdateTaskRequest 更新任务请求
type UpdateTaskRequest struct {
	Name             *string                `json:"name"`
	Description      *string                `json:"description"`
	TaskType         *string                `json:"task_type"`
	Config           map[string]interface{} `json:"config"`
	Schedule         *string                `json:"schedule"`
	BatchSize        *int                   `json:"batch_size"`
	Enabled          *bool                  `json:"enabled"`
	AutoScanMetadata *bool                  `json:"auto_scan_metadata"`
}

// StopTaskRequest 仅 PostgreSQL CDC stop 强制要求不可逆确认；普通业务 Kafka continuous 可提交空 body。
type StopTaskRequest struct {
	Confirmed        bool   `json:"confirmed"`
	ConfirmationText string `json:"confirmation_text"`
}

// ReplayTaskRequest 创建业务 Kafka bounded replay execution。
// 除 ranges 与新目标位置外不接受任何任务配置覆盖。
type ReplayTaskRequest struct {
	Ranges []ReplayOffsetRangeRequest `json:"ranges"`
	Target ReplayTargetRequest        `json:"target"`
}

type ReplayOffsetRangeRequest struct {
	Partition   string `json:"partition" example:"0"`
	StartOffset int64  `json:"start_offset" example:"10"`
	EndOffset   int64  `json:"end_offset" example:"20"`
}

type ReplayTargetRequest struct {
	ParentLocator string `json:"parent_locator" example:"addp://engine/8/path/replay_schema?type=schema&node_id=12"`
	Name          string `json:"name" example:"orders_replay"`
}

// ListTasksRequest 查询任务列表请求
type ListTasksRequest struct {
	Status          *TaskStatus `form:"status"`
	TaskType        string      `form:"task_type"`
	RuntimeBoundary string      `form:"runtime_boundary"`
	Page            int         `form:"page" binding:"min=1"`
	PageSize        int         `form:"page_size" binding:"min=1,max=100"`
}

// ListProviderTasksResponse 是 TaskProvider 标准任务列表响应。
type ListProviderTasksResponse struct {
	Items    []TransferTask `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// TaskStatistics 任务统计
type TaskStatistics struct {
	TotalTasks       int64 `json:"total_tasks"`
	PendingTasks     int64 `json:"pending_tasks"`
	RunningTasks     int64 `json:"running_tasks"`
	SuccessTasks     int64 `json:"success_tasks"`
	FailedTasks      int64 `json:"failed_tasks"`
	NotExecutedTasks int64 `json:"not_executed_tasks"`
	LastRunningTasks int64 `json:"last_running_tasks"`
	LastSuccessTasks int64 `json:"last_success_tasks"`
	LastFailedTasks  int64 `json:"last_failed_tasks"`
	TotalExecutions  int64 `json:"total_executions"`
	TotalRecords     int64 `json:"total_records"`
	TotalBytes       int64 `json:"total_bytes"`
}
