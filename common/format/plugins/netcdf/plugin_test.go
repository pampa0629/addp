package netcdf

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
	response map[string]interface{}
	calls    int
	path     string
}

func (r *runtimeStub) InvokeOperator(_ context.Context, _ engineplugin.ConnectionInfo, _ string, request engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error) {
	r.calls++
	raw, _ := json.Marshal(request.Params["access_plan"])
	var plan workflowaccess.SourcePlan
	_ = json.Unmarshal(raw, &plan)
	r.path = plan.Source.Access.Path
	return &engineplugin.OperatorInvokeResult{Status: "success", Result: map[string]interface{}{"result": r.response}}, nil
}
func boundFixture(t *testing.T, r *runtimeStub) *bound {
	t.Helper()
	plan, err := workflowaccess.NewSourcePlan(workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "netcdf", Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/exact.nc"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := (&Plugin{}).BindContainerInfoProvider(r, engineplugin.ConnectionInfo{"host": "runtime"}, plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.Source.Access.Path = "/data/other.nc"
	return p.(*bound)
}
func TestNetCDFDescribeAndBindingRejectIncompleteFacts(t *testing.T) {
	r := &runtimeStub{response: map[string]interface{}{"schema_version": "gdal.netcdf.inspect/v1", "format": "netcdf", "container": datatype.ContainerInfo{ChildCount: 1, Children: []datatype.ContainerChildInfo{{Name: "/v", ChildKind: "array", DataType: datatype.Media, Format: "netcdf", Native: map[string]interface{}{"name": "/v"}}}}, "format_info": map[string]interface{}{"driver": "netCDF", "scan_complete": true, "source_version": "sha256:" + strings.Repeat("a", 64)}}}
	b := boundFixture(t, r)
	described, err := b.DescribeContainer(context.Background(), nil)
	if err != nil || described.Container.ChildCount != 1 || r.path != "/data/exact.nc" {
		t.Fatal(described, err, r.path)
	}
	r.response["format_info"].(map[string]interface{})["scan_complete"] = false
	if _, err = b.DescribeContainer(context.Background(), nil); err == nil {
		t.Fatal("incomplete inspection accepted")
	}
	if _, err := format.GetModel3DCompositionProvider(format.FormatSKP); err == nil {
		t.Fatal("unrelated SKP admission opened")
	}
}
func TestNetCDFSliceVersionShapeAndOriginalNumericText(t *testing.T) {
	version := "sha256:" + strings.Repeat("a", 64)
	selection := format.GridSliceSelection{Variable: "/v", Indices: []int64{}, Width: 2, Height: 1}
	r := &runtimeStub{response: map[string]interface{}{"schema_version": "gdal.netcdf.slice/v1", "format": "netcdf", "source_version": version, "selection": selection, "variable": map[string]interface{}{"name": "/v"}, "values": [][]string{{"9007199254740993", "18446744073709551615"}}, "valid": [][]bool{{true, false}}, "value_mode": "raw_storage"}}
	b := boundFixture(t, r)
	got, err := b.ReadGridSlice(context.Background(), version, selection)
	if err != nil || got.Values[0][0] != "9007199254740993" || got.Valid[0][1] {
		t.Fatal(got, err)
	}
	r.response["values"] = [][]string{{"1"}}
	if _, err = b.ReadGridSlice(context.Background(), version, selection); err == nil {
		t.Fatal("truncated window accepted")
	}
	r.response = map[string]interface{}{"error_code": "NETCDF_SOURCE_CHANGED"}
	if _, err = b.ReadGridSlice(context.Background(), version, selection); !errors.Is(err, format.ErrContainerSourceChanged) {
		t.Fatal(err)
	}
	calls := r.calls
	selection.Width = 129
	if _, err = b.ReadGridSlice(context.Background(), version, selection); err == nil || r.calls != calls {
		t.Fatal("unbounded request reached runtime")
	}
}
