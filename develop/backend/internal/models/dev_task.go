package models

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// DevTask 是 Develop 私有开发任务定义（SQL 查询、工作流、脚本等）。
type DevTask struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	TenantID    uint   `gorm:"not null;index:idx_dev_tasks_tenant_type" json:"tenant_id"`
	Name        string `gorm:"size:255;not null" json:"name"`
	DisplayName string `gorm:"size:255" json:"display_name,omitempty"`
	DevType     string `gorm:"size:50;not null;index:idx_dev_tasks_tenant_type" json:"dev_type"` // Develop 内部类型：'query' | 'workflow' | 'script'

	// 内容存储（根据类型解析）
	Content DevTaskContent `gorm:"type:jsonb;not null" json:"content"`

	// 执行配置（JSONB 字段，统一的执行配置）
	ExecutionConfig DevTaskContent       `gorm:"type:jsonb;column:execution_config" json:"execution_config,omitempty"`
	EditorLayout    commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"editor_layout"`

	Timeout int `gorm:"default:300" json:"timeout"` // 超时时间（秒）

	// 元数据
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Tags        pq.StringArray `gorm:"type:text[]" json:"tags,omitempty"`
	CreatedBy   *uint          `json:"created_by,omitempty"`
	UpdatedBy   *uint          `json:"updated_by,omitempty"`

	// 审计字段
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	// 状态
	Status              string     `gorm:"size:50;default:'active';index:idx_dev_tasks_status" json:"status"` // 'active' | 'inactive' | 'archived'
	LastExecutionID     *string    `gorm:"size:36" json:"last_execution_id,omitempty"`                        // UUID，软引用 common.task_executions.execution_id
	LastExecutionStatus string     `gorm:"size:50" json:"last_execution_status,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
}

// ProviderDevTask 是 Develop 通过 TaskProvider API 暴露的标准任务定义。
// DevTask 内部使用 dev_type，TaskProvider 契约对外必须使用 task_type。
type ProviderDevTask struct {
	ID                  uint                 `json:"id"`
	TenantID            uint                 `json:"tenant_id"`
	Name                string               `json:"name"`
	DisplayName         string               `json:"display_name,omitempty"`
	TaskType            string               `json:"task_type"`
	Content             DevTaskContent       `json:"content,omitempty"`
	ExecutionConfig     DevTaskContent       `json:"execution_config,omitempty"`
	EditorLayout        commonModels.JSONMap `json:"editor_layout"`
	Parameters          DevTaskContent       `json:"parameters,omitempty"`
	Timeout             int                  `json:"timeout"`
	Description         string               `json:"description,omitempty"`
	Tags                pq.StringArray       `json:"tags,omitempty"`
	CreatedBy           *uint                `json:"created_by,omitempty"`
	UpdatedBy           *uint                `json:"updated_by,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	Status              string               `json:"status"`
	LastExecutionID     *string              `json:"last_execution_id,omitempty"`
	LastExecutionStatus string               `json:"last_execution_status,omitempty"`
	LastRunAt           *time.Time           `json:"last_run_at,omitempty"`
}

// ListProviderDevTasksResponse 是 TaskProvider 标准任务列表响应。
type ListProviderDevTasksResponse struct {
	Items    []ProviderDevTask `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

func NewProviderDevTask(item DevTask) ProviderDevTask {
	return ProviderDevTask{
		ID:                  item.ID,
		TenantID:            item.TenantID,
		Name:                item.Name,
		DisplayName:         item.DisplayName,
		TaskType:            item.DevType,
		Content:             item.Content,
		ExecutionConfig:     item.ExecutionConfig,
		EditorLayout:        item.EditorLayout,
		Parameters:          providerTaskParameters(item),
		Timeout:             item.Timeout,
		Description:         item.Description,
		Tags:                item.Tags,
		CreatedBy:           item.CreatedBy,
		UpdatedBy:           item.UpdatedBy,
		CreatedAt:           item.CreatedAt,
		UpdatedAt:           item.UpdatedAt,
		Status:              item.Status,
		LastExecutionID:     item.LastExecutionID,
		LastExecutionStatus: item.LastExecutionStatus,
		LastRunAt:           item.LastRunAt,
	}
}

func NewProviderDevTasks(items []DevTask) []ProviderDevTask {
	result := make([]ProviderDevTask, 0, len(items))
	for _, item := range items {
		result = append(result, NewProviderDevTask(item))
	}
	return result
}

func providerTaskParameters(item DevTask) DevTaskContent {
	if item.Content == nil {
		return DevTaskContent{}
	}
	inputs, ok := item.Content["inputs"]
	if !ok {
		return DevTaskContent{}
	}
	switch value := inputs.(type) {
	case DevTaskContent:
		return value
	case map[string]interface{}:
		return DevTaskContent(value)
	default:
		return DevTaskContent{}
	}
}

// TableName 指定表名
func (DevTask) TableName() string {
	return "develop.dev_tasks"
}

// GetQueryType 从 content 中获取查询类型
func (d *DevTask) GetQueryType() string {
	if d.Content != nil {
		if qt, ok := d.Content["query_type"].(string); ok {
			return strings.ToLower(strings.TrimSpace(qt))
		}
	}
	return ""
}

// GetQueryMode 从 execution_config 中获取查询执行模式。
func (d *DevTask) GetQueryMode() string {
	if d.ExecutionConfig != nil {
		if mode, ok := d.ExecutionConfig["query_mode"].(string); ok {
			return strings.ToLower(strings.TrimSpace(mode))
		}
	}
	return ""
}

// IsDuckDBQuery 判断查询任务是否使用 Develop 内置 DuckDB 联邦查询模式。
func (d *DevTask) IsDuckDBQuery() bool {
	return d.DevType == "query" && d.GetQueryType() == "sql" && d.GetQueryMode() == "duckdb"
}

// GetEngineID 从 execution_config 中获取引擎 ID
func (d *DevTask) GetEngineID() *uint {
	if d.ExecutionConfig != nil {
		if engineID, ok := d.ExecutionConfig["engine_id"].(float64); ok {
			if engineID <= 0 {
				return nil
			}
			id := uint(engineID)
			return &id
		}
		if engineID, ok := d.ExecutionConfig["engine_id"].(int); ok {
			if engineID <= 0 {
				return nil
			}
			id := uint(engineID)
			return &id
		}
		if engineID, ok := d.ExecutionConfig["engine_id"].(uint); ok {
			if engineID == 0 {
				return nil
			}
			id := engineID
			return &id
		}
	}
	return nil
}

// IsNotebookScript 判断脚本任务是否由 Notebook 文件承载。
func (d *DevTask) IsNotebookScript() bool {
	if d == nil || d.DevType != "script" || d.Content == nil {
		return false
	}
	notebookPath, ok := d.Content["notebook_path"].(string)
	return ok && strings.TrimSpace(notebookPath) != ""
}

// DevTaskContent 开发任务内容（支持任意 JSON 结构）
type DevTaskContent map[string]interface{}

// Value 实现 driver.Valuer 接口
func (c DevTaskContent) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	return json.Marshal(c)
}

