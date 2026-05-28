package format

import (
	"github.com/addp/common/datatype"
	formatregistry "github.com/addp/common/format/registry"
)

type FormatIdentification = formatregistry.Identification

type FormatProviderDescriptor = formatregistry.ProviderDescriptor

type FormatContentReader = string

const (
	ContentReaderTableSample      FormatContentReader = formatregistry.ContentReaderTableSample
	ContentReaderMultiTableSample FormatContentReader = formatregistry.ContentReaderMultiTableSample
	ContentReaderScopeTableSample FormatContentReader = formatregistry.ContentReaderScopeTableSample
	ContentReaderDocumentText     FormatContentReader = formatregistry.ContentReaderDocumentText
	ContentReaderBinaryContent    FormatContentReader = formatregistry.ContentReaderBinaryContent
	ContentReaderRawContent       FormatContentReader = formatregistry.ContentReaderRawContent
	ContentReaderRangeContent     FormatContentReader = formatregistry.ContentReaderRangeContent
	ContentReaderMediaThumbnail   FormatContentReader = formatregistry.ContentReaderMediaThumbnail
	ContentReaderContainerEntry   FormatContentReader = formatregistry.ContentReaderContainerEntry
	ContentReaderGraphSample      FormatContentReader = formatregistry.ContentReaderGraphSample
)

type FormatDescriptor struct {
	ID             string                   `json:"id"`
	Version        string                   `json:"version,omitempty"`
	Priority       int                      `json:"priority,omitempty"`
	Format         FormatType               `json:"format"`
	I18nKey        string                   `json:"i18n_key,omitempty"`
	DataType       datatype.DataType        `json:"data_type"`
	Layouts        []string                 `json:"layouts,omitempty"`
	ProviderHints  []string                 `json:"provider_hints,omitempty"`
	Identification FormatIdentification     `json:"identification,omitempty"`
	Providers      FormatProviderDescriptor `json:"providers,omitempty"`
	ContentReaders []string                 `json:"content_readers,omitempty"`
	TransferRead   bool                     `json:"transfer_read,omitempty"`
	TransferWrite  bool                     `json:"transfer_write,omitempty"`
	Parse          bool                     `json:"parse,omitempty"`
	Spatial        bool                     `json:"spatial,omitempty"`
	EngineFamilies []string                 `json:"engine_families,omitempty"`
}

func ListFormatDescriptors() []FormatDescriptor {
	descriptors := formatregistry.ListDescriptors()
	result := make([]FormatDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		result = append(result, fromRegistryDescriptor(descriptor))
	}
	return result
}

func GetFormatDescriptor(formatType FormatType) (FormatDescriptor, bool) {
	descriptor, ok := formatregistry.GetDescriptor(formatregistry.Format(formatType))
	if !ok {
		return FormatDescriptor{}, false
	}
	return fromRegistryDescriptor(descriptor), true
}

func SupportsAccessIndex(formatType FormatType) bool {
	descriptor, ok := GetFormatDescriptor(formatType)
	return ok && descriptor.Providers.AccessIndex
}

func RegisterFormatDescriptor(descriptor FormatDescriptor) error {
	registryDescriptor := toRegistryDescriptor(descriptor)
	if err := formatregistry.RegisterDescriptor(registryDescriptor); err != nil {
		return err
	}
	return RegisterFormatCapability(FormatCapabilityFromDescriptor(fromRegistryDescriptor(registryDescriptor)))
}

func toRegistryDescriptor(descriptor FormatDescriptor) formatregistry.Descriptor {
	return formatregistry.Descriptor{
		ID:             descriptor.ID,
		Version:        descriptor.Version,
		Priority:       descriptor.Priority,
		Format:         formatregistry.Format(descriptor.Format),
		I18nKey:        descriptor.I18nKey,
		DataType:       descriptor.DataType,
		Layouts:        descriptor.Layouts,
		ProviderHints:  descriptor.ProviderHints,
		Identification: descriptor.Identification,
		Providers:      descriptor.Providers,
		ContentReaders: descriptor.ContentReaders,
		TransferRead:   descriptor.TransferRead,
		TransferWrite:  descriptor.TransferWrite,
		Parse:          descriptor.Parse,
		Spatial:        descriptor.Spatial,
		EngineFamilies: descriptor.EngineFamilies,
	}
}

func fromRegistryDescriptor(descriptor formatregistry.Descriptor) FormatDescriptor {
	return FormatDescriptor{
		ID:             descriptor.ID,
		Version:        descriptor.Version,
		Priority:       descriptor.Priority,
		Format:         FormatType(descriptor.Format),
		I18nKey:        descriptor.I18nKey,
		DataType:       descriptor.DataType,
		Layouts:        descriptor.Layouts,
		ProviderHints:  descriptor.ProviderHints,
		Identification: descriptor.Identification,
		Providers:      descriptor.Providers,
		ContentReaders: descriptor.ContentReaders,
		TransferRead:   descriptor.TransferRead,
		TransferWrite:  descriptor.TransferWrite,
		Parse:          descriptor.Parse,
		Spatial:        descriptor.Spatial,
		EngineFamilies: descriptor.EngineFamilies,
	}
}
