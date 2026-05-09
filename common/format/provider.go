package format

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
)

// Provider 是格式层能力实现的基础接口。
//
// Provider 不接 engine id，不创建资源读取器，也不返回上层模块专用 DTO。
// 调用方应先基于 engine capability 构造读取抽象或内容流，再交给 Provider 解码。
type Provider interface {
	Format() FormatType
	Capabilities() FormatCapability
}

// TableProvider 表示格式能够从外部提供的资源流中提取 table 语义。
type TableProvider interface {
	Provider
	DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableInfo, error)
	SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

type tableParserProvider struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*TableInfo, error)
	sample     func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error)
}

func (p tableParserProvider) Format() FormatType {
	return p.formatType
}

func (p tableParserProvider) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:        p.formatType,
		DataType:      FormatDataTypeTable,
		Layouts:       []string{FormatLayoutSingle},
		ProviderHints: []string{FormatProviderTable},
		Parse:         true,
		Preview:       true,
	}
}

func (p tableParserProvider) DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableInfo, error) {
	return p.describe(ctx, input, options)
}

func (p tableParserProvider) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error) {
	return p.sample(ctx, input, offset, limit, options)
}

type ProviderRegistry struct {
	mu             sync.RWMutex
	tableProviders map[FormatType]TableProvider
}

var globalProviderRegistry = NewProviderRegistry()

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		tableProviders: make(map[FormatType]TableProvider),
	}
}

func RegisterTableProvider(provider TableProvider) error {
	return globalProviderRegistry.RegisterTableProvider(provider)
}

func (r *ProviderRegistry) RegisterTableProvider(provider TableProvider) error {
	if provider == nil {
		return fmt.Errorf("table provider cannot be nil")
	}
	formatType := provider.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("table provider must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tableProviders[formatType] = provider
	return nil
}

func GetTableProvider(formatType FormatType) (TableProvider, error) {
	return globalProviderRegistry.GetTableProvider(formatType)
}

func (r *ProviderRegistry) GetTableProvider(formatType FormatType) (TableProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.tableProviders[formatType]
	if !ok {
		return nil, fmt.Errorf("no table provider registered for format: %s", formatType)
	}
	return provider, nil
}

func ListTableProviderFormats() []FormatType {
	return globalProviderRegistry.ListTableProviderFormats()
}

func (r *ProviderRegistry) ListTableProviderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.tableProviders))
	for formatType := range r.tableProviders {
		formats = append(formats, formatType)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i] < formats[j]
	})
	return formats
}

func NewTableProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*TableInfo, error),
	sample func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error),
) TableProvider {
	return tableParserProvider{
		formatType: formatType,
		describe:   describe,
		sample:     sample,
	}
}
