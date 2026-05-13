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

// FormatPlugin 表示一个格式实现的稳定身份入口。
//
// Plugin 只声明格式身份、descriptor 和 capability，不负责识别某个资源、
// 不决定 data item 边界，也不替代具体 InfoProvider / ContentReader。
type FormatPlugin interface {
	Format() FormatType
	Descriptor() FormatDescriptor
	Capabilities() FormatCapability
}

// FormatInfoProvider 表示格式能够提供自身私有元数据。
//
// 结果应写入 attributes.format_info.<format>，不得混入 type_info 或上层模块 DTO。
type FormatInfoProvider interface {
	Provider
	DescribeFormat(ctx context.Context, input io.Reader, options *ParseOptions) (map[string]interface{}, error)
}

// TableInfoProvider 表示格式能够从外部提供的资源流中提取 table 类型信息。
//
// 类型信息是 meta 层写入 type_info.table 的主来源，不应夹带 Manager 展示 DTO。
type TableInfoProvider interface {
	Provider
	DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableInfo, error)
}

// ContentReader 是内容读取能力的标记接口。
//
// Reader 命名用于区分“读取内容数据”和“提供元数据”的 provider。
type ContentReader interface {
	Provider
}

// TableSampleReader 表示格式能够从外部提供的资源流中读取 table 样本数据。
//
// 样本数据是面向 Manager 内容查看、Transfer 探查等消费场景的数据能力，
// 和 TableInfoProvider 并列注册，避免把“类型信息”和“内容数据”绑死。
// offset / limit 是逻辑数据行窗口，不是字节偏移。input 默认从资源起点开始；
// 如果调用方基于 content_index 传入局部流，必须通过 ParseOptions.TableSample
// 提供定位上下文。
type TableSampleReader interface {
	ContentReader
	SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

// TableSampleProvider 是旧命名兼容别名，新代码应使用 TableSampleReader。
type TableSampleProvider = TableSampleReader

// TableProvider 是兼容旧调用方的组合接口。
//
// 新代码优先按 TableInfoProvider / TableSampleReader 分别表达调用意图；
// 已有 plugin 同时实现两者时仍可注册为 TableProvider。
type TableProvider interface {
	TableInfoProvider
	TableSampleReader
}

type DocumentInfo struct {
	Format    FormatType
	Title     string
	Language  string
	Encoding  string
	SizeBytes *int64
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
	SpatialAttrs map[string]interface{}
}

// ContainerInfoProvider 表示格式能够提供容器内部对象信息。
//
// 结果应由 Meta 映射到 type_info.container 和 format_info.<format>；
// provider 不决定 child 是否成为独立 data item，也不返回上层展示 DTO。
type ContainerInfoProvider interface {
	Provider
	DescribeContainer(ctx context.Context, input io.Reader, options *ParseOptions) (*ContainerInfo, error)
}

type DocumentInfoProvider interface {
	Provider
	DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*DocumentInfo, error)
}

type DocumentTextReader interface {
	ContentReader
	ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error)
}

// DocumentProvider 是兼容期组合接口，等价于同时具备 DocumentInfoProvider 和 DocumentTextReader。
type DocumentProvider interface {
	DocumentInfoProvider
	DocumentTextReader
}

type MediaInfoProvider interface {
	Provider
	DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*MediaInfo, error)
}

// MediaProvider 是旧命名兼容别名，新代码应使用 MediaInfoProvider。
type MediaProvider = MediaInfoProvider

type ComponentTableProvider interface {
	TableProvider
	DescribeTableComponents(ctx context.Context, components resource.ComponentReader, options *ParseOptions) (*TableInfo, error)
	SampleTableComponents(ctx context.Context, components resource.ComponentReader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

// ComponentSpecProvider 表示格式能够声明多组件资源的组件规格。
//
// 该接口只描述组件角色和必需性，调用方仍负责 data item 边界识别、
// 资源路径发现与 ResourceReader 构造。
type ComponentSpecProvider interface {
	Provider
	ComponentSpecs() []resource.ComponentSpec
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

type formatInfoProviderAdapter struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error)
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

type containerProviderAdapter struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*ContainerInfo, error)
}

