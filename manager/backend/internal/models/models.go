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
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

type MetadataSchemaLite struct {
	ID         uint       `json:"id" gorm:"column:id"`
	ResourceID uint       `json:"resource_id" gorm:"column:resource_id"`
	SchemaName string     `json:"schema_name" gorm:"column:schema_name"`
	LastScanAt *time.Time `json:"last_scan_at" gorm:"column:last_scan_at"`
	TableCount int        `json:"table_count" gorm:"column:table_count"`
}

type MetadataTableLite struct {
	ID        uint       `json:"id" gorm:"column:id"`
	SchemaID  uint       `json:"schema_id" gorm:"column:schema_id"`
	TableName string     `json:"table_name" gorm:"column:table_name"`
	LastScan  *time.Time `json:"last_scan_at" gorm:"column:last_scan_at"`
}

// TablePreview 表数据预览结果
type TablePreview struct {
	Columns         []string                 `json:"columns"`
	Rows            []map[string]interface{} `json:"rows"`
	Total           int                      `json:"total"`
	Page            int                      `json:"page"`
	PageSize        int                      `json:"page_size"`
	GeometryColumns []string                 `json:"geometry_columns"`
}
