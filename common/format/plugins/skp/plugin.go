package skp

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

// Plugin declares SketchUp identity; conversion belongs to Model3D Runtime.
type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }
func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
func (*Plugin) Format() format.FormatType { return format.FormatSKP }
func (*Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID: "builtin-skp", Format: format.FormatSKP, I18nKey: "format.skp",
		DataType: datatype.Model3D, Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".skp"}},
	}
}
