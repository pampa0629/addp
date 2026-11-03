package grib

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
)

type runtimeStub struct {
	engineplugin.WorkflowRuntimeProvider
	payload map[string]interface{}
	path    string
	calls   int
}

func (r *runtimeStub) InvokeOperator(_ context.Context, _ engineplugin.ConnectionInfo, _ string, request engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error) {
	r.calls++
	raw, _ := json.Marshal(request.Params["access_plan"])
	var plan workflowaccess.SourcePlan
	_ = json.Unmarshal(raw, &plan)
	r.path = plan.Source.Access.Path
	return &engineplugin.OperatorInvokeResult{Status: "success", Result: map[string]interface{}{"result": r.payload}}, nil
}
func bind(t *testing.T, r *runtimeStub) *bound {
	t.Helper()
	plan, err := workflowaccess.NewSourcePlan(workflowaccess.Source{Kind: "file", Format: "grib", Access: workflowaccess.Access{Method: "mounted_path", Path: "/source/input.grb2"}})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := (&Plugin{}).BindContainerInfoProvider(r, engineplugin.ConnectionInfo{"host": "runtime"}, plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.Source.Access.Path = "/other"
	return provider.(*bound)
}
func field() map[string]interface{} {
	return map[string]interface{}{"name": "message-1", "type": "Float64", "value_mode": "decoded_native_units", "dimensions": []map[string]string{{"name": "y", "size": "3"}, {"name": "x", "size": "4"}}, "slice_supported": true}
}
func description() map[string]interface{} {
	return map[string]interface{}{"schema_version": "gdal.grib.inspect/v1", "format": "grib", "container": datatype.ContainerInfo{ChildCount: 1, ResourceCount: 1, Children: []datatype.ContainerChildInfo{{Name: "message-1", ChildKind: "array", DataType: datatype.Media, Format: "grib", Native: field()}}}, "format_info": map[string]interface{}{"driver": "GRIB", "encoding": "GRIB2", "profile": "grib2_regular_fields/v1", "scan_complete": true, "source_version": "sha256:" + strings.Repeat("a", 64), "value_mode": "decoded_native_units"}}
}
func TestGRIBExactBindingAndCompleteCatalog(t *testing.T) {
	r := &runtimeStub{payload: description()}
	b := bind(t, r)
	got, err := b.DescribeContainer(context.Background(), nil)
	if err != nil || got.Container.ChildCount != 1 || r.path != "/source/input.grb2" {
		t.Fatal(got, err, r.path)
	}
	for _, key := range []string{"driver", "encoding", "profile", "source_version", "scan_complete", "value_mode"} {
		t.Run(key, func(t *testing.T) {
			r.payload = description()
			r.payload["format_info"].(map[string]interface{})[key] = nil
			if _, err := b.DescribeContainer(context.Background(), nil); err == nil {
				t.Fatal("incomplete proof accepted")
			}
		})
	}
	r.payload = description()
	r.payload["container"].(datatype.ContainerInfo).Children[0].Name = "message-01"
	if _, err := b.DescribeContainer(context.Background(), nil); err == nil {
		t.Fatal("ambiguous message identity accepted")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("SKP admission opened")
	}
}
func TestGRIBSliceUsesDecodedUnitsAndIndependentMissingMask(t *testing.T) {
	version := "sha256:" + strings.Repeat("a", 64)
	selection := format.GridSliceSelection{Variable: "message-1", Indices: []int64{}, Width: 2, Height: 1}
	payload := func() map[string]interface{} {
		return map[string]interface{}{"schema_version": "gdal.grib.slice/v1", "format": "grib", "source_version": version, "selection": selection, "variable": field(), "value_mode": "decoded_native_units", "values": [][]string{{"9999.0", "NaN"}}, "valid": [][]bool{{true, false}}, "spatial": nil}
	}
	r := &runtimeStub{payload: payload()}
	b := bind(t, r)
	result, err := b.ReadGridSlice(context.Background(), version, selection)
	if err != nil || result.Values[0][0] != "9999.0" || !result.Valid[0][0] || result.Valid[0][1] {
		t.Fatal(result, err)
	}
	for _, change := range []func(map[string]interface{}){
		func(p map[string]interface{}) { p["value_mode"] = "raw_storage" }, func(p map[string]interface{}) { p["source_version"] = "sha256:" + strings.Repeat("b", 64) }, func(p map[string]interface{}) { p["values"] = [][]string{{"NaN", "NaN"}} }, func(p map[string]interface{}) { p["values"] = [][]string{{"9999", "9999"}} }, func(p map[string]interface{}) { p["valid"] = [][]bool{{true}} }, func(p map[string]interface{}) { p["variable"] = map[string]interface{}{"name": "message-2"} }, func(p map[string]interface{}) { p["spatial"] = map[string]interface{}{} },
	} {
		r.payload = payload()
		change(r.payload)
		if _, err := b.ReadGridSlice(context.Background(), version, selection); err == nil {
			t.Fatal("invalid slice accepted", r.payload)
		}
	}
	for code, want := range map[string]error{"GRIB_SOURCE_CHANGED": format.ErrContainerSourceChanged, "GRIB_SLICE_INVALID": format.ErrGridSliceSelection} {
		r.payload = map[string]interface{}{"error_code": code}
		if _, err := b.ReadGridSlice(context.Background(), version, selection); !errors.Is(err, want) {
			t.Fatal(code, err)
		}
	}
}
func TestGRIBSliceRejectsOutOfGridAndNonCanonicalMessageBeforeReturning(t *testing.T) {
	version := "sha256:" + strings.Repeat("a", 64)
	r := &runtimeStub{}
	b := bind(t, r)
	for _, name := range []string{"message-0", "message-01", "message-129", "TMP", "message-1/../../secret"} {
		if _, err := b.ReadGridSlice(context.Background(), version, format.GridSliceSelection{Variable: name, Width: 1, Height: 1}); !errors.Is(err, format.ErrGridSliceSelection) {
			t.Fatal(name, err)
		}
	}
	if r.calls != 0 {
		t.Fatal("invalid selector reached runtime")
	}
	for _, x := range []int64{4, 1 << 62} {
		selection := format.GridSliceSelection{Variable: "message-1", Indices: []int64{}, X: x, Width: 1, Height: 1}
		r.payload = map[string]interface{}{"schema_version": "gdal.grib.slice/v1", "format": "grib", "source_version": version, "selection": selection, "variable": field(), "value_mode": "decoded_native_units", "values": [][]string{{"1.0"}}, "valid": [][]bool{{true}}}
		if _, err := b.ReadGridSlice(context.Background(), version, selection); err == nil {
			t.Fatal("out-of-grid runtime response accepted", x)
		}
	}
	oversized := field()
	oversized["dimensions"] = []map[string]string{{"name": "y", "size": "16777216"}, {"name": "x", "size": "16777216"}}
	if validField("message-1", oversized) {
		t.Fatal("field point budget ignored")
	}
}

func TestGRIBDescriptorRecognizesContentAndOnlyDeclaresReaders(t *testing.T) {
	if got := format.DetectFormat("opaque.bin", []byte("GRIB\x00\x00\x00\x02")); got != format.FormatGRIB {
		t.Fatal(got)
	}
	d := (&Plugin{}).Descriptor()
	if d.DataType != datatype.Container || len(d.Layouts) != 1 || d.Layouts[0] != "single" {
		t.Fatal(d)
	}
}
