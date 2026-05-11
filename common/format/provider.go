package format

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/addp/common/resource"
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

type DocumentInfo struct {
	Format      FormatType
	Title       string
	Language    string
	Encoding    string
	SizeBytes   *int64
	TextPreview string
	Truncated   bool
}

type MediaInfo struct {
	Format       FormatType
	MediaType    string
	MIMEType     string
	Width        int
	Height       int
	DurationMS   *int64
	Encoding     string
	ColorSpace   string
	SizeBytes    *int64
	PreviewKind  string
	SpatialAttrs map[string]interface{}
}

type DocumentProvider interface {
	Provider
	DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*DocumentInfo, error)
	ExtractText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error)
}

type MediaProvider interface {
	Provider
	DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*MediaInfo, error)
}

type ComponentTableProvider interface {
	TableProvider
	DescribeTableComponents(ctx context.Context, components resource.ComponentReader, options *ParseOptions) (*TableInfo, error)
	SampleTableComponents(ctx context.Context, components resource.ComponentReader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

type ScopeTableProvider interface {
	TableProvider
	DescribeTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, options *ParseOptions) (*TableInfo, error)
	SampleTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

type tableProviderAdapter struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*TableInfo, error)
	sample     func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error)
}

type documentProviderAdapter struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error)
	extract    func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error)
}

type mediaProviderAdapter struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*MediaInfo, error)
}

func (p tableProviderAdapter) Format() FormatType {
	return p.formatType
}

func (p tableProviderAdapter) Capabilities() FormatCapability {
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

func (p tableProviderAdapter) DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableInfo, error) {
	return p.describe(ctx, input, options)
}

func (p tableProviderAdapter) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error) {
	return p.sample(ctx, input, offset, limit, options)
}

func (p documentProviderAdapter) Format() FormatType {
	return p.formatType
}

func (p documentProviderAdapter) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:        p.formatType,
		DataType:      FormatDataTypeDocument,
		Layouts:       []string{FormatLayoutSingle},
		ProviderHints: []string{FormatProviderDocument},
		Preview:       true,
	}
}

func (p documentProviderAdapter) DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*DocumentInfo, error) {
	return p.describe(ctx, input, options)
}

func (p documentProviderAdapter) ExtractText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error) {
	return p.extract(ctx, input, limit, options)
}

func (p mediaProviderAdapter) Format() FormatType {
	return p.formatType
}

func (p mediaProviderAdapter) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:        p.formatType,
		DataType:      FormatDataTypeMedia,
		Layouts:       []string{FormatLayoutSingle},
		ProviderHints: []string{FormatProviderMedia},
		Preview:       true,
	}
}

func (p mediaProviderAdapter) DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*MediaInfo, error) {
	return p.describe(ctx, input, options)
}

type ProviderRegistry struct {
	mu                sync.RWMutex
	tableProviders    map[FormatType]TableProvider
	documentProviders map[FormatType]DocumentProvider
	mediaProviders    map[FormatType]MediaProvider
}

var globalProviderRegistry = NewProviderRegistry()

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		tableProviders:    make(map[FormatType]TableProvider),
		documentProviders: make(map[FormatType]DocumentProvider),
		mediaProviders:    make(map[FormatType]MediaProvider),
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

func RegisterDocumentProvider(provider DocumentProvider) error {
	return globalProviderRegistry.RegisterDocumentProvider(provider)
}

func (r *ProviderRegistry) RegisterDocumentProvider(provider DocumentProvider) error {
	if provider == nil {
		return fmt.Errorf("document provider cannot be nil")
	}
	formatType := provider.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("document provider must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.documentProviders[formatType] = provider
	return nil
}

func RegisterMediaProvider(provider MediaProvider) error {
	return globalProviderRegistry.RegisterMediaProvider(provider)
}

func (r *ProviderRegistry) RegisterMediaProvider(provider MediaProvider) error {
	if provider == nil {
		return fmt.Errorf("media provider cannot be nil")
	}
	formatType := provider.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("media provider must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.mediaProviders[formatType] = provider
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

func GetDocumentProvider(formatType FormatType) (DocumentProvider, error) {
	return globalProviderRegistry.GetDocumentProvider(formatType)
}

func (r *ProviderRegistry) GetDocumentProvider(formatType FormatType) (DocumentProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.documentProviders[formatType]
	if !ok {
		return nil, fmt.Errorf("no document provider registered for format: %s", formatType)
	}
	return provider, nil
}

func GetMediaProvider(formatType FormatType) (MediaProvider, error) {
	return globalProviderRegistry.GetMediaProvider(formatType)
}

func (r *ProviderRegistry) GetMediaProvider(formatType FormatType) (MediaProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.mediaProviders[formatType]
	if !ok {
		return nil, fmt.Errorf("no media provider registered for format: %s", formatType)
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

func ListDocumentProviderFormats() []FormatType {
	return globalProviderRegistry.ListDocumentProviderFormats()
}

func (r *ProviderRegistry) ListDocumentProviderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.documentProviders))
	for formatType := range r.documentProviders {
		formats = append(formats, formatType)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i] < formats[j]
	})
	return formats
}

func ListMediaProviderFormats() []FormatType {
	return globalProviderRegistry.ListMediaProviderFormats()
}

func (r *ProviderRegistry) ListMediaProviderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.mediaProviders))
	for formatType := range r.mediaProviders {
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
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*TableInfo, error) {
			return nil, fmt.Errorf("table provider %s does not implement DescribeTable", formatType)
		}
	}
	if sample == nil {
		sample = func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
			return nil, fmt.Errorf("table provider %s does not implement SampleTable", formatType)
		}
	}
	return tableProviderAdapter{
		formatType: formatType,
		describe:   describe,
		sample:     sample,
	}
}

func NewMediaProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*MediaInfo, error),
) MediaProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*MediaInfo, error) {
			return nil, fmt.Errorf("media provider %s does not implement DescribeMedia", formatType)
		}
	}
	return mediaProviderAdapter{
		formatType: formatType,
		describe:   describe,
	}
}

func NewDocumentProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error),
	extract func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error),
) DocumentProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error) {
			return nil, fmt.Errorf("document provider %s does not implement DescribeDocument", formatType)
		}
	}
	if extract == nil {
		extract = func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error) {
			return "", false, fmt.Errorf("document provider %s does not implement ExtractText", formatType)
		}
	}
	return documentProviderAdapter{
		formatType: formatType,
		describe:   describe,
		extract:    extract,
	}
}