func (p tableProviderAdapter) Format() FormatType {
	return p.formatType
}

func (p tableProviderAdapter) Descriptor() FormatDescriptor {
	descriptor, ok := GetFormatDescriptor(p.formatType)
	if ok {
		return descriptor
	}
	return FormatDescriptor{
		ID:       "inline-" + string(p.formatType),
		Format:   p.formatType,
		DataType: FormatDataTypeTable,
		Layouts:  []string{FormatLayoutSingle},
	}
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
	}
}

func (p formatInfoProviderAdapter) Format() FormatType {
	return p.formatType
}

func (p formatInfoProviderAdapter) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:  p.formatType,
		Layouts: []string{FormatLayoutSingle},
	}
}

func (p formatInfoProviderAdapter) DescribeFormat(ctx context.Context, input io.Reader, options *ParseOptions) (map[string]interface{}, error) {
	return p.describe(ctx, input, options)
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
	}
}

func (p documentProviderAdapter) DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*DocumentInfo, error) {
	return p.describe(ctx, input, options)
}

func (p documentProviderAdapter) ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error) {
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
	}
}

func (p mediaProviderAdapter) DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*MediaInfo, error) {
	return p.describe(ctx, input, options)
}

func (p containerProviderAdapter) Format() FormatType {
	return p.formatType
}

func (p containerProviderAdapter) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:        p.formatType,
		DataType:      FormatDataTypeContainer,
		Layouts:       []string{FormatLayoutSingle},
		ProviderHints: []string{FormatProviderContainer},
	}
}

func (p containerProviderAdapter) DescribeContainer(ctx context.Context, input io.Reader, options *ParseOptions) (*ContainerInfo, error) {
	return p.describe(ctx, input, options)
}

type ProviderRegistry struct {
	mu                     sync.RWMutex
	formatPlugins          map[FormatType]FormatPlugin
	tableProviders         map[FormatType]TableProvider
	formatInfoProviders    map[FormatType]FormatInfoProvider
	tableInfoProviders     map[FormatType]TableInfoProvider
	tableSampleProviders   map[FormatType]TableSampleProvider
	documentProviders      map[FormatType]DocumentProvider
	documentInfoProviders  map[FormatType]DocumentInfoProvider
	documentTextReaders    map[FormatType]DocumentTextReader
	mediaInfoProviders     map[FormatType]MediaInfoProvider
	containerInfoProviders map[FormatType]ContainerInfoProvider
}

var globalProviderRegistry = NewProviderRegistry()

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		formatPlugins:          make(map[FormatType]FormatPlugin),
		tableProviders:         make(map[FormatType]TableProvider),
		formatInfoProviders:    make(map[FormatType]FormatInfoProvider),
		tableInfoProviders:     make(map[FormatType]TableInfoProvider),
		tableSampleProviders:   make(map[FormatType]TableSampleProvider),
		documentProviders:      make(map[FormatType]DocumentProvider),
		documentInfoProviders:  make(map[FormatType]DocumentInfoProvider),
		documentTextReaders:    make(map[FormatType]DocumentTextReader),
		mediaInfoProviders:     make(map[FormatType]MediaInfoProvider),
		containerInfoProviders: make(map[FormatType]ContainerInfoProvider),
	}
}

func RegisterFormatPlugin(plugin FormatPlugin) error {
	return globalProviderRegistry.RegisterFormatPlugin(plugin)
}

