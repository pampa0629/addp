package orc

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

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
		DataType:       datatype.DataTypeTable,
		Layouts:        []string{format.LayoutSingle, format.LayoutWhole},
		ProviderHints:  []string{format.FormatProviderTable},
		Identification: format.FormatIdentification{Extensions: []string{".orc"}, MimeTypes: []string{"application/orc", "application/vnd.apache.orc"}},
		ContentReaders: []string{string(format.ContentReaderRawContent)},
	}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
