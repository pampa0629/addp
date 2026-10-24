package jpeg2000

import (
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/shared/rastermedia"
)

type Plugin struct{}

func (p *Plugin) Format() format.FormatType { return format.FormatJPEG2000 }
func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{ID: "builtin-jpeg2000", Format: p.Format(), I18nKey: "format.jpeg2000", DataType: datatype.Media, Layouts: []string{format.LayoutSingle}, Identification: format.FormatIdentification{
		Extensions: []string{".jp2", ".j2k"}, MimeTypes: []string{"image/jp2"}, ContentSignatures: []string{"hex:0000000c6a5020200d0a870a", "hex:ff4fff51"}}}
}
func (p *Plugin) RequiredMediaInfoOperators() []string { return []string{"raster_inspect"} }
func (p *Plugin) BindMediaInfoProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, tenantID uint, plan workflowaccess.SourcePlan) (format.BoundMediaInfoProvider, error) {
	return rastermedia.Bind(p.Format(), "JP2OpenJPEG", runtime, conn, tenantID, plan)
}
func init() {
	if err := format.RegisterFormatPlugin(&Plugin{}); err != nil {
		panic(err)
	}
}