func (r *ProviderRegistry) RegisterFormatPlugin(plugin FormatPlugin) error {
	if plugin == nil {
		return fmt.Errorf("format plugin cannot be nil")
	}
	formatType := plugin.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("format plugin must define format")
	}
	descriptor := plugin.Descriptor()
	if descriptor.Format == "" {
		descriptor.Format = formatType
	}
	if descriptor.Format != formatType {
		return fmt.Errorf("format plugin descriptor format %s does not match plugin format %s", descriptor.Format, formatType)
	}
	if descriptor.ID != "" && descriptor.DataType != "" && shouldRegisterPluginDescriptor(formatType, descriptor) {
		if err := RegisterFormatDescriptor(descriptor); err != nil {
			return err
		}
	}

	r.mu.Lock()
	r.formatPlugins[formatType] = plugin
	r.mu.Unlock()

	if provider, ok := plugin.(FormatInfoProvider); ok {
		if err := r.RegisterFormatInfoProvider(provider); err != nil {
			return err
		}
	}
	if provider, ok := plugin.(TableProvider); ok {
		if err := r.RegisterTableProvider(provider); err != nil {
			return err
		}
	} else {
		if provider, ok := plugin.(TableInfoProvider); ok {
			if err := r.RegisterTableInfoProvider(provider); err != nil {
				return err
			}
		}
		if reader, ok := plugin.(TableSampleProvider); ok {
			if err := r.RegisterTableSampleProvider(reader); err != nil {
				return err
			}
		}
	}
	if provider, ok := plugin.(DocumentProvider); ok {
		if err := r.RegisterDocumentProvider(provider); err != nil {
			return err
		}
	} else {
		if provider, ok := plugin.(DocumentInfoProvider); ok {
			if err := r.RegisterDocumentInfoProvider(provider); err != nil {
				return err
			}
		}
		if reader, ok := plugin.(DocumentTextReader); ok {
			if err := r.RegisterDocumentTextReader(reader); err != nil {
				return err
			}
		}
	}
	if provider, ok := plugin.(MediaInfoProvider); ok {
		if err := r.RegisterMediaInfoProvider(provider); err != nil {
			return err
		}
	}
	if provider, ok := plugin.(ContainerInfoProvider); ok {
		if err := r.RegisterContainerInfoProvider(provider); err != nil {
			return err
		}
	}
	return nil
}

func shouldRegisterPluginDescriptor(formatType FormatType, descriptor FormatDescriptor) bool {
	_, ok := GetFormatDescriptor(formatType)
	return !ok
}

