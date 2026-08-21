package format

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type ProviderRegistry struct {
	mu            sync.RWMutex
	formatPlugins map[FormatType]FormatPlugin
}

var globalProviderRegistry = NewProviderRegistry()

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		formatPlugins: make(map[FormatType]FormatPlugin),
	}
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

func GetFormatDescriptorProvider(formatType FormatType) (FormatDescriptorProvider, error) {
	return globalProviderRegistry.GetFormatDescriptorProvider(formatType)
}

func (r *ProviderRegistry) GetFormatDescriptorProvider(formatType FormatType) (FormatDescriptorProvider, error) {
	return pluginCapability[FormatDescriptorProvider](r, formatType, "format descriptor provider")
}

func GetFormatInfoProvider(formatType FormatType) (FormatInfoProvider, error) {
	return globalProviderRegistry.GetFormatInfoProvider(formatType)
}

func (r *ProviderRegistry) GetFormatInfoProvider(formatType FormatType) (FormatInfoProvider, error) {
	return pluginCapability[FormatInfoProvider](r, formatType, "format info provider")
}

func GetSpatialEncodingCapabilityProvider(formatType FormatType) (SpatialEncodingCapabilityProvider, error) {
	return globalProviderRegistry.GetSpatialEncodingCapabilityProvider(formatType)
}

func (r *ProviderRegistry) GetSpatialEncodingCapabilityProvider(formatType FormatType) (SpatialEncodingCapabilityProvider, error) {
	return pluginCapability[SpatialEncodingCapabilityProvider](r, formatType, "spatial encoding capability provider")
}

func GetSpatialEncodingCapability(formatType FormatType) (SpatialEncodingCapability, error) {
	provider, err := GetSpatialEncodingCapabilityProvider(formatType)
	if err != nil {
		return SpatialEncodingCapability{}, err
	}
	return provider.SpatialEncodingCapability(), nil
}

func GetCRSDefinitionWriteRequirementProvider(formatType FormatType) (CRSDefinitionWriteRequirementProvider, error) {
	return globalProviderRegistry.GetCRSDefinitionWriteRequirementProvider(formatType)
}

func (r *ProviderRegistry) GetCRSDefinitionWriteRequirementProvider(formatType FormatType) (CRSDefinitionWriteRequirementProvider, error) {
	return pluginCapability[CRSDefinitionWriteRequirementProvider](r, formatType, "CRS definition write requirement provider")
}

func GetColumnarCompressionCapabilityProvider(formatType FormatType) (ColumnarCompressionCapabilityProvider, error) {
	return globalProviderRegistry.GetColumnarCompressionCapabilityProvider(formatType)
}

func (r *ProviderRegistry) GetColumnarCompressionCapabilityProvider(formatType FormatType) (ColumnarCompressionCapabilityProvider, error) {
	return pluginCapability[ColumnarCompressionCapabilityProvider](r, formatType, "columnar compression capability provider")
}

func GetColumnarCompressionCapability(formatType FormatType) (ColumnarCompressionCapability, error) {
	provider, err := GetColumnarCompressionCapabilityProvider(formatType)
	if err != nil {
		return ColumnarCompressionCapability{}, err
	}
	capability := provider.ColumnarCompressionCapability()
	capability.Codecs = append([]string(nil), capability.Codecs...)
	if err := validateColumnarCompressionCapability(capability); err != nil {
		return ColumnarCompressionCapability{}, fmt.Errorf("format %s has invalid columnar compression capability: %w", formatType, err)
	}
	return capability, nil
}

func validateColumnarCompressionCapability(capability ColumnarCompressionCapability) error {
	if capability.Default == "" || capability.Default != strings.TrimSpace(strings.ToLower(capability.Default)) {
		return fmt.Errorf("default codec must be a canonical lowercase value")
	}
	seen := make(map[string]struct{}, len(capability.Codecs))
	for _, codec := range capability.Codecs {
		if codec == "" || codec != strings.TrimSpace(strings.ToLower(codec)) {
			return fmt.Errorf("codec %q must be a canonical lowercase value", codec)
		}
		if _, exists := seen[codec]; exists {
			return fmt.Errorf("codec %q is duplicated", codec)
		}
		seen[codec] = struct{}{}
	}
	if _, exists := seen[capability.Default]; !exists {
		return fmt.Errorf("default codec %q is not declared", capability.Default)
	}
	return nil
}

