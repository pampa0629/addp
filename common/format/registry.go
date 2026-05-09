package format

import (
	"fmt"
	"sync"
)

// Registry 是解析器的全局注册中心。
// 这里只保留 engine-native 和 document collection 的历史解析入口；
// 文件表能力统一走 TableProvider。
type Registry struct {
	mu sync.RWMutex

	dbTableParsers       map[string]DBTableParser       // key: engine type (e.g., "postgresql", "mysql")
	docCollectionParsers map[string]DocCollectionParser // key: engine type (e.g., "mongodb", "couchdb")
}

var (
	globalRegistry = NewRegistry()
)

// NewRegistry 创建新的解析器注册中心
func NewRegistry() *Registry {
	return &Registry{
		dbTableParsers:       make(map[string]DBTableParser),
		docCollectionParsers: make(map[string]DocCollectionParser),
	}
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
