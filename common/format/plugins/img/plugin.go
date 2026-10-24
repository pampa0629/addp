package img

import (
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/shared/rastermedia"
)

type Plugin struct{}

func (p *Plugin) Format() format.FormatType { return format.FormatIMG }
func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{ID: "builtin-img", Format: p.Format(), I18nKey: "format.img",
		DataType: datatype.Media, Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".img"}, ContentSignatures: []string{"hex:454846415f4845414445525f54414700"}}}
}
func (p *Plugin) RequiredMediaInfoOperators() []string { return []string{"raster_inspect"} }

func (p *Plugin) BindMediaInfoProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, tenantID uint, plan workflowaccess.SourcePlan) (format.BoundMediaInfoProvider, error) {
	return rastermedia.Bind(p.Format(), "HFA", runtime, conn, tenantID, plan)
}

func init() {
	if err := format.RegisterFormatPlugin(&Plugin{}); err != nil {
		panic(err)
	}
}
