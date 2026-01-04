package format

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// ====================
// Scanner 相关类型
// 用于数据库和对象存储的元数据扫描
// ====================

// Scanner 数据库扫描器接口
// 用于扫描 PostgreSQL、MySQL 等关系型数据库的元数据
type Scanner interface {
	// ListSchemas 列出所有Schema（PostgreSQL）或数据库（MySQL）
	ListSchemas() ([]SchemaInfo, error)
	// ScanTables 扫描指定Schema的所有表
	ScanTables(schemaName string) ([]ScannerTableInfo, error)
	// ScanFields 扫描指定表的所有字段
	ScanFields(schemaName, tableName string) ([]ScannerFieldInfo, error)
	// Close 关闭连接
	Close() error
}

// SchemaInfo Schema信息（PostgreSQL schema / MySQL database）
type SchemaInfo struct {
	Name           string
	TableCount     int
	TotalSizeBytes int64
}

// ScannerTableInfo 表信息（Scanner 专用）
// 注意：这是旧的扫描器结构体，新代码应使用 TableInfo (info.go)
type ScannerTableInfo struct {
	Name      string
	Type      string
	Comment   string
	RowCount  int64
	SizeBytes int64
}

// ScannerFieldInfo 字段信息（Scanner 专用）
// 注意：这是旧的扫描器结构体，新代码应使用 FieldInfo (info.go)
type ScannerFieldInfo struct {
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

// ====================
// 对象存储扫描相关类型
// ====================

// ObjectStorageScanner 对象存储扫描器接口
// 用于扫描 S3、MinIO、OSS 等对象存储的元数据
type ObjectStorageScanner interface {
	// ListNodes 列出指定路径下的节点（目录和文件）
	ListNodes(path string) ([]ObjectNode, error)
	// ScanPath 扫描指定路径下的所有对象，提取元数据
	ScanPath(path string) ([]ObjectMetadata, error)
	// AllowedBuckets 返回允许访问的 bucket 列表
	AllowedBuckets() []string
}

// ObjectNode 对象存储节点（用于目录浏览）
type ObjectNode struct {
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	Type         string     `json:"type"` // bucket/prefix/object
	SizeBytes    int64      `json:"size_bytes"`
	FileType     string     `json:"file_type"`
	LastModified *time.Time `json:"last_modified,omitempty"`
	ObjectCount  int64      `json:"object_count"`
}

// ObjectMetadata 扫描后的对象存储元数据
type ObjectMetadata struct {
	Bucket            string
	Path              string
	RelativePath      string
	NodeType          string
	FileType          string
	SizeBytes         int64
	ObjectCount       int64
	LastModified      *time.Time
	ExtractedMetadata *ExtractedMetadata // 提取的详细元数据
}

// ====================
// 元数据提取相关类型
// ====================

// FileMetadataExtractor 文件元数据提取器接口
// 用于从各种文件类型中提取特定的元数据
type FileMetadataExtractor interface {
	// SupportedTypes 返回支持的文件类型/MIME类型
	SupportedTypes() []string
	// Extract 从输入中提取元数据
	Extract(ctx context.Context, input ExtractInput) (*ExtractedMetadata, error)
	// Priority 返回提取器优先级（数字越大优先级越高）
	Priority() int
}

// ExtractInput 元数据提取输入
type ExtractInput struct {
	EngineID     uint              // 存储引擎ID (来自 system.engines)
	ObjectKey    string            // 对象键/文件路径
	ContentType  string            // 内容类型 (MIME type)
	Size         int64             // 文件大小（字节）
	Reader       io.Reader         // 内容读取器（可选，用于需要读取内容的提取器）
	Metadata     map[string]string // 基础元数据（S3/MinIO返回的用户自定义元数据）
	LastModified time.Time         // 最后修改时间
	ETag         string            // ETag（对象版本标识）
}

// ExtractedMetadata 提取的元数据结果
type ExtractedMetadata struct {
	BasicInfo   BasicMetadata          // 基础信息（必需）
	SchemaInfo  *SchemaMetadata        // 结构化数据的schema（可选）
	PreviewData interface{}            // 预览数据（可选）
	CustomAttrs map[string]interface{} // 自定义属性（扩展字段）
}

// BasicMetadata 基础元数据
type BasicMetadata struct {
	FileName     string    // 文件名
	FileType     string    // 文件类型（友好名称，如 "GeoJSON", "CSV"）
	Size         int64     // 文件大小
	ContentType  string    // MIME类型
	Encoding     string    // 字符编码（如 "UTF-8"）
	LastModified time.Time // 最后修改时间
	Checksum     string    // 校验和 (MD5/SHA256)
	ETag         string    // ETag
}

// SchemaMetadata 结构化数据的schema信息
// 适用于CSV、Parquet、GeoJSON FeatureCollection等有表格结构的数据
type SchemaMetadata struct {
	Columns    []ColumnMetadata           // 列信息
	RowCount   int64                      // 总行数（-1表示未知）
	SampleData []map[string]interface{}   // 前N行样本数据（可选）
	Extra      map[string]interface{}     // 额外的schema信息
}

// ColumnMetadata 列元数据
type ColumnMetadata struct {
	Name        string      // 列名
	Type        string      // 数据类型（string, int, float, bool, geometry等）
	Nullable    bool        // 是否可为空
	Description string      // 列描述（可选）
	Example     interface{} // 示例值（可选）
}

// ====================
// Extractor Registry
// ====================

// ExtractorRegistry 元数据提取器注册表
type ExtractorRegistry struct {
	extractors map[string][]FileMetadataExtractor // key: MIME type
	mu         sync.RWMutex
}

var (
	globalExtractorRegistry = NewExtractorRegistry()
)

// NewExtractorRegistry 创建提取器注册中心
func NewExtractorRegistry() *ExtractorRegistry {
	return &ExtractorRegistry{
		extractors: make(map[string][]FileMetadataExtractor),
	}
}

// RegisterExtractor 注册元数据提取器到全局注册表
func RegisterExtractor(extractor FileMetadataExtractor) error {
	return globalExtractorRegistry.Register(extractor)
}

// Register 注册提取器
func (r *ExtractorRegistry) Register(extractor FileMetadataExtractor) error {
	if extractor == nil {
		return fmt.Errorf("extractor cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, mimeType := range extractor.SupportedTypes() {
		// 规范化MIME类型（转小写）
		mimeType = strings.ToLower(strings.TrimSpace(mimeType))
		r.extractors[mimeType] = append(r.extractors[mimeType], extractor)
	}

	// 为每个MIME类型的提取器按优先级排序
	for _, extractors := range r.extractors {
		sort.Slice(extractors, func(i, j int) bool {
			return extractors[i].Priority() > extractors[j].Priority()
		})
	}

	return nil
}

// GetExtractor 根据内容类型获取最佳提取器（优先级最高的）
func GetExtractor(contentType string) FileMetadataExtractor {
	return globalExtractorRegistry.GetExtractor(contentType)
}

// GetExtractor 从当前注册表获取最佳提取器
func (r *ExtractorRegistry) GetExtractor(contentType string) FileMetadataExtractor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 规范化MIME类型
	contentType = strings.ToLower(strings.TrimSpace(contentType))

	// 1. 完全匹配
	if extractors := r.extractors[contentType]; len(extractors) > 0 {
		return extractors[0] // 返回优先级最高的
	}

	// 2. 尝试主类型匹配（例如 "image/jpeg" -> "image/*"）
	if parts := strings.Split(contentType, "/"); len(parts) == 2 {
		wildcardType := parts[0] + "/*"
		if extractors := r.extractors[wildcardType]; len(extractors) > 0 {
			return extractors[0]
		}
	}

	// 3. 尝试通配符匹配 "*/*"
	if extractors := r.extractors["*/*"]; len(extractors) > 0 {
		return extractors[0]
	}

	return nil
}

// ListRegisteredTypes 列出所有已注册的MIME类型
func ListRegisteredExtractorTypes() []string {
	return globalExtractorRegistry.ListTypes()
}

// ListTypes 列出当前注册表中的所有MIME类型
func (r *ExtractorRegistry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.extractors))
	for mimeType := range r.extractors {
		types = append(types, mimeType)
	}

	sort.Strings(types)
	return types
}
