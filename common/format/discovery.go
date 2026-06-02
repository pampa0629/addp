package format

import (
	"github.com/addp/common/datatype"
	formatregistry "github.com/addp/common/format/registry"
)

type FormatSupportView struct {
	PluginID        string                     `json:"plugin_id"`
	Format          FormatType                 `json:"format"`
	DataType        datatype.DataType          `json:"data_type"`
	Layouts         []string                   `json:"layouts,omitempty"`
	Identification  FormatIdentification       `json:"identification,omitempty"`
	Providers       FormatProviderDescriptor   `json:"providers,omitempty"`
	ContentReaders  []string                   `json:"content_readers,omitempty"`
	Implementations FormatImplementationStatus `json:"implementations,omitempty"`
	Parse           bool                       `json:"parse,omitempty"`
	Spatial         bool                       `json:"spatial,omitempty"`
}

type FormatImplementationStatus struct {
	FormatPlugin           bool `json:"format_plugin,omitempty"`
	FormatInfoProvider     bool `json:"format_info_provider,omitempty"`
	TableInfoProvider      bool `json:"table_info_provider,omitempty"`
	TableSampleReader      bool `json:"table_sample_reader,omitempty"`
	MultiTableInfoProvider bool `json:"multi_table_info_provider,omitempty"`
	MultiTableSampleReader bool `json:"multi_table_sample_reader,omitempty"`
	ScopeTableInfoProvider bool `json:"scope_table_info_provider,omitempty"`
	ScopeTableSampleReader bool `json:"scope_table_sample_reader,omitempty"`
	ScopeTableReader       bool `json:"scope_table_reader_provider,omitempty"`
	TableReaderProvider    bool `json:"table_reader_provider,omitempty"`
	MultiTableReader       bool `json:"multi_table_reader_provider,omitempty"`
	TableWriterProvider    bool `json:"table_writer_provider,omitempty"`
	MultiTableWriter       bool `json:"multi_table_writer_provider,omitempty"`
	DocumentInfoProvider   bool `json:"document_info_provider,omitempty"`
	DocumentTextReader     bool `json:"document_text_reader,omitempty"`
	BinaryContentReader    bool `json:"binary_content_reader,omitempty"`
	MediaInfoProvider      bool `json:"media_info_provider,omitempty"`
	ContainerInfoProvider  bool `json:"container_info_provider,omitempty"`
	ContainerChildResolver bool `json:"container_child_resolver,omitempty"`
}

type FormatConflictDiagnostic = formatregistry.ConflictDiagnostic

func ListFormatSupportViews() []FormatSupportView {
	views := formatregistry.ListSupportViews()
	result := make([]FormatSupportView, 0, len(views))
	for _, view := range views {
		result = append(result, fromRegistrySupportView(view))
	}
	return result
}

func GetFormatSupportView(formatType FormatType) (FormatSupportView, bool) {
	view, ok := formatregistry.GetSupportView(formatregistry.Format(formatType))
	if !ok {
		return FormatSupportView{}, false
	}
	return fromRegistrySupportView(view), true
}

func ListFormatConflictDiagnostics() []FormatConflictDiagnostic {
	return formatregistry.ListConflictDiagnostics()
}

func fromRegistrySupportView(view formatregistry.SupportView) FormatSupportView {
	return FormatSupportView{
		PluginID:        view.PluginID,
		Format:          FormatType(view.Format),
		DataType:        view.DataType,
		Layouts:         view.Layouts,
		Identification:  view.Identification,
		Providers:       view.Providers,
		ContentReaders:  append([]string(nil), view.ContentReaders...),
		Implementations: implementationStatusForFormat(FormatType(view.Format), view.Identification),
		Parse:           view.Parse,
		Spatial:         view.Spatial,
	}
}

func implementationStatusForFormat(formatType FormatType, identification FormatIdentification) FormatImplementationStatus {
	status := FormatImplementationStatus{}

	if _, err := GetFormatPlugin(formatType); err == nil {
		status.FormatPlugin = true
	}
	if _, err := GetFormatInfoProvider(formatType); err == nil {
		status.FormatInfoProvider = true
	}
	if _, err := GetTableInfoProvider(formatType); err == nil {
		status.TableInfoProvider = true
	}
	if _, err := GetTableSampleReader(formatType); err == nil {
		status.TableSampleReader = true
	}
	if _, err := GetMultiTableInfoProvider(formatType); err == nil {
		status.MultiTableInfoProvider = true
	}
	if _, err := GetMultiTableSampleReader(formatType); err == nil {
		status.MultiTableSampleReader = true
	}
	if _, err := GetScopeTableInfoProvider(formatType); err == nil {
		status.ScopeTableInfoProvider = true
	}
	if _, err := GetScopeTableSampleReader(formatType); err == nil {
		status.ScopeTableSampleReader = true
	}
	if _, err := GetScopeTableReaderProvider(formatType); err == nil {
		status.ScopeTableReader = true
	}
	if _, err := GetTableReaderProvider(formatType); err == nil {
		status.TableReaderProvider = true
	}
	if _, err := GetMultiTableReaderProvider(formatType); err == nil {
		status.MultiTableReader = true
	}
	if _, err := GetTableWriterProvider(formatType); err == nil {
		status.TableWriterProvider = true
	}
	if _, err := GetMultiTableWriterProvider(formatType); err == nil {
		status.MultiTableWriter = true
	}
	if _, err := GetDocumentInfoProvider(formatType); err == nil {
		status.DocumentInfoProvider = true
	}
	if _, err := GetDocumentTextReader(formatType); err == nil {
		status.DocumentTextReader = true
	}
	if _, err := GetBinaryContentReader(formatType); err == nil {
		status.BinaryContentReader = true
	}
	if _, err := GetMediaInfoProvider(formatType); err == nil {
		status.MediaInfoProvider = true
	}
	if _, err := GetContainerInfoProvider(formatType); err == nil {
		status.ContainerInfoProvider = true
	}
	if _, err := GetContainerChildResolver(formatType); err == nil {
		status.ContainerChildResolver = true
	}

	return status
}
