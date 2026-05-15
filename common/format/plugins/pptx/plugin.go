package pptx

import "github.com/addp/common/format"

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatPPTX
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:            "builtin-pptx",
		Format:        p.Format(),
		I18nKey:       "format.pptx",
		DataType:      format.FormatDataTypeDocument,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument},
		Identification: format.FormatIdentification{
			Extensions: []string{".pptx"},
			MimeTypes:  []string{"application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		},
		ContentReaders: []string{
			string(format.ContentReaderRawContent),
			string(format.ContentReaderRangeContent),
		},
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile, format.EngineFamilyDocument},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.Format())
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        p.Format(),
		DataType:      format.FormatDataTypeDocument,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument},
		ContentReaders: []string{
			string(format.ContentReaderRawContent),
			string(format.ContentReaderRangeContent),
		},
	}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