// Scan 实现 sql.Scanner 接口
func (c *DevTaskContent) Scan(value interface{}) error {
	if value == nil {
		*c = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

// CreateDevTaskRequest 创建开发任务请求
type CreateDevTaskRequest struct {
	Name            string                 `json:"name" binding:"required"`
	DisplayName     string                 `json:"display_name"`
	DevType         string                 `json:"dev_type" binding:"required,oneof=query workflow script"` // Develop 内部类型；TaskProvider 对外映射为 task_type
	Content         map[string]interface{} `json:"content" binding:"required"`
	ExecutionConfig map[string]interface{} `json:"execution_config"`
	EditorLayout    commonModels.JSONMap   `json:"editor_layout"`
	Timeout         int                    `json:"timeout"`
	Description     string                 `json:"description"`
	Tags            []string               `json:"tags"`
}

// UpdateDevTaskRequest 更新开发任务请求
type UpdateDevTaskRequest struct {
	Name            string                 `json:"name"`
	DisplayName     string                 `json:"display_name"`
	Content         map[string]interface{} `json:"content"`
	ExecutionConfig map[string]interface{} `json:"execution_config"`
	EditorLayout    commonModels.JSONMap   `json:"editor_layout"`
	Timeout         int                    `json:"timeout"`
	Description     string                 `json:"description"`
	Tags            []string               `json:"tags"`
	Status          string                 `json:"status" binding:"omitempty,oneof=active inactive archived"`
}

// NotebookRuntimeBindingRequest 完整替换 Notebook 当前任务的运行时绑定。
type NotebookRuntimeBindingRequest struct {
	EngineID uint   `json:"engine_id" binding:"required"`
	Kernel   string `json:"kernel" binding:"required"`
}

// WorkflowStorageEngineBinding 描述工作流内容中对一个存储 Engine 的 Locator 引用集合。
type WorkflowStorageEngineBinding struct {
	EngineID            uint                            `json:"engine_id"`
	ReferenceCount      int                             `json:"reference_count"`
	ResourceTypes       []string                        `json:"resource_types"`
	Available           bool                            `json:"available"`
	Engine              *WorkflowStorageEngineCandidate `json:"engine,omitempty"`
	CompatibleEngineIDs []uint                          `json:"compatible_engine_ids"`
}

// WorkflowStorageEngineCandidate 是面向用户 API 的最小存储 Engine 摘要。
type WorkflowStorageEngineCandidate struct {
	ID               uint   `json:"id"`
	Name             string `json:"name"`
	EngineType       string `json:"engine_type"`
	LifecycleState   string `json:"lifecycle_state"`
	ConnectionStatus string `json:"connection_status"`
}

// WorkflowStorageEngineBindingsResponse 返回任务当前绑定和可选择的存储 Engine。
type WorkflowStorageEngineBindingsResponse struct {
	Items            []WorkflowStorageEngineBinding   `json:"items"`
	CandidateEngines []WorkflowStorageEngineCandidate `json:"candidate_engines"`
}

// RebindWorkflowStorageEngineRequest 替换一个旧存储 Engine 绑定。
type RebindWorkflowStorageEngineRequest struct {
	TargetEngineID uint `json:"target_engine_id" binding:"required"`
}

// RebindWorkflowStorageEngineResponse 返回更新后的任务和替换数量。
type RebindWorkflowStorageEngineResponse struct {
	Task                 DevTask `json:"task"`
	SourceEngineID       uint    `json:"source_engine_id"`
	TargetEngineID       uint    `json:"target_engine_id"`
	ReplacedLocatorCount int     `json:"replaced_locator_count"`
}

// ListDevTasksRequest 查询开发任务列表请求
type ListDevTasksRequest struct {
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
	DevType  string `form:"dev_type" binding:"omitempty,oneof=query workflow script"`
	Status   string `form:"status" binding:"omitempty,oneof=active inactive archived"`
	Tag      string `form:"tag"`
	Keyword  string `form:"keyword"` // 搜索名称或描述
}

// ListDevTasksResponse 开发任务列表响应
type ListDevTasksResponse struct {
	Items    []DevTask `json:"items"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}
