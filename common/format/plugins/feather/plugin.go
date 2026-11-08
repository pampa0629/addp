package feather

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatFeather
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-feather",
		Format:         p.Format(),
		I18nKey:        "format.feather",
		DataType:       datatype.Table,
		Layouts:        []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".feather", ".arrow"}, MimeTypes: []string{"application/vnd.apache.arrow.file"}, ContentSignatures: []string{"hex:4152524f5731"}},
	}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
