package grd

import (
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/shared/rastermedia"
)

type Plugin struct{}

func (p *Plugin) Format() format.FormatType { return format.FormatGRD }
func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{ID: "builtin-grd", Format: p.Format(), I18nKey: "format.grd",
		DataType: datatype.Media, Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".grd"}, ContentSignatures: []string{"hex:44535242", "hex:44534242", "hex:44534141"}}}
}
func (p *Plugin) RequiredMediaInfoOperators() []string { return []string{"raster_inspect"} }
func (p *Plugin) BindMediaInfoProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, tenantID uint, plan workflowaccess.SourcePlan) (format.BoundMediaInfoProvider, error) {
	return rastermedia.Bind(p.Format(), []string{"GS7BG", "GSBG", "GSAG"}, runtime, conn, tenantID, plan)
}

func init() {
	if err := format.RegisterFormatPlugin(&Plugin{}); err != nil {
		panic(err)
	}
}
