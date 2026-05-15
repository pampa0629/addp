package orc

import "github.com/addp/common/format"

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatORC
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-orc",
		Format:         p.Format(),
		I18nKey:        "format.orc",
		DataType:       format.FormatDataTypeTable,
		Layouts:        []string{format.FormatLayoutSingle, format.FormatLayoutWhole},
		ProviderHints:  []string{format.FormatProviderTable},
		Identification: format.FormatIdentification{Extensions: []string{".orc"}, MimeTypes: []string{"application/orc", "application/vnd.apache.orc"}},
		ContentReaders: []string{string(format.ContentReaderRawContent)},
		TransferRead:   true,
		TransferWrite:  true,
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.Format())
	if ok {
		return capability
	}
	return format.FormatCapabilityFromDescriptor(p.Descriptor())
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