func RegisterFormatInfoProvider(provider FormatInfoProvider) error {
	return globalProviderRegistry.RegisterFormatInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterFormatInfoProvider(provider FormatInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("format info provider cannot be nil")
	}
	formatType := provider.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("format info provider must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.formatInfoProviders[formatType] = provider
	return nil
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
	r.tableInfoProviders[formatType] = provider
	r.tableSampleProviders[formatType] = provider
	return nil
}

func RegisterTableInfoProvider(provider TableInfoProvider) error {
	return globalProviderRegistry.RegisterTableInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterTableInfoProvider(provider TableInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("table info provider cannot be nil")
	}
	formatType := provider.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("table info provider must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tableInfoProviders[formatType] = provider
	return nil
}

func RegisterTableSampleProvider(provider TableSampleProvider) error {
	return globalProviderRegistry.RegisterTableSampleProvider(provider)
}

func (r *ProviderRegistry) RegisterTableSampleProvider(provider TableSampleProvider) error {
	if provider == nil {
		return fmt.Errorf("table sample provider cannot be nil")
	}
	formatType := provider.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("table sample provider must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tableSampleProviders[formatType] = provider
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
	r.documentInfoProviders[formatType] = provider
	r.documentTextReaders[formatType] = provider
	return nil
}

func RegisterDocumentInfoProvider(provider DocumentInfoProvider) error {
	return globalProviderRegistry.RegisterDocumentInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterDocumentInfoProvider(provider DocumentInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("document info provider cannot be nil")
	}
	formatType := provider.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("document info provider must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.documentInfoProviders[formatType] = provider
	return nil
}

func RegisterDocumentTextReader(reader DocumentTextReader) error {
	return globalProviderRegistry.RegisterDocumentTextReader(reader)
}

func (r *ProviderRegistry) RegisterDocumentTextReader(reader DocumentTextReader) error {
	if reader == nil {
		return fmt.Errorf("document text reader cannot be nil")
	}
	formatType := reader.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("document text reader must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.documentTextReaders[formatType] = reader
	return nil
}

func RegisterMediaProvider(provider MediaProvider) error {
	return globalProviderRegistry.RegisterMediaProvider(provider)
}

func (r *ProviderRegistry) RegisterMediaProvider(provider MediaProvider) error {
	return r.RegisterMediaInfoProvider(provider)
}

func RegisterMediaInfoProvider(provider MediaInfoProvider) error {
	return globalProviderRegistry.RegisterMediaInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterMediaInfoProvider(provider MediaInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("media info provider cannot be nil")
	}
	formatType := provider.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("media info provider must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.mediaInfoProviders[formatType] = provider
	return nil
}

func RegisterContainerInfoProvider(provider ContainerInfoProvider) error {
	return globalProviderRegistry.RegisterContainerInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterContainerInfoProvider(provider ContainerInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("container info provider cannot be nil")
	}
	formatType := provider.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("container info provider must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.containerInfoProviders[formatType] = provider
	return nil
}

func GetFormatPlugin(formatType FormatType) (FormatPlugin, error) {
	return globalProviderRegistry.GetFormatPlugin(formatType)
}

func (r *ProviderRegistry) GetFormatPlugin(formatType FormatType) (FormatPlugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, ok := r.formatPlugins[formatType]
	if !ok {
		return nil, fmt.Errorf("no format plugin registered for format: %s", formatType)
	}
	return plugin, nil
}

func GetFormatInfoProvider(formatType FormatType) (FormatInfoProvider, error) {
	return globalProviderRegistry.GetFormatInfoProvider(formatType)
}

func (r *ProviderRegistry) GetFormatInfoProvider(formatType FormatType) (FormatInfoProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.formatInfoProviders[formatType]
	if !ok {
		return nil, fmt.Errorf("no format info provider registered for format: %s", formatType)
	}
	return provider, nil
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

func GetTableInfoProvider(formatType FormatType) (TableInfoProvider, error) {
	return globalProviderRegistry.GetTableInfoProvider(formatType)
}

func (r *ProviderRegistry) GetTableInfoProvider(formatType FormatType) (TableInfoProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.tableInfoProviders[formatType]
	if !ok {
		return nil, fmt.Errorf("no table info provider registered for format: %s", formatType)
	}
	return provider, nil
}

func GetTableSampleProvider(formatType FormatType) (TableSampleProvider, error) {
	return globalProviderRegistry.GetTableSampleProvider(formatType)
}

func (r *ProviderRegistry) GetTableSampleProvider(formatType FormatType) (TableSampleProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.tableSampleProviders[formatType]
	if !ok {
		return nil, fmt.Errorf("no table sample provider registered for format: %s", formatType)
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

func GetDocumentInfoProvider(formatType FormatType) (DocumentInfoProvider, error) {
	return globalProviderRegistry.GetDocumentInfoProvider(formatType)
}

func (r *ProviderRegistry) GetDocumentInfoProvider(formatType FormatType) (DocumentInfoProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.documentInfoProviders[formatType]
	if !ok {
		return nil, fmt.Errorf("no document info provider registered for format: %s", formatType)
	}
	return provider, nil
}

func GetDocumentTextReader(formatType FormatType) (DocumentTextReader, error) {
	return globalProviderRegistry.GetDocumentTextReader(formatType)
}

func (r *ProviderRegistry) GetDocumentTextReader(formatType FormatType) (DocumentTextReader, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	reader, ok := r.documentTextReaders[formatType]
	if !ok {
		return nil, fmt.Errorf("no document text reader registered for format: %s", formatType)
	}
	return reader, nil
}

func GetMediaProvider(formatType FormatType) (MediaProvider, error) {
	return globalProviderRegistry.GetMediaProvider(formatType)
}

func (r *ProviderRegistry) GetMediaProvider(formatType FormatType) (MediaProvider, error) {
	return r.GetMediaInfoProvider(formatType)
}

func GetMediaInfoProvider(formatType FormatType) (MediaInfoProvider, error) {
	return globalProviderRegistry.GetMediaInfoProvider(formatType)
}

func (r *ProviderRegistry) GetMediaInfoProvider(formatType FormatType) (MediaInfoProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.mediaInfoProviders[formatType]
	if !ok {
		return nil, fmt.Errorf("no media info provider registered for format: %s", formatType)
	}
	return provider, nil
}

func GetContainerInfoProvider(formatType FormatType) (ContainerInfoProvider, error) {
	return globalProviderRegistry.GetContainerInfoProvider(formatType)
}

func (r *ProviderRegistry) GetContainerInfoProvider(formatType FormatType) (ContainerInfoProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.containerInfoProviders[formatType]
	if !ok {
		return nil, fmt.Errorf("no container info provider registered for format: %s", formatType)
	}
	return provider, nil
}

func ListFormatPluginFormats() []FormatType {
	return globalProviderRegistry.ListFormatPluginFormats()
}

func (r *ProviderRegistry) ListFormatPluginFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.formatPlugins))
	for formatType := range r.formatPlugins {
		formats = append(formats, formatType)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i] < formats[j]
	})
	return formats
}

func ListFormatInfoProviderFormats() []FormatType {
	return globalProviderRegistry.ListFormatInfoProviderFormats()
}

func (r *ProviderRegistry) ListFormatInfoProviderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.formatInfoProviders))
	for formatType := range r.formatInfoProviders {
		formats = append(formats, formatType)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i] < formats[j]
	})
	return formats
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

func ListTableInfoProviderFormats() []FormatType {
	return globalProviderRegistry.ListTableInfoProviderFormats()
}

func (r *ProviderRegistry) ListTableInfoProviderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.tableInfoProviders))
	for formatType := range r.tableInfoProviders {
		formats = append(formats, formatType)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i] < formats[j]
	})
	return formats
}