func GetMultiTableInfoProvider(formatType FormatType) (MultiTableInfoProvider, error) {
	return globalProviderRegistry.GetMultiTableInfoProvider(formatType)
}

func (r *ProviderRegistry) GetMultiTableInfoProvider(formatType FormatType) (MultiTableInfoProvider, error) {
	return pluginCapability[MultiTableInfoProvider](r, formatType, "multi table info provider")
}

func GetMultiTableSampleReader(formatType FormatType) (MultiTableSampleReader, error) {
	return globalProviderRegistry.GetMultiTableSampleReader(formatType)
}

func (r *ProviderRegistry) GetMultiTableSampleReader(formatType FormatType) (MultiTableSampleReader, error) {
	return pluginCapability[MultiTableSampleReader](r, formatType, "multi table sample reader")
}

func GetScopeTableInfoProvider(formatType FormatType) (ScopeTableInfoProvider, error) {
	return globalProviderRegistry.GetScopeTableInfoProvider(formatType)
}

func (r *ProviderRegistry) GetScopeTableInfoProvider(formatType FormatType) (ScopeTableInfoProvider, error) {
	return pluginCapability[ScopeTableInfoProvider](r, formatType, "scope table info provider")
}

func GetScopeTableSampleReader(formatType FormatType) (ScopeTableSampleReader, error) {
	return globalProviderRegistry.GetScopeTableSampleReader(formatType)
}

func (r *ProviderRegistry) GetScopeTableSampleReader(formatType FormatType) (ScopeTableSampleReader, error) {
	return pluginCapability[ScopeTableSampleReader](r, formatType, "scope table sample reader")
}

func GetScopeTableReaderProvider(formatType FormatType) (ScopeTableReaderProvider, error) {
	return globalProviderRegistry.GetScopeTableReaderProvider(formatType)
}

func (r *ProviderRegistry) GetScopeTableReaderProvider(formatType FormatType) (ScopeTableReaderProvider, error) {
	return pluginCapability[ScopeTableReaderProvider](r, formatType, "scope table reader provider")
}

func GetScopeTableWriterProvider(formatType FormatType) (ScopeTableWriterProvider, error) {
	return globalProviderRegistry.GetScopeTableWriterProvider(formatType)
}

func (r *ProviderRegistry) GetScopeTableWriterProvider(formatType FormatType) (ScopeTableWriterProvider, error) {
	return pluginCapability[ScopeTableWriterProvider](r, formatType, "scope table writer provider")
}

func GetRuntimeFormatDetectorFactory(formatType FormatType) (RuntimeFormatDetectorFactory, error) {
	return globalProviderRegistry.GetRuntimeFormatDetectorFactory(formatType)
}

func (r *ProviderRegistry) GetRuntimeFormatDetectorFactory(formatType FormatType) (RuntimeFormatDetectorFactory, error) {
	return pluginCapability[RuntimeFormatDetectorFactory](r, formatType, "runtime format detector factory")
}

func GetRuntimeContainerInfoProviderFactory(formatType FormatType) (RuntimeContainerInfoProviderFactory, error) {
	return globalProviderRegistry.GetRuntimeContainerInfoProviderFactory(formatType)
}

func (r *ProviderRegistry) GetRuntimeContainerInfoProviderFactory(formatType FormatType) (RuntimeContainerInfoProviderFactory, error) {
	return pluginCapability[RuntimeContainerInfoProviderFactory](r, formatType, "runtime container info provider factory")
}

