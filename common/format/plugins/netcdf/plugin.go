package netcdf

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
)

type Plugin struct{}

func (*Plugin) Format() format.FormatType { return format.FormatNetCDF }
func (p *Plugin) Descriptor() format.FormatDescriptor {
	// HDF5's shared signature is not a NetCDF identity. The exact .nc/.cdf
	// candidate is refined by the Runtime's NetCDF-4 composition inspection.
	return format.FormatDescriptor{ID: "builtin-netcdf", Format: p.Format(), I18nKey: "format.netcdf", DataType: datatype.Container, Layouts: []string{format.LayoutSingle}, Identification: format.FormatIdentification{Extensions: []string{".nc", ".cdf", ".nc4"}, MimeTypes: []string{"application/x-netcdf"}, ContentSignatures: []string{"hex:43444601", "hex:43444602", "hex:43444605"}}}
}
func (*Plugin) RequiredContainerInfoOperators() []string  { return []string{"netcdf.inspect"} }
func (*Plugin) RequiredContainerSliceOperators() []string { return []string{"netcdf.read_slice"} }
func (*Plugin) BindContainerInfoProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, plan workflowaccess.SourcePlan) (format.BoundContainerInfoProvider, error) {
	source, err := format.BindContainerRuntimeSource(runtime, conn, plan, format.FormatNetCDF)
	if err != nil {
		return nil, err
	}
	return &bound{source: source}, nil
}

type bound struct {
	source *format.ContainerRuntimeSource
}

func (*bound) Format() format.FormatType { return format.FormatNetCDF }
func (b *bound) DescribeContainer(ctx context.Context, _ *format.ParseOptions) (*format.ContainerDescribeResult, error) {
	payload, err := b.source.Invoke(ctx, "inspect", map[string]interface{}{})
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
	if description.Container == nil || description.Container.ChildCount != len(description.Container.Children) || len(description.Container.Children) > 256 || description.FormatInfo["scan_complete"] != true || description.FormatInfo["driver"] != "netCDF" || !format.ValidContainerSourceVersion(description.FormatInfo["source_version"]) || !validEncoding(description.FormatInfo) {
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
func validEncoding(info map[string]interface{}) bool {
	switch info["encoding"] {
	case "CDF-1", "CDF-2", "CDF-5":
		return info["composition"] == nil
	case "NetCDF-4":
		proof, ok := info["composition"].(map[string]interface{})
		return ok && len(proof) == 2 && proof["profile"] == "root_numeric_v1" && proof["complete"] == true
	default:
		return false
	}
}
func (b *bound) ReadGridSlice(ctx context.Context, version string, selection format.GridSliceSelection) (*format.GridSliceResult, error) {
	if !format.ValidContainerSourceVersion(version) {
		return nil, fmt.Errorf("NetCDF requires the scanned content version")
	}
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	payload, err := b.source.Invoke(ctx, "read_slice", map[string]interface{}{"source_version": version, "selection": selection})
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
