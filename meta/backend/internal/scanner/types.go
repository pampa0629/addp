package scanner

import "time"

// SchemaInfo Schema信息（PostgreSQL schema / MySQL database）
type SchemaInfo struct {
	Name           string
	TableCount     int
	TotalSizeBytes int64
}

// TableInfo 表信息
type TableInfo struct {
	Name           string
	Type           string
	Comment        string
	RowCount       int64
	SizeBytes      int64
}

// FieldInfo 字段信息
type FieldInfo struct {
	Name             string
	OrdinalPosition  int
	DataType         string
	ColumnType       string
	IsNullable       bool
	DefaultValue     string
	Comment          string
	IsPrimaryKey     bool
	IsUniqueKey      bool
	CharacterSet     string
	Collation        string
	NumericPrecision int
	NumericScale     int
}

// Scanner 扫描器接口
type Scanner interface {
	// ListSchemas 列出所有Schema（PostgreSQL）或数据库（MySQL）
	ListSchemas() ([]SchemaInfo, error)
	// ScanTables 扫描指定Schema的所有表
	ScanTables(schemaName string) ([]TableInfo, error)
	// ScanFields 扫描指定表的所有字段
	ScanFields(schemaName, tableName string) ([]FieldInfo, error)
	// Close 关闭连接
	Close() error
}

// ObjectNode 对象存储节点（用于目录浏览）
type ObjectNode struct {
 Name         string    `json:"name"`
 Path         string    `json:"path"`
 Type         string    `json:"type"` // bucket/prefix/object
 SizeBytes    int64     `json:"size_bytes"`
 FileType     string    `json:"file_type"`
 LastModified *time.Time `json:"last_modified,omitempty"`
 ObjectCount  int64     `json:"object_count"`
}

// ObjectMetadata 扫描后的对象存储元数据
type ObjectMetadata struct {
 Bucket        string
 Path          string
 RelativePath  string
 NodeType      string
 FileType      string
 SizeBytes     int64
 ObjectCount   int64
 LastModified  *time.Time
}

// ObjectStorageScanner 对象存储扫描器接口
type ObjectStorageScanner interface {
 ListNodes(path string) ([]ObjectNode, error)
 ScanPath(path string) ([]ObjectMetadata, error)
 AllowedBuckets() []string
}
