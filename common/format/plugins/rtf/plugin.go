package rtf

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatRTF
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-rtf",
		Format:   p.Format(),
		I18nKey:  "format.rtf",
		DataType: datatype.Document,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".rtf"},
			MimeTypes:         []string{"application/rtf", "text/rtf"},
			ContentSignatures: []string{`{\rtf`},
		},
	}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
