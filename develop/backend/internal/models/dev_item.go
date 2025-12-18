package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

// DevItem 统一开发项定义（SQL查询、工作流、脚本等）
type DevItem struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TenantID    uint           `gorm:"not null;index:idx_dev_items_tenant_type" json:"tenant_id"`
	Name        string         `gorm:"size:255;not null" json:"name"`
	DisplayName string         `gorm:"size:255" json:"display_name,omitempty"`
	DevType     string         `gorm:"size:50;not null;index:idx_dev_items_tenant_type" json:"dev_type"` // 'sql' | 'workflow' | 'script'

	// 内容存储（根据类型解析）
	Content DevItemContent `gorm:"type:jsonb;not null" json:"content"`

	// 执行配置
	ResourceID  *uint  `gorm:"index:idx_dev_items_resource" json:"resource_id,omitempty"`
	Schedule    string `gorm:"size:100" json:"schedule,omitempty"`     // Cron 表达式
	IsScheduled bool   `gorm:"default:false" json:"is_scheduled"`
	Timeout     int    `gorm:"default:300" json:"timeout"` // 超时时间（秒）

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
	Status              string     `gorm:"size:50;default:'active';index:idx_dev_items_status" json:"status"` // 'active' | 'inactive' | 'archived'
	LastExecutionID     *uint      `json:"last_execution_id,omitempty"`
	LastExecutionStatus string     `gorm:"size:50" json:"last_execution_status,omitempty"`
	LastExecutedAt      *time.Time `json:"last_executed_at,omitempty"`
}

// TableName 指定表名
func (DevItem) TableName() string {
	return "develop.dev_items"
}

// DevItemContent 开发项内容（支持任意 JSON 结构）
type DevItemContent map[string]interface{}

// Value 实现 driver.Valuer 接口
func (c DevItemContent) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	return json.Marshal(c)
}

// Scan 实现 sql.Scanner 接口
func (c *DevItemContent) Scan(value interface{}) error {
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

// CreateDevItemRequest 创建开发项请求
type CreateDevItemRequest struct {
	Name        string                 `json:"name" binding:"required"`
	DisplayName string                 `json:"display_name"`
	DevType     string                 `json:"dev_type" binding:"required,oneof=sql workflow script"`
	Content     map[string]interface{} `json:"content" binding:"required"`
	ResourceID  *uint                  `json:"resource_id"`
	Schedule    string                 `json:"schedule"`
	IsScheduled bool                   `json:"is_scheduled"`
	Timeout     int                    `json:"timeout"`
	Description string                 `json:"description"`
	Tags        []string               `json:"tags"`
}

// UpdateDevItemRequest 更新开发项请求
type UpdateDevItemRequest struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"display_name"`
	Content     map[string]interface{} `json:"content"`
	ResourceID  *uint                  `json:"resource_id"`
	Schedule    string                 `json:"schedule"`
	IsScheduled bool                   `json:"is_scheduled"`
	Timeout     int                    `json:"timeout"`
	Description string                 `json:"description"`
	Tags        []string               `json:"tags"`
	Status      string                 `json:"status" binding:"omitempty,oneof=active inactive archived"`
}

// ListDevItemsRequest 查询开发项列表请求
type ListDevItemsRequest struct {
	Page       int    `form:"page" binding:"min=1"`
	PageSize   int    `form:"page_size" binding:"min=1,max=100"`
	DevType    string `form:"dev_type" binding:"omitempty,oneof=sql workflow script"`
	Status     string `form:"status" binding:"omitempty,oneof=active inactive archived"`
	ResourceID *uint  `form:"resource_id"`
	Tag        string `form:"tag"`
	Keyword    string `form:"keyword"` // 搜索名称或描述
}

// ListDevItemsResponse 开发项列表响应
type ListDevItemsResponse struct {
	Items    []DevItem `json:"items"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}
