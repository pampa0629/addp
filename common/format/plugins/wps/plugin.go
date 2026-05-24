package wps

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatWPS
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:            "builtin-wps",
		Format:        p.Format(),
		I18nKey:       "format.wps",
		DataType:      datatype.DataTypeDocument,
		Layouts:       []string{format.LayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument},
		Identification: format.FormatIdentification{
			Extensions: []string{".wps"},
			MimeTypes:  []string{"application/vnd.ms-works", "application/wps-office.doc", "application/x-wps", "application/kswps"},
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
		DataType:      datatype.DataTypeDocument,
		Layouts:       []string{format.LayoutSingle},
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
