package format

import (
	"fmt"
	"sync"
)

// Registry 是解析器的全局注册中心
// 支持四种类型的解析器：
//  1. FileTableParser（文件表解析器）
//  2. DBTableParser（数据库表解析器）
//  3. ObjectInfoParser（对象信息解析器）
//  4. DocCollectionParser（文档集合解析器）
type Registry struct {
	mu sync.RWMutex

	// 新接口
	fileTableParsers      map[FormatType]FileTableParser  // key: FormatType (e.g., FormatCSV, FormatShapefile)
	dbTableParsers        map[string]DBTableParser        // key: engine type (e.g., "postgresql", "mysql")
	objectInfoParsers     map[string]ObjectInfoParser     // key: content type (e.g., "image/jpeg", "application/pdf")
	docCollectionParsers  map[string]DocCollectionParser  // key: engine type (e.g., "mongodb", "couchdb")
}

var (
	globalRegistry = NewRegistry()
)

// NewRegistry 创建新的解析器注册中心
func NewRegistry() *Registry {
	return &Registry{
		fileTableParsers:     make(map[FormatType]FileTableParser),
		dbTableParsers:       make(map[string]DBTableParser),
		objectInfoParsers:    make(map[string]ObjectInfoParser),
		docCollectionParsers: make(map[string]DocCollectionParser),
	}
}

// ============ FileTableParser 注册 ============

// RegisterFileTableParser 注册文件表解析器到全局注册中心
func RegisterFileTableParser(parser FileTableParser) error {
	return globalRegistry.RegisterFileParser(parser)
}

// RegisterFileParser 注册文件表解析器
func (r *Registry) RegisterFileParser(parser FileTableParser) error {
	if parser == nil {
		return fmt.Errorf("file table parser cannot be nil")
	}

	formats := parser.SupportedFormats()
	if len(formats) == 0 {
		return fmt.Errorf("file table parser must support at least one format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, formatType := range formats {
		r.fileTableParsers[formatType] = parser
	}

	return nil
}

// GetFileTableParser 获取指定格式的文件表解析器
func GetFileTableParser(formatType FormatType) (FileTableParser, error) {
	return globalRegistry.GetFileParser(formatType)
}

// GetFileParser 获取指定格式的文件表解析器
func (r *Registry) GetFileParser(formatType FormatType) (FileTableParser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.fileTableParsers[formatType]
	if !ok {
		return nil, fmt.Errorf("no file table parser registered for format: %s", formatType)
	}

	return parser, nil
}

// ListSupportedFileFormats 列出所有已注册的文件格式
func ListSupportedFileFormats() []FormatType {
	return globalRegistry.ListFileFormats()
}

// ListFileFormats 列出所有已注册的文件格式
func (r *Registry) ListFileFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.fileTableParsers))
	for formatType := range r.fileTableParsers {
		formats = append(formats, formatType)
	}

	return formats
}

// ============ DBTableParser 注册 ============

// RegisterDBTableParser 注册数据库表解析器到全局注册中心
func RegisterDBTableParser(parser DBTableParser) error {
	return globalRegistry.RegisterDBParser(parser)
}

// RegisterDBParser 注册数据库表解析器
func (r *Registry) RegisterDBParser(parser DBTableParser) error {
	if parser == nil {
		return fmt.Errorf("db table parser cannot be nil")
	}

	engineTypes := parser.SupportedEngineTypes()
	if len(engineTypes) == 0 {
		return fmt.Errorf("db table parser must support at least one engine type")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, engineType := range engineTypes {
		r.dbTableParsers[engineType] = parser
	}

	return nil
}

// GetDBTableParser 获取指定引擎类型的数据库表解析器
func GetDBTableParser(engineType string) (DBTableParser, error) {
	return globalRegistry.GetDBParser(engineType)
}

// GetDBParser 获取指定引擎类型的数据库表解析器
func (r *Registry) GetDBParser(engineType string) (DBTableParser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.dbTableParsers[engineType]
	if !ok {
		return nil, fmt.Errorf("no db table parser registered for engine type: %s", engineType)
	}

	return parser, nil
}

// ListSupportedEngineTypes 列出所有已注册的引擎类型
func ListSupportedEngineTypes() []string {
	return globalRegistry.ListEngineTypes()
}

// ListEngineTypes 列出所有已注册的引擎类型
func (r *Registry) ListEngineTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.dbTableParsers))
	for engineType := range r.dbTableParsers {
		types = append(types, engineType)
	}

	return types
}