func ListTableSampleProviderFormats() []FormatType {
	return globalProviderRegistry.ListTableSampleProviderFormats()
}

func (r *ProviderRegistry) ListTableSampleProviderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.tableSampleProviders))
	for formatType := range r.tableSampleProviders {
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

func ListDocumentInfoProviderFormats() []FormatType {
	return globalProviderRegistry.ListDocumentInfoProviderFormats()
}

func (r *ProviderRegistry) ListDocumentInfoProviderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.documentInfoProviders))
	for formatType := range r.documentInfoProviders {
		formats = append(formats, formatType)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i] < formats[j]
	})
	return formats
}

func ListDocumentTextReaderFormats() []FormatType {
	return globalProviderRegistry.ListDocumentTextReaderFormats()
}

func (r *ProviderRegistry) ListDocumentTextReaderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.documentTextReaders))
	for formatType := range r.documentTextReaders {
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
	return r.ListMediaInfoProviderFormats()
}

func ListMediaInfoProviderFormats() []FormatType {
	return globalProviderRegistry.ListMediaInfoProviderFormats()
}

func (r *ProviderRegistry) ListMediaInfoProviderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.mediaInfoProviders))
	for formatType := range r.mediaInfoProviders {
		formats = append(formats, formatType)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i] < formats[j]
	})
	return formats
}

func ListContainerInfoProviderFormats() []FormatType {
	return globalProviderRegistry.ListContainerInfoProviderFormats()
}

func (r *ProviderRegistry) ListContainerInfoProviderFormats() []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(r.containerInfoProviders))
	for formatType := range r.containerInfoProviders {
		formats = append(formats, formatType)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i] < formats[j]
	})
	return formats
}

func NewFormatInfoProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error),
) FormatInfoProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error) {
			return nil, fmt.Errorf("format info provider %s does not implement DescribeFormat", formatType)
		}
	}
	return formatInfoProviderAdapter{
		formatType: formatType,
		describe:   describe,
	}
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

func NewContainerInfoProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*ContainerInfo, error),
) ContainerInfoProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*ContainerInfo, error) {
			return nil, fmt.Errorf("container info provider %s does not implement DescribeContainer", formatType)
		}
	}
	return containerProviderAdapter{
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
			return "", false, fmt.Errorf("document provider %s does not implement ReadDocumentText", formatType)
		}
	}
	return documentProviderAdapter{
		formatType: formatType,
		describe:   describe,
		extract:    extract,
	}
}
