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

// ObjectMetadata 表示对象存储或文件系统扫描得到的轻量资源元数据。
type ObjectMetadata struct {
	Bucket              string
	Path                string
	NodeType            string
	FileType            string
	SizeBytes           int64
	ObjectCount         int64
	LastModified        *time.Time
	ExtractedMetadata   *ExtractedMetadata
	ExtractedAttributes map[string]interface{}
}

// FileMetadataExtractor 从文件内容中提取格式相关的增强元数据。
type FileMetadataExtractor interface {
	SupportedTypes() []string
	Extract(ctx context.Context, input ExtractInput) (*ExtractedMetadata, error)
	Priority() int
}

// ExtractInput 是文件元数据提取输入。
type ExtractInput struct {
	ObjectKey    string
	ContentType  string
	Size         int64
	Reader       io.Reader
	Metadata     map[string]string
	LastModified time.Time
	ETag         string
}

// ExtractedMetadata 是文件元数据提取结果。
type ExtractedMetadata struct {
	BasicInfo   BasicMetadata
	SchemaInfo  *SchemaMetadata
	ContentData interface{}
	CustomAttrs map[string]interface{}
}

// BasicMetadata 是跨格式通用的基础文件元数据。
type BasicMetadata struct {
	FileName     string
	FileType     string
	Size         int64
	ContentType  string
	Encoding     string
	LastModified time.Time
	Checksum     string
	ETag         string
}

// SchemaMetadata 是结构化文件的轻量 schema 元数据。
type SchemaMetadata struct {
	Columns    []ColumnMetadata
	RowCount   int64
	SampleData []map[string]interface{}
	Extra      map[string]interface{}
}

// ColumnMetadata 是增强元数据提取使用的列描述。
type ColumnMetadata struct {
	Name        string
	Type        string
	Nullable    bool
	Description string
	Example     interface{}
}

// ExtractorRegistry 维护按 MIME 类型注册的文件元数据提取器。
type ExtractorRegistry struct {
	extractors map[string][]FileMetadataExtractor
	mu         sync.RWMutex
}

var globalExtractorRegistry = NewExtractorRegistry()

func NewExtractorRegistry() *ExtractorRegistry {
	return &ExtractorRegistry{
		extractors: make(map[string][]FileMetadataExtractor),
	}
}

func RegisterExtractor(extractor FileMetadataExtractor) error {
	return globalExtractorRegistry.Register(extractor)
}

func (r *ExtractorRegistry) Register(extractor FileMetadataExtractor) error {
	if extractor == nil {
		return fmt.Errorf("extractor cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, mimeType := range extractor.SupportedTypes() {
		mimeType = strings.ToLower(strings.TrimSpace(mimeType))
		r.extractors[mimeType] = append(r.extractors[mimeType], extractor)
	}

	for _, extractors := range r.extractors {
		sort.Slice(extractors, func(i, j int) bool {
			return extractors[i].Priority() > extractors[j].Priority()
		})
	}

	return nil
}

func GetExtractor(contentType string) FileMetadataExtractor {
	return globalExtractorRegistry.GetExtractor(contentType)
}

func (r *ExtractorRegistry) GetExtractor(contentType string) FileMetadataExtractor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if extractors := r.extractors[contentType]; len(extractors) > 0 {
		return extractors[0]
	}

	if parts := strings.Split(contentType, "/"); len(parts) == 2 {
		wildcardType := parts[0] + "/*"
		if extractors := r.extractors[wildcardType]; len(extractors) > 0 {
			return extractors[0]
		}
	}

	if extractors := r.extractors["*/*"]; len(extractors) > 0 {
		return extractors[0]
	}

	return nil
}

func ListRegisteredExtractorTypes() []string {
	return globalExtractorRegistry.ListTypes()
}

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
