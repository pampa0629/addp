package format

import formatregistry "github.com/addp/common/format/registry"

type FormatIdentification = formatregistry.Identification

type FormatProviderDescriptor = formatregistry.ProviderDescriptor

type FormatPreviewDescriptor = formatregistry.PreviewDescriptor

type FormatDescriptor struct {
	ID             string                   `json:"id"`
	Version        string                   `json:"version,omitempty"`
	Priority       int                      `json:"priority,omitempty"`
	Format         FormatType               `json:"format"`
	I18nKey        string                   `json:"i18n_key,omitempty"`
	DataType       string                   `json:"data_type"`
	Layouts        []string                 `json:"layouts,omitempty"`
	ProviderHints  []string                 `json:"provider_hints,omitempty"`
	Identification FormatIdentification     `json:"identification,omitempty"`
	Providers      FormatProviderDescriptor `json:"providers,omitempty"`
	Preview        FormatPreviewDescriptor  `json:"preview,omitempty"`
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

func RegisterFormatDescriptor(descriptor FormatDescriptor) error {
	registryDescriptor := toRegistryDescriptor(descriptor)
	if err := formatregistry.RegisterDescriptor(registryDescriptor); err != nil {
		return err
	}
	return RegisterFormatCapability(FormatCapabilityFromDescriptor(fromRegistryDescriptor(registryDescriptor)))
}

func FormatCapabilityFromDescriptor(descriptor FormatDescriptor) FormatCapability {
	return FormatCapability{
		Format:         descriptor.Format,
		I18nKey:        descriptor.I18nKey,
		Extensions:     append([]string(nil), descriptor.Identification.Extensions...),
		DataType:       descriptor.DataType,
		Layouts:        append([]string(nil), descriptor.Layouts...),
		ProviderHints:  append([]string(nil), descriptor.ProviderHints...),
		Spatial:        descriptor.Spatial,
		TransferRead:   descriptor.TransferRead,
		TransferWrite:  descriptor.TransferWrite,
		Preview:        descriptor.Preview.Kind != "" || len(descriptor.Preview.PreviewMaterials) > 0 || descriptor.Preview.FrontendRenderer != "",
		Parse:          descriptor.Parse,
		EngineFamilies: append([]string(nil), descriptor.EngineFamilies...),
	}
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
		Preview:        descriptor.Preview,
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
		Preview:        descriptor.Preview,
		TransferRead:   descriptor.TransferRead,
		TransferWrite:  descriptor.TransferWrite,
		Parse:          descriptor.Parse,
		Spatial:        descriptor.Spatial,
		EngineFamilies: descriptor.EngineFamilies,
	}
}
