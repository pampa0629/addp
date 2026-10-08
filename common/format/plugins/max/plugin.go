package max

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

// Plugin declares 3ds Max identity; conversion belongs to Model3D Runtime.
type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }
func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
func (*Plugin) Format() format.FormatType { return format.FormatMAX }
func (*Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID: "builtin-max", Format: format.FormatMAX, I18nKey: "format.max",
		DataType: datatype.Model3D, Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".max"}},
	}
}
