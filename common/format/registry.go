package format

import (
	"context"
	"fmt"
	"sync"
)

// Registry 是解析器的全局注册中心
// 支持三种类型的解析器：
//  1. Parser（旧接口，向后兼容）
//  2. FileTableParser（文件表解析器）
//  3. DBTableParser（数据库表解析器）
type Registry struct {
	mu sync.RWMutex

	// 旧接口（向后兼容）
	parsers map[FormatType]Parser

	// 新接口
	fileTableParsers map[FormatType]FileTableParser // key: FormatType (e.g., FormatCSV, FormatShapefile)
	dbTableParsers   map[string]DBTableParser       // key: engine type (e.g., "postgresql", "mysql")
}

var (
	globalRegistry = NewRegistry()
)

// NewRegistry 创建新的解析器注册中心
func NewRegistry() *Registry {
	return &Registry{
		parsers:          make(map[FormatType]Parser),
		fileTableParsers: make(map[FormatType]FileTableParser),
		dbTableParsers:   make(map[string]DBTableParser),
	}
}

// ============ 旧接口（向后兼容）============

// Register 注册解析器到全局注册中心
// @Deprecated: 使用 RegisterFileTableParser 或 RegisterDBTableParser
func Register(parser Parser) error {
	return globalRegistry.RegisterParser(parser)
}

// RegisterParser 注册解析器
// @Deprecated: 使用 RegisterFileTableParser 或 RegisterDBTableParser
func (r *Registry) RegisterParser(parser Parser) error {
	if parser == nil {
		return fmt.Errorf("parser cannot be nil")
	}

	formats := parser.SupportedFormats()
	if len(formats) == 0 {
		return fmt.Errorf("parser must support at least one format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, formatType := range formats {
		r.parsers[formatType] = parser
	}

	return nil
}

// GetParser 获取指定格式的解析器
// @Deprecated: 使用 GetFileTableParser
func GetParser(formatType FormatType) (Parser, error) {
	return globalRegistry.Get(formatType)
}

// Get 获取指定格式的解析器
// @Deprecated: 使用 GetFileTableParser
func (r *Registry) Get(formatType FormatType) (Parser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.parsers[formatType]
	if !ok {
		return nil, fmt.Errorf("no parser registered for format: %s", formatType)
	}

	return parser, nil
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

// ============ 便捷函数（向后兼容）============

// ListSupportedFormats 列出所有已注册的格式（旧接口）
// @Deprecated: 使用 ListSupportedFileFormats
func ListSupportedFormats() []FormatType {
	return globalRegistry.ListFormats()
}

// ListFormats 列出所有已注册的格式（旧接口）
// @Deprecated: 使用 ListFileFormats
func (r *Registry) ListFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.parsers))
	for formatType := range r.parsers {
		formats = append(formats, formatType)
	}

	return formats
}

// ParseSchema 使用注册的解析器解析 Schema（旧接口）
// @Deprecated: 使用 FileTableParser.ParseTableInfo() 替代
func ParseSchema(ctx context.Context, formatType FormatType, input interface{}) (*Schema, error) {
	_, err := GetParser(formatType)
	if err != nil {
		return nil, err
	}

	// TODO: 根据 input 类型转换为 io.Reader
	// 这里简化处理，实际使用时各模块会直接调用特定解析器

	return nil, fmt.Errorf("not implemented: use specific parser directly")
}

