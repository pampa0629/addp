package avro

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatAvro
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-avro",
		Format:         p.Format(),
		I18nKey:        "format.avro",
		DataType:       datatype.DataTypeTable,
		Layouts:        []string{format.LayoutSingle, format.LayoutWhole},
		Identification: format.FormatIdentification{Extensions: []string{".avro"}, MimeTypes: []string{"application/avro", "application/x-avro-binary"}},
	}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