// ============ ObjectInfoParser 注册 ============

// RegisterObjectInfoParser 注册对象信息解析器到全局注册中心
func RegisterObjectInfoParser(parser ObjectInfoParser) error {
	return globalRegistry.RegisterObjectParser(parser)
}

// RegisterObjectParser 注册对象信息解析器
func (r *Registry) RegisterObjectParser(parser ObjectInfoParser) error {
	if parser == nil {
		return fmt.Errorf("object info parser cannot be nil")
	}

	contentTypes := parser.SupportedContentTypes()
	if len(contentTypes) == 0 {
		return fmt.Errorf("object info parser must support at least one content type")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, contentType := range contentTypes {
		r.objectInfoParsers[contentType] = parser
	}

	return nil
}

// GetObjectInfoParser 获取指定内容类型的对象信息解析器
func GetObjectInfoParser(contentType string) (ObjectInfoParser, error) {
	return globalRegistry.GetObjectParser(contentType)
}

// GetObjectParser 获取指定内容类型的对象信息解析器
func (r *Registry) GetObjectParser(contentType string) (ObjectInfoParser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.objectInfoParsers[contentType]
	if !ok {
		return nil, fmt.Errorf("no object info parser registered for content type: %s", contentType)
	}

	return parser, nil
}

// ListSupportedContentTypes 列出所有已注册的内容类型
func ListSupportedContentTypes() []string {
	return globalRegistry.ListContentTypes()
}

// ListContentTypes 列出所有已注册的内容类型
func (r *Registry) ListContentTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.objectInfoParsers))
	for contentType := range r.objectInfoParsers {
		types = append(types, contentType)
	}

	return types
}

// ============ DocCollectionParser 注册 ============

// RegisterDocCollectionParser 注册文档集合解析器到全局注册中心
func RegisterDocCollectionParser(parser DocCollectionParser) error {
	return globalRegistry.RegisterDocParser(parser)
}

// RegisterDocParser 注册文档集合解析器
func (r *Registry) RegisterDocParser(parser DocCollectionParser) error {
	if parser == nil {
		return fmt.Errorf("doc collection parser cannot be nil")
	}

	engineTypes := parser.SupportedEngineTypes()
	if len(engineTypes) == 0 {
		return fmt.Errorf("doc collection parser must support at least one engine type")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, engineType := range engineTypes {
		r.docCollectionParsers[engineType] = parser
	}

	return nil
}

// GetDocCollectionParser 获取指定引擎类型的文档集合解析器
func GetDocCollectionParser(engineType string) (DocCollectionParser, error) {
	return globalRegistry.GetDocParser(engineType)
}

// GetDocParser 获取指定引擎类型的文档集合解析器
func (r *Registry) GetDocParser(engineType string) (DocCollectionParser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.docCollectionParsers[engineType]
	if !ok {
		return nil, fmt.Errorf("no doc collection parser registered for engine type: %s", engineType)
	}

	return parser, nil
}

// ListSupportedDocEngineTypes 列出所有已注册的文档集合引擎类型
func ListSupportedDocEngineTypes() []string {
	return globalRegistry.ListDocEngineTypes()
}

// ListDocEngineTypes 列出所有已注册的文档集合引擎类型
func (r *Registry) ListDocEngineTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.docCollectionParsers))
	for engineType := range r.docCollectionParsers {
		types = append(types, engineType)
	}

	return types
}

