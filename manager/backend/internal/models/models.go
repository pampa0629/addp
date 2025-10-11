package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// Resource 直接映射到 system.resources 表
type Resource struct {
	ID             uint           `json:"id" gorm:"primaryKey"`
	Name           string         `json:"name"`
	ResourceType   string         `json:"resource_type"` // postgresql, minio
	ConnectionInfo ConnectionInfo `json:"connection_info" gorm:"type:jsonb"`
	Description    string         `json:"description"`
	CreatedBy      *uint          `json:"created_by"`
	TenantID       *uint          `json:"tenant_id"`
	IsActive       bool           `json:"is_active" gorm:"default:true"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// TableName 指定表名为 system.resources
func (Resource) TableName() string {
	return "system.resources"
}

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

type JSONMap map[string]interface{}

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal(bytes, &data); err != nil {
		return err
	}
	*m = JSONMap(data)
	return nil
}

// ResourceListResponse API 响应
type ResourceListResponse struct {
	Data  []Resource `json:"data"`
	Total int64      `json:"total"`
}

// ManagedTable 纳入管理的数据库表
type ManagedTable struct {
	ID          uint            `json:"id" gorm:"primaryKey"`
	ResourceID  uint            `json:"resource_id" gorm:"index"` // system.resources 的 ID
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
	ResourceID   uint            `json:"resource_id" gorm:"index"`              // system.resources 的 ID
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
	ID           uint                 `json:"id"`
	Name         string               `json:"name"`
	ResourceType string               `json:"resource_type"`
	Schemas      []DataExplorerSchema `json:"schemas"`
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
	ResourceID     uint       `json:"resource_id" gorm:"column:resource_id"`
	ResID          uint       `json:"res_id" gorm:"column:res_id"`
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
	ResourceID      uint       `json:"resource_id" gorm:"column:resource_id"`
	ResID           uint       `json:"res_id" gorm:"column:res_id"`
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
}

type ObjectPreview struct {
	Bucket       string                `json:"bucket"`
	Path         string                `json:"path"`
	NodeType     string                `json:"node_type"`
	SizeBytes    int64                 `json:"size_bytes"`
	ObjectCount  int64                 `json:"object_count,omitempty"`
	LastModified *time.Time            `json:"last_modified,omitempty"`
	ContentType  string                `json:"content_type,omitempty"`
	Metadata     map[string]string     `json:"metadata,omitempty"`
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
	Kind      string      `json:"kind"`
	Text      string      `json:"text,omitempty"`
	JSON      interface{} `json:"json,omitempty"`
	GeoJSON   interface{} `json:"geojson,omitempty"`
	ImageData string      `json:"image_data,omitempty"`
	Data      string      `json:"data,omitempty"`      // Generic data field (used for PDF base64)
	Encoding  string      `json:"encoding,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
}
