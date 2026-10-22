package img

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
)

const inspectionSchema = "gdal.raster.inspect/v1"

type Plugin struct{}

func (p *Plugin) Format() format.FormatType { return format.FormatIMG }
func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{ID: "builtin-img", Format: p.Format(), I18nKey: "format.img",
		DataType: datatype.Media, Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".img"}, ContentSignatures: []string{"hex:454846415f4845414445525f54414700"}}}
}
func (p *Plugin) RequiredMediaInfoOperators() []string { return []string{"raster_inspect"} }

func (p *Plugin) BindMediaInfoProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, tenantID uint, plan workflowaccess.SourcePlan) (format.BoundMediaInfoProvider, error) {
	if runtime == nil || tenantID == 0 || len(conn) == 0 {
		return nil, fmt.Errorf("IMG inspection requires a runtime, connection and tenant")
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if plan.Source.Format != string(p.Format()) || plan.Source.Kind != workflowaccess.KindFile {
		return nil, fmt.Errorf("IMG inspection requires an img file source plan")
	}
	// Freeze mutable caller maps, including nested metadata and credentials.
	encoded, err := json.Marshal(struct {
		Conn engineplugin.ConnectionInfo
		Plan workflowaccess.SourcePlan
	}{conn, plan})
	if err != nil {
		return nil, err
	}
	var frozen struct {
		Conn engineplugin.ConnectionInfo
		Plan workflowaccess.SourcePlan
	}
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		return nil, err
	}
	return &boundProvider{runtime: runtime, conn: frozen.Conn, plan: frozen.Plan, tenantID: tenantID}, nil
}

type boundProvider struct {
	runtime  engineplugin.WorkflowRuntimeProvider
	conn     engineplugin.ConnectionInfo
	plan     workflowaccess.SourcePlan
	tenantID uint
}

func (p *boundProvider) Format() format.FormatType { return format.FormatIMG }
func (p *boundProvider) DescribeMedia(ctx context.Context) (*format.MediaDescribeResult, error) {
	result, err := p.runtime.InvokeOperator(ctx, p.conn, "raster_inspect", engineplugin.OperatorInvokeRequest{
		TenantID: p.tenantID, Params: map[string]interface{}{"access_plan": p.plan.JSONMap()},
	})
	if err != nil {
		return nil, fmt.Errorf("IMG inspection runtime failed: %w", err)
	}
	if result == nil || result.Status != "success" {
		return nil, fmt.Errorf("IMG inspection did not succeed")
	}
	payload, ok := result.Result["result"].(map[string]interface{})
	if !ok || payload["schema_version"] != inspectionSchema || payload["format"] != "img" {
		return nil, fmt.Errorf("invalid IMG inspection response contract")
	}
	encoded, err := json.Marshal(payload["description"])
	if err != nil {
		return nil, err
	}
	var description format.MediaDescribeResult
	if err := json.Unmarshal(encoded, &description); err != nil {
		return nil, err
	}
	if description.Media == nil || description.Media.Kind != datatype.MediaKindImage || description.Media.Width <= 0 || description.Media.Height <= 0 || description.Media.Encoding != "HFA" || description.FormatInfo["scan_complete"] != true {
		return nil, fmt.Errorf("IMG inspection returned incomplete media facts")
	}
	return &description, nil
}

func init() {
	if err := format.RegisterFormatPlugin(&Plugin{}); err != nil {
		panic(err)
	}
}
