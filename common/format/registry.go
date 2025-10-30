package format

import (
	"context"
	"fmt"
	"sync"
)

// Registry 是解析器的全局注册中心
type Registry struct {
	mu      sync.RWMutex
	parsers map[FormatType]Parser
}

var (
	globalRegistry = NewRegistry()
)

// NewRegistry 创建新的解析器注册中心
func NewRegistry() *Registry {
	return &Registry{
		parsers: make(map[FormatType]Parser),
	}
}

// Register 注册解析器到全局注册中心
func Register(parser Parser) error {
	return globalRegistry.RegisterParser(parser)
}

// RegisterParser 注册解析器
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
func GetParser(formatType FormatType) (Parser, error) {
	return globalRegistry.Get(formatType)
}

// Get 获取指定格式的解析器
func (r *Registry) Get(formatType FormatType) (Parser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.parsers[formatType]
	if !ok {
		return nil, fmt.Errorf("no parser registered for format: %s", formatType)
	}

	return parser, nil
}

// ListSupportedFormats 列出所有已注册的格式
func ListSupportedFormats() []FormatType {
	return globalRegistry.ListFormats()
}

// ListFormats 列出所有已注册的格式
func (r *Registry) ListFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.parsers))
	for formatType := range r.parsers {
		formats = append(formats, formatType)
	}

	return formats
}

// ParseSchema 使用注册的解析器解析 Schema
// 这是一个便捷函数，自动选择合适的解析器
func ParseSchema(ctx context.Context, formatType FormatType, input interface{}) (*Schema, error) {
	_, err := GetParser(formatType)
	if err != nil {
		return nil, err
	}

	// TODO: 根据 input 类型转换为 io.Reader
	// 这里简化处理，实际使用时各模块会直接调用特定解析器

	return nil, fmt.Errorf("not implemented: use specific parser directly")
}
