package execution

import (
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/models"
)

// TaskExecution 统一执行记录
// 用于 Transfer、Develop、Orchestrator、Manager 等模块的执行记录
type TaskExecution struct {
	ID       int64 `gorm:"primaryKey" json:"id"`
	TenantID int   `gorm:"not null;index:idx_task_executions_tenant_status" json:"tenant_id"`

	// 执行标识
	ExecutionID string `gorm:"size:255;uniqueIndex;not null" json:"execution_id"` // UUID 全局唯一

	// 模块标识
	Module   string `gorm:"size:50;not null;index:idx_task_executions_module_type" json:"module"`       // 'transfer'/'develop'/'orchestrator'/'manager'
	TaskType string `gorm:"size:100;not null;index:idx_task_executions_module_type" json:"task_type"`   // 'import'/'export'/'sync'/'query'/'workflow'/'notebook'/'orchestration'/'mvt_generation'/'embedding'
	Source   string `gorm:"size:50;not null;default:'';index:idx_task_executions_source" json:"source"` // 触发来源模块

	// 关联原始任务
	SourceTaskID   *int    `json:"source_task_id,omitempty"`                   // 关联各模块的任务ID
	SourceTaskName *string `gorm:"size:255" json:"source_task_name,omitempty"` // 任务名称（冗余，便于查询）

	// 父执行（Orchestrator 子步骤追踪父编排）
	ParentExecutionID *string `gorm:"size:36" json:"parent_execution_id,omitempty"`

	// 执行状态
	Status      string  `gorm:"size:50;not null;index:idx_task_executions_tenant_status" json:"status"` // 'pending'/'running'/'success'/'failed'/'timeout'/'cancelled'
	Progress    int     `gorm:"default:0" json:"progress"`                                              // 0-100
	CurrentStep *string `gorm:"size:255" json:"current_step,omitempty"`                                 // 当前步骤（Orchestrator/Workflow）

	// 触发信息
	TriggerType string `gorm:"size:50;not null;index:idx_task_executions_trigger_type" json:"trigger_type"` // 'manual'/'scheduled'
	TriggeredBy *int   `json:"triggered_by,omitempty"`                                                      // 触发用户ID

	// JSONB 字段
	ExecutionConfig models.JSONMap `gorm:"type:jsonb" json:"execution_config,omitempty"` // 执行配置
	ErrorDetails    models.JSONMap `gorm:"type:jsonb" json:"error_details,omitempty"`    // 错误详情（仅失败时有值）
	Metadata        models.JSONMap `gorm:"type:jsonb" json:"metadata,omitempty"`         // 模块特有扩展数据（结果、断点、步骤结果等）

	// 性能指标
	ExecutionTimeMs *int64 `json:"execution_time_ms,omitempty"` // 执行时长（毫秒）
	RowsAffected    *int64 `json:"rows_affected,omitempty"`     // SQL 影响行数
	RecordsRead     *int64 `json:"records_read,omitempty"`      // Transfer 读取记录数
	RecordsWritten  *int64 `json:"records_written,omitempty"`   // Transfer 写入记录数
	BytesRead       *int64 `json:"bytes_read,omitempty"`        // Transfer 读取字节数
	BytesWritten    *int64 `json:"bytes_written,omitempty"`     // Transfer 写入字节数

	// 时间戳
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `gorm:"not null;index:idx_task_executions_created_at,sort:desc" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"not null" json:"updated_at"`
}

// TableName 指定表名
func (TaskExecution) TableName() string {
	return "common.task_executions"
}

// 执行状态常量
const (
	ExecutionStatusPending   = "pending"
	ExecutionStatusRunning   = "running"
	ExecutionStatusSuccess   = "success"
	ExecutionStatusFailed    = "failed"
	ExecutionStatusTimeout   = "timeout"
	ExecutionStatusCancelled = "cancelled"
)

// 模块常量
const (
	ModuleConsole      = "console"
	ModuleMeta         = "meta"
	ModuleTransfer     = "transfer"
	ModuleDevelop      = "develop"
	ModuleOrchestrator = "orchestrator"
	ModuleQuality      = "quality"
	ModuleManager      = "manager"
	ModuleGraph        = "graph"
	ModuleSystem       = "system"
	ModuleMonitor      = "monitor"
	ModuleStandard     = "standard"
	ModuleModel        = "model"
	ModuleAsset        = "asset"
	ModulePortal       = "portal"
	ModuleService      = "service"
)

// 触发类型常量
const (
	TriggerTypeManual    = "manual"
	TriggerTypeScheduled = "scheduled"
)

func NormalizeTriggerType(triggerType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(triggerType))
	if normalized == "" {
		return TriggerTypeManual, nil
	}
	switch normalized {
	case TriggerTypeManual, TriggerTypeScheduled:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported trigger_type %q: use manual or scheduled", triggerType)
	}
}

// TaskType 常量（各模块使用）
const (
	// Develop 模块
	TaskTypeQuery    = "query"
	TaskTypeWorkflow = "workflow"
	TaskTypeNotebook = "notebook"
	// Transfer 模块
	TaskTypeImport = "import"
	TaskTypeExport = "export"
	TaskTypeSync   = "sync"
	// Graph 模块
	TaskTypeKGBuild = "kg_build"
	// Quality 模块
	TaskTypeQualityCheck = "check"
)

// CalculateDuration 计算执行时长
func (e *TaskExecution) CalculateDuration() int64 {
	if e.StartedAt == nil {
		return 0
	}
	endTime := time.Now()
	if e.CompletedAt != nil {
		endTime = *e.CompletedAt
	}
	return endTime.Sub(*e.StartedAt).Milliseconds()
}

// IsCompleted 判断是否已完成
func (e *TaskExecution) IsCompleted() bool {
	return e.Status == ExecutionStatusSuccess ||
		e.Status == ExecutionStatusFailed ||
		e.Status == ExecutionStatusTimeout ||
		e.Status == ExecutionStatusCancelled
}

// IsRunning 判断是否正在运行
func (e *TaskExecution) IsRunning() bool {
	return e.Status == ExecutionStatusRunning || e.Status == ExecutionStatusPending
}