func GetRuntimeScopeTableReaderFactory(formatType FormatType) (RuntimeScopeTableReaderFactory, error) {
	return globalProviderRegistry.GetRuntimeScopeTableReaderFactory(formatType)
}

func (r *ProviderRegistry) GetRuntimeScopeTableReaderFactory(formatType FormatType) (RuntimeScopeTableReaderFactory, error) {
	return pluginCapability[RuntimeScopeTableReaderFactory](r, formatType, "runtime scope table reader factory")
}

func GetRuntimeScopeTableWriterFactory(formatType FormatType) (RuntimeScopeTableWriterFactory, error) {
	return globalProviderRegistry.GetRuntimeScopeTableWriterFactory(formatType)
}

func (r *ProviderRegistry) GetRuntimeScopeTableWriterFactory(formatType FormatType) (RuntimeScopeTableWriterFactory, error) {
	return pluginCapability[RuntimeScopeTableWriterFactory](r, formatType, "runtime scope table writer factory")
}

func GetTableInfoProvider(formatType FormatType) (TableInfoProvider, error) {
	return globalProviderRegistry.GetTableInfoProvider(formatType)
}

func (r *ProviderRegistry) GetTableInfoProvider(formatType FormatType) (TableInfoProvider, error) {
	return pluginCapability[TableInfoProvider](r, formatType, "table info provider")
}

func GetTableSampleReader(formatType FormatType) (TableSampleReader, error) {
	return globalProviderRegistry.GetTableSampleReader(formatType)
}

func (r *ProviderRegistry) GetTableSampleReader(formatType FormatType) (TableSampleReader, error) {
	return pluginCapability[TableSampleReader](r, formatType, "table sample reader")
}

func GetTableReaderProvider(formatType FormatType) (TableReaderProvider, error) {
	return globalProviderRegistry.GetTableReaderProvider(formatType)
}

func (r *ProviderRegistry) GetTableReaderProvider(formatType FormatType) (TableReaderProvider, error) {
	return pluginCapability[TableReaderProvider](r, formatType, "table reader provider")
}

func GetMultiTableReaderProvider(formatType FormatType) (MultiTableReaderProvider, error) {
	return globalProviderRegistry.GetMultiTableReaderProvider(formatType)
}

func (r *ProviderRegistry) GetMultiTableReaderProvider(formatType FormatType) (MultiTableReaderProvider, error) {
	return pluginCapability[MultiTableReaderProvider](r, formatType, "multi table reader provider")
}

func GetTableWriterProvider(formatType FormatType) (TableWriterProvider, error) {
	return globalProviderRegistry.GetTableWriterProvider(formatType)
}

func (r *ProviderRegistry) GetTableWriterProvider(formatType FormatType) (TableWriterProvider, error) {
	return pluginCapability[TableWriterProvider](r, formatType, "table writer provider")
}

func GetMultiTableWriterProvider(formatType FormatType) (MultiTableWriterProvider, error) {
	return globalProviderRegistry.GetMultiTableWriterProvider(formatType)
}

func (r *ProviderRegistry) GetMultiTableWriterProvider(formatType FormatType) (MultiTableWriterProvider, error) {
	return pluginCapability[MultiTableWriterProvider](r, formatType, "multi table writer provider")
}

func GetDocumentInfoProvider(formatType FormatType) (DocumentInfoProvider, error) {
	return globalProviderRegistry.GetDocumentInfoProvider(formatType)
}

func (r *ProviderRegistry) GetDocumentInfoProvider(formatType FormatType) (DocumentInfoProvider, error) {
	return pluginCapability[DocumentInfoProvider](r, formatType, "document info provider")
}

func GetDocumentTextReader(formatType FormatType) (DocumentTextReader, error) {
	return globalProviderRegistry.GetDocumentTextReader(formatType)
}

func (r *ProviderRegistry) GetDocumentTextReader(formatType FormatType) (DocumentTextReader, error) {
	return pluginCapability[DocumentTextReader](r, formatType, "document text reader")
}

