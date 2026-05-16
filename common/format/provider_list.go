package format

import "sort"

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
