package format

import formatregistry "github.com/addp/common/format/registry"

type FormatCapabilityView struct {
	PluginID        string                     `json:"plugin_id"`
	Format          FormatType                 `json:"format"`
	DataType        string                     `json:"data_type"`
	Layouts         []string                   `json:"layouts,omitempty"`
	Identification  FormatIdentification       `json:"identification,omitempty"`
	Providers       FormatProviderDescriptor   `json:"providers,omitempty"`
	ContentReaders  []string                   `json:"content_readers,omitempty"`
	Implementations FormatImplementationStatus `json:"implementations,omitempty"`
	Transfer        FormatTransferDescriptor   `json:"transfer,omitempty"`
	Parse           bool                       `json:"parse,omitempty"`
	Spatial         bool                       `json:"spatial,omitempty"`
	EngineFamilies  []string                   `json:"engine_families,omitempty"`
}

type FormatImplementationStatus struct {
	FormatPlugin           bool `json:"format_plugin,omitempty"`
	TableProvider          bool `json:"table_provider,omitempty"`
	FormatInfoProvider     bool `json:"format_info_provider,omitempty"`
	TableInfoProvider      bool `json:"table_info_provider,omitempty"`
	TableSampleReader      bool `json:"table_sample_reader,omitempty"`
	TableSampleProvider    bool `json:"table_sample_provider,omitempty"` // legacy alias
	ComponentTableProvider bool `json:"component_table_provider,omitempty"`
	ScopeTableProvider     bool `json:"scope_table_provider,omitempty"`
	DocumentInfoProvider   bool `json:"document_info_provider,omitempty"`
	DocumentTextReader     bool `json:"document_text_reader,omitempty"`
	DocumentProvider       bool `json:"document_provider,omitempty"` // legacy composite
	MediaInfoProvider      bool `json:"media_info_provider,omitempty"`
	ContainerInfoProvider  bool `json:"container_info_provider,omitempty"`
	MetadataExtractor      bool `json:"metadata_extractor,omitempty"`
}

type FormatTransferDescriptor = formatregistry.TransferDescriptor

type FormatConflictDiagnostic = formatregistry.ConflictDiagnostic

func ListFormatCapabilityViews() []FormatCapabilityView {
	views := formatregistry.ListCapabilityViews()
	result := make([]FormatCapabilityView, 0, len(views))
	for _, view := range views {
		result = append(result, fromRegistryCapabilityView(view))
	}
	return result
}

func GetFormatCapabilityView(formatType FormatType) (FormatCapabilityView, bool) {
	view, ok := formatregistry.GetCapabilityView(formatregistry.Format(formatType))
	if !ok {
		return FormatCapabilityView{}, false
	}
	return fromRegistryCapabilityView(view), true
}

func ListFormatConflictDiagnostics() []FormatConflictDiagnostic {
	return formatregistry.ListConflictDiagnostics()
}

func fromRegistryCapabilityView(view formatregistry.CapabilityView) FormatCapabilityView {
	return FormatCapabilityView{
		PluginID:        view.PluginID,
		Format:          FormatType(view.Format),
		DataType:        view.DataType,
		Layouts:         view.Layouts,
		Identification:  view.Identification,
		Providers:       view.Providers,
		ContentReaders:  append([]string(nil), view.ContentReaders...),
		Implementations: implementationStatusForFormat(FormatType(view.Format), view.Identification),
		Transfer:        view.Transfer,
		Parse:           view.Parse,
		Spatial:         view.Spatial,
		EngineFamilies:  view.EngineFamilies,
	}
}

func implementationStatusForFormat(formatType FormatType, identification FormatIdentification) FormatImplementationStatus {
	status := FormatImplementationStatus{}

	if _, err := GetFormatPlugin(formatType); err == nil {
		status.FormatPlugin = true
	}
	if provider, err := GetTableProvider(formatType); err == nil {
		status.TableProvider = true
		_, status.ComponentTableProvider = provider.(ComponentTableProvider)
		_, status.ScopeTableProvider = provider.(ScopeTableProvider)
	}
	if _, err := GetFormatInfoProvider(formatType); err == nil {
		status.FormatInfoProvider = true
	}
	if _, err := GetTableInfoProvider(formatType); err == nil {
		status.TableInfoProvider = true
	}
	if _, err := GetTableSampleProvider(formatType); err == nil {
		status.TableSampleReader = true
		status.TableSampleProvider = true
	}
	if _, err := GetDocumentInfoProvider(formatType); err == nil {
		status.DocumentInfoProvider = true
	}
	if _, err := GetDocumentTextReader(formatType); err == nil {
		status.DocumentTextReader = true
	}
	if _, err := GetDocumentProvider(formatType); err == nil {
		status.DocumentProvider = true
	}
	if _, err := GetMediaInfoProvider(formatType); err == nil {
		status.MediaInfoProvider = true
	}
	if _, err := GetContainerInfoProvider(formatType); err == nil {
		status.ContainerInfoProvider = true
	}
	status.MetadataExtractor = hasMetadataExtractor(formatType, identification)

	return status
}

func hasMetadataExtractor(formatType FormatType, identification FormatIdentification) bool {
	mimeTypes := append([]string(nil), identification.MimeTypes...)
	if primary := FormatToMIME(formatType); primary != "" && primary != "application/octet-stream" {
		mimeTypes = append(mimeTypes, primary)
	}
	for _, mimeType := range mimeTypes {
		if GetExtractor(mimeType) != nil {
			return true
		}
	}
	return false
}
