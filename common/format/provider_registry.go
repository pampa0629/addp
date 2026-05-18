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
	multiTableProviders     map[FormatType]MultiTableProvider
	formatInfoProviders     map[FormatType]FormatInfoProvider
	tableInfoProviders      map[FormatType]TableInfoProvider
	tableSampleProviders    map[FormatType]TableSampleProvider
	tableReaderProviders    map[FormatType]TableReaderProvider
	multiTableReaders       map[FormatType]MultiTableReaderProvider
	tableWriterProviders    map[FormatType]TableWriterProvider
	multiTableWriters       map[FormatType]MultiTableWriterProvider
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
		multiTableProviders:     make(map[FormatType]MultiTableProvider),
		formatInfoProviders:     make(map[FormatType]FormatInfoProvider),
		tableInfoProviders:      make(map[FormatType]TableInfoProvider),
		tableSampleProviders:    make(map[FormatType]TableSampleProvider),
		tableReaderProviders:    make(map[FormatType]TableReaderProvider),
		multiTableReaders:       make(map[FormatType]MultiTableReaderProvider),
		tableWriterProviders:    make(map[FormatType]TableWriterProvider),
		multiTableWriters:       make(map[FormatType]MultiTableWriterProvider),
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

func GetMultiTableProvider(formatType FormatType) (MultiTableProvider, error) {
	return globalProviderRegistry.GetMultiTableProvider(formatType)
}

func (r *ProviderRegistry) GetMultiTableProvider(formatType FormatType) (MultiTableProvider, error) {
	return providerFromMap(r, r.multiTableProviders, formatType, "multi table provider")
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

func GetTableReaderProvider(formatType FormatType) (TableReaderProvider, error) {
	return globalProviderRegistry.GetTableReaderProvider(formatType)
}

func (r *ProviderRegistry) GetTableReaderProvider(formatType FormatType) (TableReaderProvider, error) {
	return providerFromMap(r, r.tableReaderProviders, formatType, "table reader provider")
}

func GetMultiTableReaderProvider(formatType FormatType) (MultiTableReaderProvider, error) {
	return globalProviderRegistry.GetMultiTableReaderProvider(formatType)
}

func (r *ProviderRegistry) GetMultiTableReaderProvider(formatType FormatType) (MultiTableReaderProvider, error) {
	return providerFromMap(r, r.multiTableReaders, formatType, "multi table reader provider")
}

func GetTableWriterProvider(formatType FormatType) (TableWriterProvider, error) {
	return globalProviderRegistry.GetTableWriterProvider(formatType)
}

func (r *ProviderRegistry) GetTableWriterProvider(formatType FormatType) (TableWriterProvider, error) {
	return providerFromMap(r, r.tableWriterProviders, formatType, "table writer provider")
}

func GetMultiTableWriterProvider(formatType FormatType) (MultiTableWriterProvider, error) {
	return globalProviderRegistry.GetMultiTableWriterProvider(formatType)
}

func (r *ProviderRegistry) GetMultiTableWriterProvider(formatType FormatType) (MultiTableWriterProvider, error) {
	return providerFromMap(r, r.multiTableWriters, formatType, "multi table writer provider")
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

func ListMultiTableProviderFormats() []FormatType {
	return globalProviderRegistry.ListMultiTableProviderFormats()
}

func (r *ProviderRegistry) ListMultiTableProviderFormats() []FormatType {
	return sortedMapKeys(r, r.multiTableProviders)
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

func ListTableReaderProviderFormats() []FormatType {
	return globalProviderRegistry.ListTableReaderProviderFormats()
}

func (r *ProviderRegistry) ListTableReaderProviderFormats() []FormatType {
	return sortedMapKeys(r, r.tableReaderProviders)
}

func ListMultiTableReaderProviderFormats() []FormatType {
	return globalProviderRegistry.ListMultiTableReaderProviderFormats()
}

func (r *ProviderRegistry) ListMultiTableReaderProviderFormats() []FormatType {
	return sortedMapKeys(r, r.multiTableReaders)
}

func ListTableWriterProviderFormats() []FormatType {
	return globalProviderRegistry.ListTableWriterProviderFormats()
}

func (r *ProviderRegistry) ListTableWriterProviderFormats() []FormatType {
	return sortedMapKeys(r, r.tableWriterProviders)
}

func ListMultiTableWriterProviderFormats() []FormatType {
	return globalProviderRegistry.ListMultiTableWriterProviderFormats()
}

func (r *ProviderRegistry) ListMultiTableWriterProviderFormats() []FormatType {
	return sortedMapKeys(r, r.multiTableWriters)
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
