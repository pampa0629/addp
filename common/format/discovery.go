package format

// FormatCapabilitySnapshot 是由静态 descriptor 和当前进程插件接口断言派生的诊断视图。
//
// 它不是格式事实源；业务调用方需要可执行能力时应直接调用 Get*Provider / Get*Reader。
type FormatCapabilitySnapshot struct {
	Descriptor      FormatDescriptor             `json:"descriptor"`
	Implementations FormatImplementationSnapshot `json:"implementations,omitempty"`
}

type FormatImplementationSnapshot struct {
	FormatPlugin             bool `json:"format_plugin,omitempty"`
	FormatDescriptorProvider bool `json:"format_descriptor_provider,omitempty"`
	FormatInfoProvider       bool `json:"format_info_provider,omitempty"`
	TableInfoProvider        bool `json:"table_info_provider,omitempty"`
	TableSampleReader        bool `json:"table_sample_reader,omitempty"`
	MultiTableInfoProvider   bool `json:"multi_table_info_provider,omitempty"`
	MultiTableSampleReader   bool `json:"multi_table_sample_reader,omitempty"`
	ScopeTableInfoProvider   bool `json:"scope_table_info_provider,omitempty"`
	ScopeTableSampleReader   bool `json:"scope_table_sample_reader,omitempty"`
	ScopeTableReader         bool `json:"scope_table_reader_provider,omitempty"`
	TableReaderProvider      bool `json:"table_reader_provider,omitempty"`
	MultiTableReader         bool `json:"multi_table_reader_provider,omitempty"`
	TableWriterProvider      bool `json:"table_writer_provider,omitempty"`
	MultiTableWriter         bool `json:"multi_table_writer_provider,omitempty"`
	DocumentInfoProvider     bool `json:"document_info_provider,omitempty"`
	DocumentTextReader       bool `json:"document_text_reader,omitempty"`
	BinaryContentReader      bool `json:"binary_content_reader,omitempty"`
	MediaInfoProvider        bool `json:"media_info_provider,omitempty"`
	ContainerInfoProvider    bool `json:"container_info_provider,omitempty"`
	ContainerChildResolver   bool `json:"container_child_resolver,omitempty"`
	AccessIndexProvider      bool `json:"access_index_provider,omitempty"`
}

func ListFormatCapabilitySnapshots() []FormatCapabilitySnapshot {
	descriptors := ListFormatDescriptors()
	result := make([]FormatCapabilitySnapshot, 0, len(descriptors))
	for _, descriptor := range descriptors {
		result = append(result, FormatCapabilitySnapshot{
			Descriptor:      descriptor,
			Implementations: implementationSnapshotForFormat(descriptor.Format),
		})
	}
	return result
}

func GetFormatCapabilitySnapshot(formatType FormatType) (FormatCapabilitySnapshot, bool) {
	descriptor, ok := GetFormatDescriptor(formatType)
	if !ok {
		return FormatCapabilitySnapshot{}, false
	}
	return FormatCapabilitySnapshot{
		Descriptor:      descriptor,
		Implementations: implementationSnapshotForFormat(formatType),
	}, true
}

func ListFormatConflictDiagnostics() []FormatConflictDiagnostic {
	return globalDescriptorRegistry.ListFormatConflictDiagnostics()
}

func implementationSnapshotForFormat(formatType FormatType) FormatImplementationSnapshot {
	status := FormatImplementationSnapshot{}

	plugin, err := GetFormatPlugin(formatType)
	if err != nil {
		return status
	}
	status.FormatPlugin = true
	_, status.FormatDescriptorProvider = plugin.(FormatDescriptorProvider)
	_, status.FormatInfoProvider = plugin.(FormatInfoProvider)
	_, status.TableInfoProvider = plugin.(TableInfoProvider)
	_, status.TableSampleReader = plugin.(TableSampleReader)
	_, status.MultiTableInfoProvider = plugin.(MultiTableInfoProvider)
	_, status.MultiTableSampleReader = plugin.(MultiTableSampleReader)
	_, status.ScopeTableInfoProvider = plugin.(ScopeTableInfoProvider)
	_, status.ScopeTableSampleReader = plugin.(ScopeTableSampleReader)
	_, status.ScopeTableReader = plugin.(ScopeTableReaderProvider)
	_, status.TableReaderProvider = plugin.(TableReaderProvider)
	_, status.MultiTableReader = plugin.(MultiTableReaderProvider)
	_, status.TableWriterProvider = plugin.(TableWriterProvider)
	_, status.MultiTableWriter = plugin.(MultiTableWriterProvider)
	_, status.DocumentInfoProvider = plugin.(DocumentInfoProvider)
	_, status.DocumentTextReader = plugin.(DocumentTextReader)
	_, status.BinaryContentReader = plugin.(BinaryContentReader)
	_, status.MediaInfoProvider = plugin.(MediaInfoProvider)
	_, status.ContainerInfoProvider = plugin.(ContainerInfoProvider)
	_, status.ContainerChildResolver = plugin.(ContainerChildResolver)
	if provider, ok := plugin.(AccessIndexProvider); ok {
		status.AccessIndexProvider = provider.SupportsAccessIndex()
	}
	return status
}
