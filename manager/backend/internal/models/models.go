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
	PreviewKind           string                   `json:"preview_kind,omitempty"` // 细分预览语义，如 graph_overview
	GeometryColumns       []string                 `json:"geometry_columns"`
	RenderGeometryColumns map[string]string        `json:"render_geometry_columns,omitempty"` // 原几何列 -> 地图渲染列
	Object                *ObjectPreview           `json:"object,omitempty"`
	Graph                 *GraphPreviewData        `json:"graph,omitempty"`
	ItemMeta              *ItemMetadata            `json:"item_meta,omitempty"` // 数据项元数据（来自 meta 模块）
	// MVT preview metadata (for frontend to switch between GeoJSON and MVT rendering)
	EngineID   uint   `json:"engineId,omitempty"`   // Engine ID for MVT API
	Schema     string `json:"schema,omitempty"`     // Schema name for MVT API
	Table      string `json:"table,omitempty"`      // Table name for MVT API
	EngineType string `json:"engineType,omitempty"` // Engine type for downstream preview APIs
	// Spatial metadata (for spatial data preview)
	SRID   int       `json:"srid,omitempty"`   // 空间参考系统 ID
	Extent []float64 `json:"extent,omitempty"` // 空间范围 [minX, minY, maxX, maxY]
}

// GraphPreviewData 图数据轻量样本，供通用预览按需展示。
type GraphPreviewData struct {
	Nodes         []GraphPreviewNode         `json:"nodes"`
	Relationships []GraphPreviewRelationship `json:"relationships"`
}

type GraphPreviewNode struct {
	ElementID  string                 `json:"element_id"`
	Labels     []string               `json:"labels"`
	Properties map[string]interface{} `json:"properties"`
}

type GraphPreviewRelationship struct {
	ElementID   string                 `json:"element_id"`
	Type        string                 `json:"type"`
	StartNodeID string                 `json:"start_node_id"`
	EndNodeID   string                 `json:"end_node_id"`
	Properties  map[string]interface{} `json:"properties"`
}

// ItemMetadata 数据项元数据（来自 meta 模块，附加在预览响应中）
type ItemMetadata struct {
	ItemType        string          `json:"item_type"`          // 原始类型值，如 "table", "graph"
	ItemTypeI18nKey string          `json:"item_type_i18n_key"` // i18n key，如 "engine.term.table"
	FullName        string          `json:"full_name"`          // 在引擎内的完整路径
	RowCount        *int64          `json:"row_count,omitempty"`
	Attributes      []MetaAttribute `json:"attributes"` // key-value 列表（字段数、行数、大小等）
	ScannedAt       *time.Time      `json:"scanned_at,omitempty"`
}

// MetaAttribute 元数据属性键值对
type MetaAttribute struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// ColumnMetadata 列元数据
type ColumnMetadata struct {
	ColumnName   string `json:"column_name"` // 列名
	Type         string `json:"type"`        // 字段类型
	IsNullable   bool   `json:"nullable"`    // 是否可为空
	IsPrimaryKey bool   `json:"primary_key"` // 是否主键
	Comment      string `json:"comment"`     // 列注释
}

type ObjectPreview struct {
	Bucket       string                `json:"bucket"`
	Path         string                `json:"path"`
	NodeType     string                `json:"node_type"`
	SizeBytes    int64                 `json:"size_bytes"`
	ObjectCount  int64                 `json:"object_count,omitempty"`
	LastModified *time.Time            `json:"last_modified,omitempty"`
	ContentType  string                `json:"content_type,omitempty"`
	URL          string                `json:"url,omitempty"`
	Metadata     map[string]string     `json:"metadata,omitempty"`
	Attributes   JSONMap               `json:"attributes,omitempty"`
	EngineID     uint                  `json:"engine_id,omitempty"`
	StorageRef   string                `json:"storage_ref,omitempty"`
	Children     []ObjectPreviewChild  `json:"children,omitempty"`
	Content      *ObjectPreviewContent `json:"content,omitempty"`
	Truncated    bool                  `json:"truncated,omitempty"`
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
	Data             string                 `json:"data,omitempty"` // Generic data field (used for PDF base64)
	URL              string                 `json:"url,omitempty"`
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
	ObjectPreviewKindVideo       = "video"
	ObjectPreviewKindJSON        = "json"
	ObjectPreviewKindContainer   = "container"
	ObjectPreviewKindText        = "text"
	ObjectPreviewKindMarkdown    = "markdown"
	ObjectPreviewKindTable       = "table"
	ObjectPreviewKindUnsupported = "unsupported"
)

const (
	// PreviewMaterial* values describe the material Manager sends to the frontend
	// renderer. They are presentation-layer values, not common/format content
	// reader capability names such as raw_content, range_content, or binary_content.
	PreviewMaterialText      = "text"
	PreviewMaterialMarkdown  = "markdown"
	PreviewMaterialHTML      = "html"
	PreviewMaterialJSON      = "json"
	PreviewMaterialGeoJSON   = "geojson"
	PreviewMaterialRawBinary = "raw_binary"
	PreviewMaterialTable     = "table"
	PreviewMaterialURL       = "url"
)

func IsKnownPreviewMaterial(material string) bool {
	switch material {
	case PreviewMaterialText,
		PreviewMaterialMarkdown,
		PreviewMaterialHTML,
		PreviewMaterialJSON,
		PreviewMaterialGeoJSON,
		PreviewMaterialRawBinary,
		PreviewMaterialTable,
		PreviewMaterialURL:
		return true
	default:
		return false
	}
}

// MetaManualScanRequest 发起即时扫描的请求体
type MetaManualScanRequest struct {
	EngineID     uint     `json:"engine_id"`
	NodeID       uint     `json:"node_id,omitempty"`
	ItemID       uint     `json:"item_id,omitempty"`
	Targets      []string `json:"targets,omitempty"`
	CatalogPaths []string `json:"catalog_paths"`
	ScanDepth    string   `json:"scan_depth"`
	Force        bool     `json:"force"`
}

// MetaScanResponse 元数据扫描响应
type MetaScanResponse struct {
	Status              string                   `json:"status"`
	Message             string                   `json:"message"`
	CatalogNodesScanned int                      `json:"catalog_nodes_scanned"`
	ItemsScanned        int                      `json:"items_scanned"`
	FieldsScanned       int                      `json:"fields_scanned"`
	DurationMs          int64                    `json:"duration_ms"`
	StartedAt           string                   `json:"started_at"`
	Extraction          *MetaExtractionScanStats `json:"extraction,omitempty"`
}

type MetaExtractionScanStats struct {
	Documents   int `json:"documents"`
	Extracted   int `json:"extracted"`
	Unsupported int `json:"unsupported"`
	Failed      int `json:"failed"`
	Indexed     int `json:"indexed"`
	IndexFailed int `json:"index_failed"`
}
