package grib

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
)

type Plugin struct{}

func (*Plugin) Format() format.FormatType { return format.FormatGRIB }
func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{ID: "builtin-grib", Format: p.Format(), I18nKey: "format.grib", DataType: datatype.Container, Layouts: []string{format.LayoutSingle}, Identification: format.FormatIdentification{Extensions: []string{".grib", ".grb", ".grib2", ".grb2"}, MimeTypes: []string{"application/x-grib"}, ContentSignatures: []string{"hex:47524942"}}}
}
func (*Plugin) RequiredContainerInfoOperators() []string  { return []string{"grib.inspect"} }
func (*Plugin) RequiredContainerSliceOperators() []string { return []string{"grib.read_slice"} }
func (*Plugin) BindContainerInfoProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, plan workflowaccess.SourcePlan) (format.BoundContainerInfoProvider, error) {
	source, err := format.BindContainerRuntimeSource(runtime, conn, plan, format.FormatGRIB)
	if err != nil {
		return nil, err
	}
	return &bound{source: source}, nil
}

type bound struct {
	source *format.ContainerRuntimeSource
}

func (*bound) Format() format.FormatType { return format.FormatGRIB }

func validField(name string, v map[string]interface{}) bool {
	var field struct {
		Name           string                        `json:"name"`
		Type           string                        `json:"type"`
		ValueMode      string                        `json:"value_mode"`
		Dimensions     []struct{ Name, Size string } `json:"dimensions"`
		SliceSupported bool                          `json:"slice_supported"`
	}
	raw, err := json.Marshal(v)
	if err != nil || json.Unmarshal(raw, &field) != nil {
		return false
	}
	if field.Name != name || field.Type != "Float64" || field.ValueMode != "decoded_native_units" || !field.SliceSupported || len(field.Dimensions) != 2 {
		return false
	}
	points := uint64(1)
	for i, d := range field.Dimensions {
		n, err := strconv.ParseUint(d.Size, 10, 64)
		if err != nil || n < 2 || n > 1<<24 || strconv.FormatUint(n, 10) != d.Size || d.Name != []string{"y", "x"}[i] {
			return false
		}
		points *= n
	}
	return points <= 1<<24
}
func (b *bound) DescribeContainer(ctx context.Context, _ *format.ParseOptions) (*format.ContainerDescribeResult, error) {
	payload, err := b.source.Invoke(ctx, "inspect", nil)
	if err != nil {
		return nil, err
	}
	var description struct {
		Container  *datatype.ContainerInfo `json:"container"`
		FormatInfo map[string]interface{}  `json:"format_info"`
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &description); err != nil {
		return nil, err
	}
	info := description.FormatInfo
	c := description.Container
	if payload["schema_version"] != "gdal.grib.inspect/v1" || info["driver"] != "GRIB" || info["encoding"] != "GRIB2" || info["profile"] != "grib2_regular_fields/v1" || info["value_mode"] != "decoded_native_units" || info["scan_complete"] != true || !format.ValidContainerSourceVersion(info["source_version"]) || c == nil || c.ChildCount != len(c.Children) || c.ChildCount < 1 || c.ChildCount > 128 || c.ResourceCount != 1 {
		return nil, fmt.Errorf("GRIB inspection proof incomplete")
	}
	for i, child := range c.Children {
		if child.Name != "message-"+strconv.Itoa(i+1) || child.ChildKind != "array" || child.Format != "grib" || child.DataType != datatype.Media || !validField(child.Name, child.Native) {
			return nil, fmt.Errorf("GRIB field index invalid")
		}
	}
	return &format.ContainerDescribeResult{Container: c, FormatInfo: info}, nil
}
func (b *bound) ReadGridSlice(ctx context.Context, version string, selection format.GridSliceSelection) (*format.GridSliceResult, error) {
	ordinal, ordinalErr := strconv.Atoi(strings.TrimPrefix(selection.Variable, "message-"))
	if ordinalErr != nil || ordinal < 1 || ordinal > 128 || selection.Variable != "message-"+strconv.Itoa(ordinal) {
		return nil, format.ErrGridSliceSelection
	}
	if !format.ValidContainerSourceVersion(version) || selection.Validate() != nil || len(selection.Indices) != 0 {
		return nil, format.ErrGridSliceSelection
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
	if result.SchemaVersion != "gdal.grib.slice/v1" || result.Format != "grib" || result.SourceVersion != version || result.ValueMode != "decoded_native_units" || !reflect.DeepEqual(result.Selection, selection) || !validField(selection.Variable, result.Variable) || result.Spatial != nil || len(result.Values) != selection.Height || len(result.Valid) != selection.Height {
		return nil, fmt.Errorf("GRIB slice binding incomplete")
	}
	dimensions := result.Variable["dimensions"].([]interface{})
	height, _ := strconv.Atoi(dimensions[0].(map[string]interface{})["size"].(string))
	width, _ := strconv.Atoi(dimensions[1].(map[string]interface{})["size"].(string))
	if selection.X > int64(width-selection.Width) || selection.Y > int64(height-selection.Height) {
		return nil, fmt.Errorf("GRIB slice exceeds selected field")
	}
	for y, row := range result.Values {
		if len(row) != selection.Width || len(result.Valid[y]) != selection.Width {
			return nil, fmt.Errorf("GRIB slice shape mismatch")
		}
		for x, text := range row {
			v, err := strconv.ParseFloat(text, 64)
			if err != nil || len(text) > 128 || result.Valid[y][x] && (math.IsNaN(v) || math.IsInf(v, 0)) || !result.Valid[y][x] && text != "NaN" {
				return nil, fmt.Errorf("GRIB numeric value or mask invalid")
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
