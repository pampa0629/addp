package format

import formatregistry "github.com/addp/common/format/registry"

type FormatCapabilityView struct {
	PluginID        string                     `json:"plugin_id"`
	Format          FormatType                 `json:"format"`
	DataType        string                     `json:"data_type"`
	Layouts         []string                   `json:"layouts,omitempty"`
	Identification  FormatIdentification       `json:"identification,omitempty"`
	Providers       FormatProviderDescriptor   `json:"providers,omitempty"`
	Implementations FormatImplementationStatus `json:"implementations,omitempty"`
	Preview         FormatPreviewDescriptor    `json:"preview,omitempty"`
	Transfer        FormatTransferDescriptor   `json:"transfer,omitempty"`
	Parse           bool                       `json:"parse,omitempty"`
	Spatial         bool                       `json:"spatial,omitempty"`
	EngineFamilies  []string                   `json:"engine_families,omitempty"`
}

type FormatImplementationStatus struct {
	TableProvider          bool `json:"table_provider,omitempty"`
	ComponentTableProvider bool `json:"component_table_provider,omitempty"`
	ScopeTableProvider     bool `json:"scope_table_provider,omitempty"`
	DocumentProvider       bool `json:"document_provider,omitempty"`
	MediaProvider          bool `json:"media_provider,omitempty"`
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
		Implementations: implementationStatusForFormat(FormatType(view.Format), view.Identification),
		Preview:         view.Preview,
		Transfer:        view.Transfer,
		Parse:           view.Parse,
		Spatial:         view.Spatial,
		EngineFamilies:  view.EngineFamilies,
	}
}

func implementationStatusForFormat(formatType FormatType, identification FormatIdentification) FormatImplementationStatus {
	status := FormatImplementationStatus{}

	if provider, err := GetTableProvider(formatType); err == nil {
		status.TableProvider = true
		_, status.ComponentTableProvider = provider.(ComponentTableProvider)
		_, status.ScopeTableProvider = provider.(ScopeTableProvider)
	}
	if _, err := GetDocumentProvider(formatType); err == nil {
		status.DocumentProvider = true
	}
	if _, err := GetMediaProvider(formatType); err == nil {
		status.MediaProvider = true
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
