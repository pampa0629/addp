package document

import "github.com/addp/common/format"

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatDocument
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:            "builtin-document",
		Format:        p.Format(),
		I18nKey:       "format.document",
		DataType:      format.FormatDataTypeDocument,
		Layouts:       []string{format.FormatLayoutWhole},
		ProviderHints: []string{format.FormatProviderDocument},
		Providers:     format.FormatProviderDescriptor{DocumentInfo: true},
		ContentReaders: []string{
			string(format.ContentReaderDocumentText),
			string(format.ContentReaderRawContent),
		},
		TransferRead:   true,
		TransferWrite:  true,
		EngineFamilies: []string{format.EngineFamilyDocument},
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