func GetBinaryContentReader(formatType FormatType) (BinaryContentReader, error) {
	return globalProviderRegistry.GetBinaryContentReader(formatType)
}

func (r *ProviderRegistry) GetBinaryContentReader(formatType FormatType) (BinaryContentReader, error) {
	return pluginCapability[BinaryContentReader](r, formatType, "binary content reader")
}

func GetMediaInfoProvider(formatType FormatType) (MediaInfoProvider, error) {
	return globalProviderRegistry.GetMediaInfoProvider(formatType)
}

func (r *ProviderRegistry) GetMediaInfoProvider(formatType FormatType) (MediaInfoProvider, error) {
	return pluginCapability[MediaInfoProvider](r, formatType, "media info provider")
}

func GetModel3DInfoProvider(formatType FormatType) (Model3DInfoProvider, error) {
	return globalProviderRegistry.GetModel3DInfoProvider(formatType)
}

func (r *ProviderRegistry) GetModel3DInfoProvider(formatType FormatType) (Model3DInfoProvider, error) {
	return pluginCapability[Model3DInfoProvider](r, formatType, "3D model info provider")
}

func GetScopeModel3DInfoProvider(formatType FormatType) (ScopeModel3DInfoProvider, error) {
	return globalProviderRegistry.GetScopeModel3DInfoProvider(formatType)
}

func (r *ProviderRegistry) GetScopeModel3DInfoProvider(formatType FormatType) (ScopeModel3DInfoProvider, error) {
	return pluginCapability[ScopeModel3DInfoProvider](r, formatType, "scope 3D model info provider")
}

func GetPointCloudInfoProvider(formatType FormatType) (PointCloudInfoProvider, error) {
	return globalProviderRegistry.GetPointCloudInfoProvider(formatType)
}

func (r *ProviderRegistry) GetPointCloudInfoProvider(formatType FormatType) (PointCloudInfoProvider, error) {
	return pluginCapability[PointCloudInfoProvider](r, formatType, "point cloud info provider")
}

func GetScopePointCloudInfoProvider(formatType FormatType) (ScopePointCloudInfoProvider, error) {
	return globalProviderRegistry.GetScopePointCloudInfoProvider(formatType)
}

func (r *ProviderRegistry) GetScopePointCloudInfoProvider(formatType FormatType) (ScopePointCloudInfoProvider, error) {
	return pluginCapability[ScopePointCloudInfoProvider](r, formatType, "scope point cloud info provider")
}

func GetGaussianSplatInfoProvider(formatType FormatType) (GaussianSplatInfoProvider, error) {
	return globalProviderRegistry.GetGaussianSplatInfoProvider(formatType)
}

func (r *ProviderRegistry) GetGaussianSplatInfoProvider(formatType FormatType) (GaussianSplatInfoProvider, error) {
	return pluginCapability[GaussianSplatInfoProvider](r, formatType, "Gaussian splat info provider")
}

func GetContainerInfoProvider(formatType FormatType) (ContainerInfoProvider, error) {
	return globalProviderRegistry.GetContainerInfoProvider(formatType)
}

func (r *ProviderRegistry) GetContainerInfoProvider(formatType FormatType) (ContainerInfoProvider, error) {
	return pluginCapability[ContainerInfoProvider](r, formatType, "container info provider")
}

func GetContainerChildResolver(formatType FormatType) (ContainerChildResolver, error) {
	return globalProviderRegistry.GetContainerChildResolver(formatType)
}

func (r *ProviderRegistry) GetContainerChildResolver(formatType FormatType) (ContainerChildResolver, error) {
	return pluginCapability[ContainerChildResolver](r, formatType, "container child resolver")
}

func pluginCapability[T any](r *ProviderRegistry, formatType FormatType, label string) (T, error) {
	plugin, err := r.GetFormatPlugin(formatType)
	if err != nil {
		var zero T
		return zero, err
	}
	capability, ok := plugin.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("format %s has no %s", formatType, label)
	}
	return capability, nil
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
