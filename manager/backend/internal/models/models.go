package models

import (
	"time"

	commonModels "github.com/addp/common/models"
)

// 直接使用 Common 模块的类型，避免重复定义
type Engine = commonModels.Engine
type ConnectionInfo = commonModels.ConnectionInfo

// SearchHistory 记录用户的数据检索历史（全文检索 + 向量检索），按用户隔离
type SearchHistory struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"uniqueIndex:idx_search_history_user_query,priority:1;not null"`
	TenantID  *uint     `json:"tenant_id" gorm:"index"`
	Query     string    `json:"query" gorm:"size:512;not null;uniqueIndex:idx_search_history_user_query,priority:2"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JSONMap is now imported from common/models
// Use commonModels.JSONMap instead
type JSONMap = commonModels.JSONMap

// EngineListResponse API 响应
type EngineListResponse struct {
	Data  []Engine `json:"data"`
	Total int64    `json:"total"`
}

// MetadataScanResult 元数据扫描结果
type MetadataScanResult struct {
	TotalItems     int           `json:"total_items"`
	ManagedItems   int           `json:"managed_items"`
	UnmanagedItems int           `json:"unmanaged_items"`
	Items          []interface{} `json:"items"`
}

// DataExplorerEngine 数据探查引擎树节点
type DataExplorerEngine struct {
	ID         uint                 `json:"id"`
	Name       string               `json:"name"`
	EngineType string               `json:"engine_type"`
	Schemas    []DataExplorerSchema `json:"schemas"`
}

type ExplorerEngine struct {
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
	Mode                  string                   `json:"mode"`
	Columns               []string                 `json:"columns"`
	ColumnMetadata        []ColumnMetadata         `json:"column_metadata,omitempty"` // 列元数据（类型、是否可空、主键等）
	Rows                  []map[string]interface{} `json:"rows"`
	Total                 int                      `json:"total"`
	Page                  int                      `json:"page"`
	PageSize              int                      `json:"page_size"`
	GeometryColumns       []string                 `json:"geometry_columns"`
	RenderGeometryColumns map[string]string        `json:"render_geometry_columns,omitempty"` // 原几何列 -> 地图渲染列
	Object                *ObjectPreview           `json:"object,omitempty"`
	ItemMeta              *ItemMetadata            `json:"item_meta,omitempty"` // 数据项元数据（来自 meta 模块）
	// MVT preview metadata (for frontend to switch between GeoJSON and MVT rendering)
	EngineID   uint   `json:"engineId,omitempty"`   // Engine ID for MVT API
	Schema     string `json:"schema,omitempty"`     // Schema name for MVT API
	Table      string `json:"table,omitempty"`      // Table name for MVT API
	EngineType string `json:"engineType,omitempty"` // Engine type (e.g., "postgresql")
	// Spatial metadata (for spatial data preview)
	SRID   int       `json:"srid,omitempty"`   // 空间参考系统 ID
	Extent []float64 `json:"extent,omitempty"` // 空间范围 [minX, minY, maxX, maxY]
}

// ItemMetadata 数据项元数据（来自 meta 模块，附加在预览响应中）
type ItemMetadata struct {
	ItemType        string          `json:"item_type"`          // 原始类型值，如 "table", "label"
	ItemTypeI18nKey string          `json:"item_type_i18n_key"` // i18n key，如 "engine.term.table"
	FullName        string          `json:"full_name"`          // 在引擎内的完整路径
	Attributes      []MetaAttribute `json:"attributes"`         // key-value 列表（字段数、行数、大小等）
	ScannedAt       *time.Time      `json:"scanned_at,omitempty"`
}

// MetaAttribute 元数据属性键值对
type MetaAttribute struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// ColumnMetadata 列元数据
type ColumnMetadata struct {
	ColumnName   string `json:"column_name"`    // 列名
	DataType     string `json:"data_type"`      // 数据类型（如 varchar, int4, geometry）
	IsNullable   bool   `json:"is_nullable"`    // 是否可为空
	IsPrimaryKey bool   `json:"is_primary_key"` // 是否主键
	Comment      string `json:"comment"`        // 列注释
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
	Kind             string                 `json:"kind"`
	PreviewMaterial  string                 `json:"preview_material,omitempty"`
	FrontendRenderer string                 `json:"frontend_renderer,omitempty"`
	Text             string                 `json:"text,omitempty"`
	JSON             interface{}            `json:"json,omitempty"`
	GeoJSON          interface{}            `json:"geojson,omitempty"`
	ImageData        string                 `json:"image_data,omitempty"`
	Data             string                 `json:"data,omitempty"` // Generic data field (used for PDF base64)
	Encoding         string                 `json:"encoding,omitempty"`
	Truncated        bool                   `json:"truncated,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

const (
	ObjectPreviewKindPDF         = "pdf"
	ObjectPreviewKindDOCX        = "docx"
	ObjectPreviewKindWPS         = "wps"
	ObjectPreviewKindPPTX        = "pptx"
	ObjectPreviewKindImage       = "image"
	ObjectPreviewKindJSON        = "json"
	ObjectPreviewKindGeoJSON     = "geojson"
	ObjectPreviewKindContainer   = "container"
	ObjectPreviewKindExcel       = "excel"
	ObjectPreviewKindSQLite      = "sqlite"
	ObjectPreviewKindText        = "text"
	ObjectPreviewKindMarkdown    = "markdown"
	ObjectPreviewKindTable       = "table"
	ObjectPreviewKindShapefile   = "shapefile"
	ObjectPreviewKindUnsupported = "unsupported"
)

const (
	PreviewMaterialText      = "text"
	PreviewMaterialMarkdown  = "markdown"
	PreviewMaterialHTML      = "html"
	PreviewMaterialJSON      = "json"
	PreviewMaterialGeoJSON   = "geojson"
	PreviewMaterialImage     = "image"
	PreviewMaterialRawBinary = "raw_binary"
	PreviewMaterialTable     = "table"
	PreviewMaterialURL       = "url"
)

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
	Name          string   `json:"name" binding:"required"`
	Description   string   `json:"description"`
	EngineID      uint     `json:"engine_id"`
	Namespaces    []string `json:"namespaces"`
	ObjectPaths   []string `json:"object_paths"`
	ScanDepth     string   `json:"scan_depth"`
	ScheduleType  string   `json:"schedule_type" binding:"required"`
	ScheduleTime  string   `json:"schedule_time"`
	ScheduleValue []int    `json:"schedule_value"`
	Schedule      string   `json:"schedule"`
	Enabled       bool     `json:"enabled"`
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
	EngineName      string     `json:"engine_name,omitempty"`
	EngineType      string     `json:"engine_type,omitempty"`
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
	Namespaces  []string `json:"namespaces"`
	ObjectPaths []string `json:"object_paths"`
	ScanDepth   string   `json:"scan_depth"`
	ScanType    string   `json:"scan_type"`
}
