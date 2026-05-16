package format

import (
	"fmt"
	"sort"
	"sync"
)

type ProviderRegistry struct {
	mu                      sync.RWMutex
	formatPlugins           map[FormatType]FormatPlugin
	tableProviders          map[FormatType]TableProvider
	formatInfoProviders     map[FormatType]FormatInfoProvider
	tableInfoProviders      map[FormatType]TableInfoProvider
	tableSampleProviders    map[FormatType]TableSampleProvider
	documentProviders       map[FormatType]DocumentProvider
	documentInfoProviders   map[FormatType]DocumentInfoProvider
	documentTextReaders     map[FormatType]DocumentTextReader
	mediaInfoProviders      map[FormatType]MediaInfoProvider
	containerInfoProviders  map[FormatType]ContainerInfoProvider
	containerChildResolvers map[FormatType]ContainerChildResolver
}

var globalProviderRegistry = NewProviderRegistry()

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		formatPlugins:           make(map[FormatType]FormatPlugin),
		tableProviders:          make(map[FormatType]TableProvider),
		formatInfoProviders:     make(map[FormatType]FormatInfoProvider),
		tableInfoProviders:      make(map[FormatType]TableInfoProvider),
		tableSampleProviders:    make(map[FormatType]TableSampleProvider),
		documentProviders:       make(map[FormatType]DocumentProvider),
		documentInfoProviders:   make(map[FormatType]DocumentInfoProvider),
		documentTextReaders:     make(map[FormatType]DocumentTextReader),
		mediaInfoProviders:      make(map[FormatType]MediaInfoProvider),
		containerInfoProviders:  make(map[FormatType]ContainerInfoProvider),
		containerChildResolvers: make(map[FormatType]ContainerChildResolver),
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
	if descriptor.ID != "" && descriptor.DataType != "" && shouldRegisterPluginDescriptor(formatType) {
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
	if resolver, ok := plugin.(ContainerChildResolver); ok {
		if err := r.RegisterContainerChildResolver(resolver); err != nil {
			return err
		}
	}
	return nil
}

func shouldRegisterPluginDescriptor(formatType FormatType) bool {
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

func RegisterContainerChildResolver(resolver ContainerChildResolver) error {
	return globalProviderRegistry.RegisterContainerChildResolver(resolver)
}

func (r *ProviderRegistry) RegisterContainerChildResolver(resolver ContainerChildResolver) error {
	if resolver == nil {
		return fmt.Errorf("container child resolver cannot be nil")
	}
	formatType := resolver.Format()
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("container child resolver must define format")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.containerChildResolvers[formatType] = resolver
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

func GetContainerChildResolver(formatType FormatType) (ContainerChildResolver, error) {
	return globalProviderRegistry.GetContainerChildResolver(formatType)
}

func (r *ProviderRegistry) GetContainerChildResolver(formatType FormatType) (ContainerChildResolver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	resolver, ok := r.containerChildResolvers[formatType]
	if !ok {
		return nil, fmt.Errorf("no container child resolver registered for format: %s", formatType)
	}
	return resolver, nil
}

func ListFormatPluginFormats() []FormatType {
	return globalProviderRegistry.ListFormatPluginFormats()
}

func (r *ProviderRegistry) ListFormatPluginFormats() []FormatType {
	return sortedMapKeys(r, r.formatPlugins)
}

func ListFormatInfoProviderFormats() []FormatType {
	return globalProviderRegistry.ListFormatInfoProviderFormats()
}

func (r *ProviderRegistry) ListFormatInfoProviderFormats() []FormatType {
	return sortedMapKeys(r, r.formatInfoProviders)
}

func ListTableProviderFormats() []FormatType {
	return globalProviderRegistry.ListTableProviderFormats()
}

func (r *ProviderRegistry) ListTableProviderFormats() []FormatType {
	return sortedMapKeys(r, r.tableProviders)
}

func ListTableInfoProviderFormats() []FormatType {
	return globalProviderRegistry.ListTableInfoProviderFormats()
}

func (r *ProviderRegistry) ListTableInfoProviderFormats() []FormatType {
	return sortedMapKeys(r, r.tableInfoProviders)
}

func ListTableSampleProviderFormats() []FormatType {
	return globalProviderRegistry.ListTableSampleProviderFormats()
}

func (r *ProviderRegistry) ListTableSampleProviderFormats() []FormatType {
	return sortedMapKeys(r, r.tableSampleProviders)
}

func ListDocumentProviderFormats() []FormatType {
	return globalProviderRegistry.ListDocumentProviderFormats()
}

func (r *ProviderRegistry) ListDocumentProviderFormats() []FormatType {
	return sortedMapKeys(r, r.documentProviders)
}

func ListDocumentInfoProviderFormats() []FormatType {
	return globalProviderRegistry.ListDocumentInfoProviderFormats()
}

func (r *ProviderRegistry) ListDocumentInfoProviderFormats() []FormatType {
	return sortedMapKeys(r, r.documentInfoProviders)
}

func ListDocumentTextReaderFormats() []FormatType {
	return globalProviderRegistry.ListDocumentTextReaderFormats()
}

func (r *ProviderRegistry) ListDocumentTextReaderFormats() []FormatType {
	return sortedMapKeys(r, r.documentTextReaders)
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
	return sortedMapKeys(r, r.mediaInfoProviders)
}

func ListContainerInfoProviderFormats() []FormatType {
	return globalProviderRegistry.ListContainerInfoProviderFormats()
}

func (r *ProviderRegistry) ListContainerInfoProviderFormats() []FormatType {
	return sortedMapKeys(r, r.containerInfoProviders)
}

func ListContainerChildResolverFormats() []FormatType {
	return globalProviderRegistry.ListContainerChildResolverFormats()
}

func (r *ProviderRegistry) ListContainerChildResolverFormats() []FormatType {
	return sortedMapKeys(r, r.containerChildResolvers)
}

func sortedMapKeys[T any](r *ProviderRegistry, values map[FormatType]T) []FormatType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]FormatType, 0, len(values))
	for formatType := range values {
		formats = append(formats, formatType)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i] < formats[j]
	})
	return formats
}
