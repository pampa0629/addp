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

func GetFormatPlugin(formatType FormatType) (FormatPlugin, error) {
	return globalProviderRegistry.GetFormatPlugin(formatType)
}

func (r *ProviderRegistry) GetFormatPlugin(formatType FormatType) (FormatPlugin, error) {
	return providerFromMap(r, r.formatPlugins, formatType, "format plugin")
}

func GetFormatInfoProvider(formatType FormatType) (FormatInfoProvider, error) {
	return globalProviderRegistry.GetFormatInfoProvider(formatType)
}

func (r *ProviderRegistry) GetFormatInfoProvider(formatType FormatType) (FormatInfoProvider, error) {
	return providerFromMap(r, r.formatInfoProviders, formatType, "format info provider")
}

func GetTableProvider(formatType FormatType) (TableProvider, error) {
	return globalProviderRegistry.GetTableProvider(formatType)
}

func (r *ProviderRegistry) GetTableProvider(formatType FormatType) (TableProvider, error) {
	return providerFromMap(r, r.tableProviders, formatType, "table provider")
}

func GetTableInfoProvider(formatType FormatType) (TableInfoProvider, error) {
	return globalProviderRegistry.GetTableInfoProvider(formatType)
}

func (r *ProviderRegistry) GetTableInfoProvider(formatType FormatType) (TableInfoProvider, error) {
	return providerFromMap(r, r.tableInfoProviders, formatType, "table info provider")
}

func GetTableSampleProvider(formatType FormatType) (TableSampleProvider, error) {
	return globalProviderRegistry.GetTableSampleProvider(formatType)
}

func (r *ProviderRegistry) GetTableSampleProvider(formatType FormatType) (TableSampleProvider, error) {
	return providerFromMap(r, r.tableSampleProviders, formatType, "table sample provider")
}

func GetDocumentProvider(formatType FormatType) (DocumentProvider, error) {
	return globalProviderRegistry.GetDocumentProvider(formatType)
}

func (r *ProviderRegistry) GetDocumentProvider(formatType FormatType) (DocumentProvider, error) {
	return providerFromMap(r, r.documentProviders, formatType, "document provider")
}

func GetDocumentInfoProvider(formatType FormatType) (DocumentInfoProvider, error) {
	return globalProviderRegistry.GetDocumentInfoProvider(formatType)
}

func (r *ProviderRegistry) GetDocumentInfoProvider(formatType FormatType) (DocumentInfoProvider, error) {
	return providerFromMap(r, r.documentInfoProviders, formatType, "document info provider")
}

func GetDocumentTextReader(formatType FormatType) (DocumentTextReader, error) {
	return globalProviderRegistry.GetDocumentTextReader(formatType)
}

func (r *ProviderRegistry) GetDocumentTextReader(formatType FormatType) (DocumentTextReader, error) {
	return providerFromMap(r, r.documentTextReaders, formatType, "document text reader")
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
	return providerFromMap(r, r.mediaInfoProviders, formatType, "media info provider")
}

func GetContainerInfoProvider(formatType FormatType) (ContainerInfoProvider, error) {
	return globalProviderRegistry.GetContainerInfoProvider(formatType)
}

func (r *ProviderRegistry) GetContainerInfoProvider(formatType FormatType) (ContainerInfoProvider, error) {
	return providerFromMap(r, r.containerInfoProviders, formatType, "container info provider")
}

func GetContainerChildResolver(formatType FormatType) (ContainerChildResolver, error) {
	return globalProviderRegistry.GetContainerChildResolver(formatType)
}

func (r *ProviderRegistry) GetContainerChildResolver(formatType FormatType) (ContainerChildResolver, error) {
	return providerFromMap(r, r.containerChildResolvers, formatType, "container child resolver")
}

func providerFromMap[T any](r *ProviderRegistry, values map[FormatType]T, formatType FormatType, label string) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := values[formatType]
	if !ok {
		var zero T
		return zero, fmt.Errorf("no %s registered for format: %s", label, formatType)
	}
	return provider, nil
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
