package sgm

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

// Plugin declares SGM identity; geometry parsing belongs to SuperMap Runtime.
type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }
func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
func (*Plugin) Format() format.FormatType { return format.FormatSGM }
func (*Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{ID: "builtin-sgm", Format: format.FormatSGM, I18nKey: "format.sgm", DataType: datatype.Model3D, Layouts: []string{format.LayoutSingle}, Identification: format.FormatIdentification{Extensions: []string{".sgm"}}}
}
