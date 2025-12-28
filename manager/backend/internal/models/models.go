package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	commonModels "github.com/addp/common/models"
)

// Engine 直接映射到 system.engines 表
type Engine struct {
	ID             uint           `json:"id" gorm:"primaryKey"`
	Name           string         `json:"name"`
	EngineType     string         `json:"engine_type"` // postgresql, minio
	ConnectionInfo ConnectionInfo `json:"connection_info" gorm:"type:jsonb"`
	Description    string         `json:"description"`
	CreatedBy      *uint          `json:"created_by"`
	TenantID       *uint          `json:"tenant_id"`
	IsActive       bool           `json:"is_active" gorm:"default:true"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// TableName 指定表名为 system.engines
func (Engine) TableName() string {
	return "system.engines"
}

// Resource 是 Engine 的别名，保持向后兼容
type Resource = Engine

// Directory 目录结构
type Directory struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name"`
	ParentID  *uint     `json:"parent_id"`
	Path      string    `json:"path" gorm:"index"`
	Type      string    `json:"type"` // folder, file
	Size      int64     `json:"size"`
	MimeType  string    `json:"mime_type"`
	StorageID *uint     `json:"storage_id"` // 关联的存储引擎 ID
	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SearchHistory 记录用户的全文检索历史，按用户隔离
type SearchHistory struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"uniqueIndex:idx_search_history_user_query,priority:1;not null"`
	TenantID  *uint     `json:"tenant_id" gorm:"index"`
	Query     string    `json:"query" gorm:"size:512;not null;uniqueIndex:idx_search_history_user_query,priority:2"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConnectionInfo 存储连接信息
type ConnectionInfo map[string]interface{}

func (c ConnectionInfo) Value() (driver.Value, error) {
	return json.Marshal(c)
}

func (c *ConnectionInfo) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

// JSONMap is now imported from common/models
// Use commonModels.JSONMap instead
type JSONMap = commonModels.JSONMap

// ResourceListResponse API 响应
type ResourceListResponse struct {
	Data  []Resource `json:"data"`
	Total int64      `json:"total"`
}

// ManagedTable 纳入管理的数据库表
type ManagedTable struct {
	ID          uint            `json:"id" gorm:"primaryKey"`
	EngineID    uint            `json:"engine_id" gorm:"index"` // system.engines 的 ID
	SchemaName  string          `json:"schema_name"`              // schema/database名称
	TableName   string          `json:"table_name" gorm:"index"`
	FullName    string          `json:"full_name"`                             // schema.table 完整名称
	IsManaged   bool            `json:"is_managed" gorm:"default:false;index"` // 是否已纳入管理
	RowCount    *int64          `json:"row_count"`                             // 行数
	TableSize   *int64          `json:"table_size"`                            // 表大小（字节）
	TableType   string          `json:"table_type"`                            // BASE TABLE, VIEW
	Comment     string          `json:"comment"`                               // 表注释
	Schema      json.RawMessage `json:"schema" gorm:"type:jsonb"`              // 详细schema（仅纳管后提取）
	SampleData  json.RawMessage `json:"sample_data" gorm:"type:jsonb"`         // 采样数据
	LastScanned *time.Time      `json:"last_scanned"`                          // 最后扫描时间
	LastManaged *time.Time      `json:"last_managed"`                          // 纳管时间
	Tags        []string        `json:"tags" gorm:"type:jsonb"`                // 标签
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// TableColumn 表字段信息
type TableColumn struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsNullable   bool   `json:"is_nullable"`
	DefaultValue string `json:"default_value"`
	Comment      string `json:"comment"`
	IsPrimaryKey bool   `json:"is_primary_key"`
}

// ManagedFile 纳入管理的对象存储文件/目录
type ManagedFile struct {
	ID           uint            `json:"id" gorm:"primaryKey"`
	EngineID     uint            `json:"engine_id" gorm:"index"`              // system.engines 的 ID
	Bucket       string          `json:"bucket"`                                // bucket名称
	Path         string          `json:"path" gorm:"index"`                     // 完整路径
	FileName     string          `json:"file_name"`                             // 文件名
	FileType     string          `json:"file_type"`                             // directory, file
	MimeType     string          `json:"mime_type"`                             // 文件MIME类型
	Size         int64           `json:"size"`                                  // 文件大小
	IsManaged    bool            `json:"is_managed" gorm:"default:false;index"` // 是否已纳入管理
	FileFormat   string          `json:"file_format"`                           // parquet, csv, json, etc
	Schema       json.RawMessage `json:"schema" gorm:"type:jsonb"`              // 文件schema（结构化文件）
	RowCount     *int64          `json:"row_count"`                             // 行数（结构化文件）
	SampleData   json.RawMessage `json:"sample_data" gorm:"type:jsonb"`         // 采样数据
	LastModified time.Time       `json:"last_modified"`                         // 对象存储中的最后修改时间
	LastScanned  *time.Time      `json:"last_scanned"`                          // 最后扫描时间
	LastManaged  *time.Time      `json:"last_managed"`                          // 纳管时间
	Tags         []string        `json:"tags" gorm:"type:jsonb"`                // 标签
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// MetadataScanResult 元数据扫描结果
type MetadataScanResult struct {
	TotalItems     int           `json:"total_items"`
	ManagedItems   int           `json:"managed_items"`
	UnmanagedItems int           `json:"unmanaged_items"`
	Items          []interface{} `json:"items"`
}

// DataExplorerResource 数据探查资源树节点
type DataExplorerResource struct {
	ID         uint                 `json:"id"`
	Name       string               `json:"name"`
	EngineType string               `json:"engine_type"`
	Schemas    []DataExplorerSchema `json:"schemas"`
}

type ExplorerResource struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	EngineType  string `json:"engine_type"`
	Description string `json:"description,omitempty"`
}

type DataExplorerSchema struct {
	Name   string              `json:"name"`
	Tables []DataExplorerTable `json:"tables"`
}

type DataExplorerTable struct {
	ID          uint                `json:"id,omitempty"`
	Name        string              `json:"name"`
	FullName    string              `json:"full_name,omitempty"`
	Type        string              `json:"type,omitempty"` // table/object/directory
	Parent      string              `json:"parent_path,omitempty"`
	Depth       int                 `json:"depth,omitempty"`
	SizeBytes   int64               `json:"size_bytes,omitempty"`   // 对象存储文件大小
	ObjectCount int64               `json:"object_count,omitempty"` // 目录包含对象数量
	ContentType string              `json:"content_type,omitempty"`
	Children    []DataExplorerTable `json:"children,omitempty"` // 目录子节点
}

type MetaNodeLite struct {
	ID             uint       `json:"id" gorm:"column:id"`
	EngineID       uint       `json:"engine_id" gorm:"column:engine_id"`
	ParentNodeID   *uint      `json:"parent_node_id" gorm:"column:parent_node_id"`
	NodeType       string     `json:"node_type" gorm:"column:node_type"`
	Name           string     `json:"name" gorm:"column:name"`
	FullName       string     `json:"full_name" gorm:"column:full_name"`
	Path           string     `json:"path" gorm:"column:path"`
	Depth          int        `json:"depth" gorm:"column:depth"`
	LastScanAt     *time.Time `json:"last_scan_at" gorm:"column:last_scan_at"`
	ItemCount      int        `json:"item_count" gorm:"column:item_count"`
	TotalSizeBytes int64      `json:"total_size_bytes" gorm:"column:total_size_bytes"`
	Attributes     JSONMap    `json:"attributes" gorm:"column:attributes"`
}

type MetaItemLite struct {
	ID              uint       `json:"id" gorm:"column:id"`
	EngineID        uint       `json:"engine_id" gorm:"column:engine_id"`
	NodeID          uint       `json:"node_id" gorm:"column:node_id"`
	ItemType        string     `json:"item_type" gorm:"column:item_type"`
	Name            string     `json:"name" gorm:"column:name"`
	FullName        string     `json:"full_name" gorm:"column:full_name"`
	RowCount        *int64     `json:"row_count" gorm:"column:row_count"`
	SizeBytes       *int64     `json:"size_bytes" gorm:"column:size_bytes"`
	ObjectSizeBytes *int64     `json:"object_size_bytes" gorm:"column:object_size_bytes"`
	LastModifiedAt  *time.Time `json:"last_modified_at" gorm:"column:last_modified_at"`
	Attributes      JSONMap    `json:"attributes" gorm:"column:attributes"`
}

// TablePreview 表数据预览结果
type TablePreview struct {
	Mode            string                   `json:"mode"`
	Columns         []string                 `json:"columns"`
	Rows            []map[string]interface{} `json:"rows"`
	Total           int                      `json:"total"`
	Page            int                      `json:"page"`
	PageSize        int                      `json:"page_size"`
	GeometryColumns []string                 `json:"geometry_columns"`
	Object          *ObjectPreview           `json:"object,omitempty"`
	// MVT preview metadata (for frontend to switch between GeoJSON and MVT rendering)
	EngineID   uint   `json:"resourceId,omitempty"` // Resource ID for MVT API
	Schema     string `json:"schema,omitempty"`     // Schema name for MVT API
	Table      string `json:"table,omitempty"`      // Table name for MVT API
	EngineType string `json:"engineType,omitempty"` // Engine type (e.g., "postgresql")
}

type ObjectPreview struct {
	Bucket            string                 `json:"bucket"`
	Path              string                 `json:"path"`
	NodeType          string                 `json:"node_type"`
	SizeBytes         int64                  `json:"size_bytes"`
	ObjectCount       int64                  `json:"object_count,omitempty"`
	LastModified      *time.Time             `json:"last_modified,omitempty"`
	ContentType       string                 `json:"content_type,omitempty"`
	Metadata          map[string]string      `json:"metadata,omitempty"`
	Attributes        JSONMap                `json:"attributes,omitempty"`
	EngineID          uint                   `json:"engine_id,omitempty"`
	ObjectKey         string                 `json:"object_key,omitempty"`
	Children          []ObjectPreviewChild   `json:"children,omitempty"`
	Content           *ObjectPreviewContent  `json:"content,omitempty"`
	Truncated         bool                   `json:"truncated,omitempty"`
	ExtractedMetadata map[string]interface{} `json:"extracted_metadata,omitempty"` // 从Meta模块提取的深度元数据
}

type ObjectPreviewChild struct {
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	Type         string     `json:"type"`
	SizeBytes    int64      `json:"size_bytes"`
	LastModified *time.Time `json:"last_modified,omitempty"`
	ContentType  string     `json:"content_type,omitempty"`
}

type ObjectPreviewContent struct {
	Kind      string                 `json:"kind"`
	Text      string                 `json:"text,omitempty"`
	JSON      interface{}            `json:"json,omitempty"`
	GeoJSON   interface{}            `json:"geojson,omitempty"`
	ImageData string                 `json:"image_data,omitempty"`
	Data      string                 `json:"data,omitempty"` // Generic data field (used for PDF base64)
	Encoding  string                 `json:"encoding,omitempty"`
	Truncated bool                   `json:"truncated,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// MetaScanTask 描述 Meta 服务中的扫描任务
type MetaScanTask struct {
	ID             uint       `json:"id"`
	TenantID       uint       `json:"tenant_id"`
	EngineID       uint       `json:"engine_id"`
	Name           string     `json:"name"`
	Description    string     `json:"description,omitempty"`
	ScheduleType   string     `json:"schedule_type"`
	Schedule       string     `json:"schedule,omitempty"`
	Enabled        bool       `json:"enabled"`
	Parameters     JSONMap    `json:"parameters,omitempty"`
	ScheduleConfig JSONMap    `json:"schedule_config,omitempty"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	CreatedBy      uint       `json:"created_by,omitempty"`
	UpdatedBy      uint       `json:"updated_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// MetaScanTaskRequest 创建或更新扫描任务时的表单
type MetaScanTaskRequest struct {
	Name           string   `json:"name" binding:"required"`
	Description    string   `json:"description"`
	EngineID       uint     `json:"engine_id"`
	SchemaNames    []string `json:"schema_names"`
	ObjectPaths    []string `json:"object_paths"`
	ScanDepth      string   `json:"scan_depth"`
	ScheduleType   string   `json:"schedule_type" binding:"required"`
	ScheduleTime   string   `json:"schedule_time"`
	ScheduleValue  []int    `json:"schedule_value"`
	Schedule       string   `json:"schedule"`
	Enabled        bool     `json:"enabled"`
}

// MetaScanTaskRun 表示一次具体的扫描运行
type MetaScanTaskRun struct {
	ID              uint       `json:"id"`
	TaskID          *uint      `json:"task_id,omitempty"`
	TenantID        uint       `json:"tenant_id"`
	EngineID        uint       `json:"engine_id"`
	Name            string     `json:"name"`
	StorageType     string     `json:"storage_type"`
	TaskName        string     `json:"task_name"`
	TaskPlanName    string     `json:"task_plan_name,omitempty"`
	ResourceName    string     `json:"resource_name,omitempty"`
	ResourceType    string     `json:"resource_type,omitempty"`
	TriggerType     string     `json:"trigger_type"`
	Status          string     `json:"status"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	Parameters      JSONMap    `json:"parameters,omitempty"`
	ResultSummary   JSONMap    `json:"result_summary,omitempty"`
	ProgressTotal   int        `json:"progress_total"`
	ProgressCurrent int        `json:"progress_current"`
	ProgressMessage string     `json:"progress_message,omitempty"`
	ProgressPercent float64    `json:"progress_percent"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	TriggerUserID   *uint      `json:"trigger_user_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// MetaManualScanRequest 发起即时扫描的请求体
type MetaManualScanRequest struct {
	SchemaNames []string `json:"schema_names"`
	ObjectPaths []string `json:"object_paths"`
	ScanDepth   string   `json:"scan_depth"`
	ScanType    string   `json:"scan_type"`
}
