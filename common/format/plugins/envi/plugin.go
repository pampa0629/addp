package envi

import (
	"context"
	"fmt"
	"path"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/shared/rastermedia"
)

type Plugin struct{}

func (*Plugin) Format() format.FormatType { return format.FormatENVI }
func (*Plugin) Descriptor() format.FormatDescriptor {
	// Neither the raw extension nor a header signature identifies the primary.
	return format.FormatDescriptor{ID: "builtin-envi", Format: format.FormatENVI, I18nKey: "format.envi", DataType: datatype.Media, Layouts: []string{format.LayoutMulti}}
}
func (*Plugin) RequiredMediaInfoOperators() []string { return []string{"raster_inspect"} }
func (*Plugin) BindMediaInfoProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, tenant uint, plan workflowaccess.SourcePlan) (format.BoundMediaInfoProvider, error) {
	bound, err := rastermedia.Bind(format.FormatENVI, []string{"ENVI"}, runtime, conn, tenant, plan)
	if err != nil {
		return nil, err
	}
	member := plan.Source.RelatedFiles[0].Access
	name := member.Object
	if member.Method == workflowaccess.MethodMountedPath {
		name = member.Path
	}
	expected, _ := plan.Source.Metadata["expected_header_sha256"].(string)
	return &boundProvider{BoundMediaInfoProvider: bound, headerName: path.Base(name), headerSHA: expected}, nil
}

type boundProvider struct {
	format.BoundMediaInfoProvider
	headerName, headerSHA string
}

func (p *boundProvider) DescribeMedia(ctx context.Context) (*format.MediaDescribeResult, error) {
	result, err := p.BoundMediaInfoProvider.DescribeMedia(ctx)
	if err != nil {
		return nil, err
	}
	info := result.FormatInfo
	if info["profile"] != Profile || info["composition_complete"] != true || info["header_name"] != p.headerName || (p.headerSHA != "" && info["header_sha256"] != p.headerSHA) {
		return nil, fmt.Errorf("ENVI inspection does not prove the frozen exact pair")
	}
	return result, nil
}

func init() {
	if err := format.RegisterFormatPlugin(&Plugin{}); err != nil {
		panic(err)
	}
}
