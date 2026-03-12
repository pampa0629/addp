package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

// DevTask 统一开发任务定义（SQL查询、工作流、脚本等）
type DevTask struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TenantID    uint           `gorm:"not null;index:idx_dev_tasks_tenant_type" json:"tenant_id"`
	Name        string         `gorm:"size:255;not null" json:"name"`
	DisplayName string         `gorm:"size:255" json:"display_name,omitempty"`
	DevType     string         `gorm:"size:50;not null;index:idx_dev_tasks_tenant_type" json:"dev_type"` // 'query' | 'workflow' | 'notebook'

	// 内容存储（根据类型解析）
	Content DevTaskContent `gorm:"type:jsonb;not null" json:"content"`

	// 执行配置（JSONB 字段，统一的执行配置）
	ExecutionConfig DevTaskContent `gorm:"type:jsonb;column:execution_config" json:"execution_config,omitempty"`

	Schedule string `gorm:"size:100" json:"schedule,omitempty"` // Cron 表达式
	Enabled  bool   `gorm:"default:false" json:"enabled"`       // 调度开关
	Timeout  int    `gorm:"default:300" json:"timeout"`         // 超时时间（秒）

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
	LastExecutionID     *string    `gorm:"size:36" json:"last_execution_id,omitempty"` // UUID，软引用 common.task_executions.execution_id
	LastExecutionStatus string     `gorm:"size:50" json:"last_execution_status,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
}

// TableName 指定表名
func (DevTask) TableName() string {
	return "develop.dev_tasks"
}

// GetQueryType 从 content 中获取查询类型
func (d *DevTask) GetQueryType() string {
	if d.Content != nil {
		if qt, ok := d.Content["query_type"].(string); ok {
			return qt
		}
	}
	return ""
}

// GetEngineID 从 execution_config 中获取引擎 ID
func (d *DevTask) GetEngineID() *uint {
	if d.ExecutionConfig != nil {
		if engineID, ok := d.ExecutionConfig["engine_id"].(float64); ok {
			id := uint(engineID)
			return &id
		}
		if engineID, ok := d.ExecutionConfig["engine_id"].(int); ok {
			id := uint(engineID)
			return &id
		}
	}
	return nil
}

// IsScheduledActive 判断是否启用调度
func (d *DevTask) IsScheduledActive() bool {
	return d.Schedule != "" && d.Schedule != "0"
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
	DevType         string                 `json:"dev_type" binding:"required,oneof=query workflow script notebook"`
	Content         map[string]interface{} `json:"content" binding:"required"`
	ExecutionConfig map[string]interface{} `json:"execution_config"`
	Schedule        string                 `json:"schedule"`
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
	Schedule        string                 `json:"schedule"`
	Timeout         int                    `json:"timeout"`
	Description     string                 `json:"description"`
	Tags            []string               `json:"tags"`
	Status          string                 `json:"status" binding:"omitempty,oneof=active inactive archived"`
}

// ListDevTasksRequest 查询开发任务列表请求
type ListDevTasksRequest struct {
	Page     int    `form:"page" binding:"min=1"`
	PageSize int    `form:"page_size" binding:"min=1,max=100"`
	DevType  string `form:"dev_type" binding:"omitempty,oneof=query workflow script notebook"`
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
