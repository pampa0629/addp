package netcdf

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
)

type Plugin struct{}

func (*Plugin) Format() format.FormatType { return format.FormatNetCDF }
func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{ID: "builtin-netcdf", Format: p.Format(), I18nKey: "format.netcdf", DataType: datatype.Container, Layouts: []string{format.LayoutSingle}, Identification: format.FormatIdentification{Extensions: []string{".nc", ".cdf"}, MimeTypes: []string{"application/x-netcdf"}, ContentSignatures: []string{"hex:43444601", "hex:43444602", "hex:43444605"}}}
}
func (*Plugin) RequiredContainerInfoOperators() []string  { return []string{"netcdf.inspect"} }
func (*Plugin) RequiredContainerSliceOperators() []string { return []string{"netcdf.read_slice"} }
func (*Plugin) BindContainerInfoProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, plan workflowaccess.SourcePlan) (format.BoundContainerInfoProvider, error) {
	if runtime == nil || len(conn) == 0 {
		return nil, fmt.Errorf("NetCDF requires a runtime connection")
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if plan.Source.Kind != workflowaccess.KindFile || plan.Source.Format != "netcdf" {
		return nil, fmt.Errorf("NetCDF requires an exact file source")
	}
	raw, err := json.Marshal(struct {
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
	if err = json.Unmarshal(raw, &frozen); err != nil {
		return nil, err
	}
	return &bound{runtime: runtime, conn: frozen.Conn, plan: frozen.Plan}, nil
}

type bound struct {
	runtime engineplugin.WorkflowRuntimeProvider
	conn    engineplugin.ConnectionInfo
	plan    workflowaccess.SourcePlan
}

func (*bound) Format() format.FormatType { return format.FormatNetCDF }
func (b *bound) invoke(ctx context.Context, name string, params map[string]interface{}) (map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	params["access_plan"] = b.plan.JSONMap()
	result, err := b.runtime.InvokeOperator(ctx, b.conn, name, engineplugin.OperatorInvokeRequest{Params: params})
	if err != nil {
		return nil, err
	}
	if result == nil || result.Status != "success" {
		return nil, fmt.Errorf("NetCDF runtime did not succeed")
	}
	payload, ok := result.Result["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("NetCDF runtime omitted result")
	}
	if payload["error_code"] == "NETCDF_SLICE_INVALID" {
		return nil, format.ErrGridSliceSelection
	}
	if payload["error_code"] == "NETCDF_SOURCE_CHANGED" {
		return nil, format.ErrContainerSourceChanged
	}
	if payload["format"] != "netcdf" {
		return nil, fmt.Errorf("NetCDF runtime format mismatch")
	}
	return payload, nil
}
func (b *bound) DescribeContainer(ctx context.Context, _ *format.ParseOptions) (*format.ContainerDescribeResult, error) {
	payload, err := b.invoke(ctx, "netcdf.inspect", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	if payload["schema_version"] != "gdal.netcdf.inspect/v1" {
		return nil, fmt.Errorf("NetCDF inspection schema mismatch")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var description struct {
		Container  *datatype.ContainerInfo `json:"container"`
		FormatInfo map[string]interface{}  `json:"format_info"`
	}
	if err = json.Unmarshal(raw, &description); err != nil {
		return nil, err
	}
	if description.Container == nil || description.Container.ChildCount != len(description.Container.Children) || len(description.Container.Children) > 256 || description.FormatInfo["scan_complete"] != true || description.FormatInfo["driver"] != "netCDF" || !validVersion(description.FormatInfo["source_version"]) {
		return nil, fmt.Errorf("NetCDF inspection facts incomplete")
	}
	seen := map[string]bool{}
	for _, child := range description.Container.Children {
		if child.Name == "" || seen[child.Name] || child.ChildKind != "array" || child.Format != "netcdf" || child.Native["name"] != child.Name || child.DataType != datatype.Media && child.DataType != datatype.Unknown {
			return nil, fmt.Errorf("invalid NetCDF variable facts")
		}
		seen[child.Name] = true
	}
	return &format.ContainerDescribeResult{Container: description.Container, FormatInfo: description.FormatInfo}, nil
}
func validVersion(value interface{}) bool {
	s, ok := value.(string)
	if !ok || len(s) != 71 || !strings.HasPrefix(s, "sha256:") {
		return false
	}
	for _, r := range s[7:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func (b *bound) ReadGridSlice(ctx context.Context, version string, selection format.GridSliceSelection) (*format.GridSliceResult, error) {
	if !validVersion(version) {
		return nil, fmt.Errorf("NetCDF requires the scanned content version")
	}
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	payload, err := b.invoke(ctx, "netcdf.read_slice", map[string]interface{}{"source_version": version, "selection": selection})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var result format.GridSliceResult
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result.SchemaVersion != "gdal.netcdf.slice/v1" || result.SourceVersion != version || result.ValueMode != "raw_storage" || !reflect.DeepEqual(result.Selection, selection) || result.Variable["name"] != selection.Variable || len(result.Values) != selection.Height || len(result.Valid) != selection.Height {
		return nil, fmt.Errorf("NetCDF slice binding or shape mismatch")
	}
	for i, row := range result.Values {
		if len(row) != selection.Width || len(result.Valid[i]) != selection.Width {
			return nil, fmt.Errorf("NetCDF slice incomplete")
		}
		for _, v := range row {
			if v == "" || len(v) > 128 {
				return nil, fmt.Errorf("NetCDF numeric text invalid")
			}
			if _, err := strconv.ParseFloat(v, 64); err != nil {
				return nil, fmt.Errorf("NetCDF numeric text invalid")
			}
		}
	}
	return &result, nil
}
func init() {
	if err := format.RegisterFormatPlugin(&Plugin{}); err != nil {
		panic(err)
	}
}

var _ format.RuntimeContainerSliceProviderFactory = (*Plugin)(nil)
var _ format.ContainerSliceReader = (*bound)(nil)
