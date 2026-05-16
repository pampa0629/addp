package format

import "fmt"

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
