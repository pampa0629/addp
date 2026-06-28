package osgb

import (
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register OSGB format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatOSGB
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-osgb",
		Format:   format.FormatOSGB,
		I18nKey:  "format.osgb",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".osgb"},
			MimeTypes: []string{
				"application/octet-stream",
			},
		},
	}
}
