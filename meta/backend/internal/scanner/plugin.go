package scanner

import (
	"context"
	"io"
	"time"
)

// MetadataExtractor 元数据提取器接口
// 每个文件类型的提取器需要实现此接口
type MetadataExtractor interface {
	// SupportedTypes 返回支持的文件类型/MIME类型
	// 例如: ["application/geo+json", "application/json"]
	SupportedTypes() []string

	// Extract 从输入中提取元数据
	Extract(ctx context.Context, input ExtractInput) (*Metadata, error)

	// Priority 返回提取器优先级（数字越大优先级越高）
	// 当多个提取器支持同一MIME类型时，使用优先级最高的
	Priority() int
}

// ExtractInput 元数据提取输入
type ExtractInput struct {
	ResourceID   uint              // 数据源ID (来自 system.resources)
	ObjectKey    string            // 对象键/文件路径
	ContentType  string            // 内容类型 (MIME type)
	Size         int64             // 文件大小（字节）
	Reader       io.Reader         // 内容读取器（可选，用于需要读取内容的提取器）
	Metadata     map[string]string // 基础元数据（S3/MinIO返回的用户自定义元数据）
	LastModified time.Time         // 最后修改时间
	ETag         string            // ETag（对象版本标识）
}

// Metadata 提取的元数据结果
type Metadata struct {
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
	Columns    []ColumnInfo               // 列信息
	RowCount   int64                      // 总行数（-1表示未知）
	SampleData []map[string]interface{}   // 前N行样本数据（可选）
	Extra      map[string]interface{}     // 额外的schema信息
}

// ColumnInfo 列信息
type ColumnInfo struct {
	Name        string      // 列名
	Type        string      // 数据类型（string, int, float, bool, geometry等）
	Nullable    bool        // 是否可为空
	Description string      // 列描述（可选）
	Example     interface{} // 示例值（可选）
}

// GeoMetadata, ImageMetadata, DocumentMetadata are now defined in SDK
// and imported via sdk_adapter.go for backward compatibility
