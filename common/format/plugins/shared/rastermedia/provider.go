package rastermedia

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

func Bind(formatType format.FormatType, driver string, runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, tenantID uint, plan workflowaccess.SourcePlan) (format.BoundMediaInfoProvider, error) {
	if runtime == nil || tenantID == 0 || len(conn) == 0 {
		return nil, fmt.Errorf("%s inspection requires a runtime, connection and tenant", formatType)
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if plan.Source.Format != string(formatType) || plan.Source.Kind != workflowaccess.KindFile {
		return nil, fmt.Errorf("%s inspection requires a matching file source plan", formatType)
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
	return &boundProvider{runtime: runtime, conn: frozen.Conn, plan: frozen.Plan, tenantID: tenantID, formatType: formatType, driver: driver}, nil
}

type boundProvider struct {
	runtime    engineplugin.WorkflowRuntimeProvider
	conn       engineplugin.ConnectionInfo
	plan       workflowaccess.SourcePlan
	tenantID   uint
	formatType format.FormatType
	driver     string
}

func (p *boundProvider) Format() format.FormatType { return p.formatType }
func (p *boundProvider) DescribeMedia(ctx context.Context) (*format.MediaDescribeResult, error) {
	result, err := p.runtime.InvokeOperator(ctx, p.conn, "raster_inspect", engineplugin.OperatorInvokeRequest{
		TenantID: p.tenantID, Params: map[string]interface{}{"access_plan": p.plan.JSONMap()},
	})
	if err != nil {
		return nil, fmt.Errorf("%s inspection runtime failed: %w", p.formatType, err)
	}
	if result == nil || result.Status != "success" {
		return nil, fmt.Errorf("%s inspection did not succeed", p.formatType)
	}
	payload, ok := result.Result["result"].(map[string]interface{})
	if !ok || payload["schema_version"] != inspectionSchema || payload["format"] != string(p.formatType) {
		return nil, fmt.Errorf("invalid %s inspection response contract", p.formatType)
	}
	encoded, err := json.Marshal(payload["description"])
	if err != nil {
		return nil, err
	}
	var description format.MediaDescribeResult
	if err := json.Unmarshal(encoded, &description); err != nil {
		return nil, err
	}
	if description.Media == nil || description.Media.Kind != datatype.MediaKindImage || description.Media.Width <= 0 || description.Media.Height <= 0 || description.Media.Encoding != p.driver || description.FormatInfo["scan_complete"] != true {
		return nil, fmt.Errorf("%s inspection returned incomplete media facts", p.formatType)
	}
	return &description, nil
}
