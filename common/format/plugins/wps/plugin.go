package wps

import "github.com/addp/common/format"

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatWPS
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	descriptor, ok := format.GetFormatDescriptor(p.Format())
	if ok {
		return descriptor
	}
	return format.FormatDescriptor{
		ID:            "builtin-wps",
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
