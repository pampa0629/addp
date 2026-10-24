package rastermedia_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/img"
	"github.com/addp/common/format/plugins/shared/rastermedia"
)

type runtimeStub struct {
	engineplugin.WorkflowRuntimeProvider
	invoke func(engineplugin.ConnectionInfo, engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error)
}

func (r runtimeStub) InvokeOperator(_ context.Context, conn engineplugin.ConnectionInfo, name string, req engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error) {
	if name != "raster_inspect" {
		panic("unexpected operator")
	}
	return r.invoke(conn, req)
}

func TestNativeInspectionBindsTenantAndFreezesSource(t *testing.T) {
	for _, spec := range []struct {
		format format.FormatType
		driver string
	}{{format.FormatIMG, "HFA"}, {format.FormatJPEG2000, "JP2OpenJPEG"}} {
		t.Run(string(spec.format), func(t *testing.T) {
			if got := format.DetectFormat("scene.IMG", nil); got != format.FormatIMG {
				t.Fatal(got)
			}
			if got := format.DetectFormat("no-extension", []byte("EHFA_HEADER_TAG\x00")); got != format.FormatIMG {
				t.Fatal(got)
			}
			plan := workflowaccess.SourcePlan{SchemaVersion: workflowaccess.SchemaVersion, Source: workflowaccess.Source{
				Kind: workflowaccess.KindFile, Format: string(spec.format), Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/input.img"}}}
			conn := engineplugin.ConnectionInfo{"host": "runtime"}
			runtime := runtimeStub{invoke: func(connection engineplugin.ConnectionInfo, req engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error) {
				if connection["host"] != "runtime" || req.TenantID != 7 {
					t.Fatal("lost tenant or mutable connection")
				}
				source, _ := json.Marshal(req.Params["access_plan"])
				if !strings.Contains(string(source), "/input.img") || strings.Contains(string(source), "/wrong.img") {
					t.Fatal("source plan changed after binding")
				}
				return &engineplugin.OperatorInvokeResult{Status: "success", Result: map[string]interface{}{"result": map[string]interface{}{
					"schema_version": "gdal.raster.inspect/v1", "format": string(spec.format), "description": map[string]interface{}{
						"media":       map[string]interface{}{"kind": "image", "encoding": spec.driver, "width": 32, "height": 16},
						"format_info": map[string]interface{}{"scan_complete": true, "band_count": 3},
					}}}}, nil
			}}
			provider, err := rastermedia.Bind(spec.format, spec.driver, runtime, conn, 7, plan)
			if err != nil {
				t.Fatal(err)
			}
			conn["host"] = "wrong"
			plan.Source.Access.Path = "/wrong.img"
			description, err := provider.DescribeMedia(context.Background())
			if err != nil || description == nil || description.Media.Width != 32 {
				t.Fatalf("%+v %v", description, err)
			}
		})
	}
}

func TestIMGInspectionRejectsIncompleteOrWrongContracts(t *testing.T) {
	for _, payload := range []map[string]interface{}{
		{}, {"schema_version": "gdal.raster.inspect/v1", "format": "tiff"},
		{"schema_version": "gdal.raster.inspect/v1", "format": "img", "description": map[string]interface{}{"media": map[string]interface{}{"width": 32, "height": 16}}},
	} {
		runtime := runtimeStub{invoke: func(engineplugin.ConnectionInfo, engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error) {
			return &engineplugin.OperatorInvokeResult{Status: "success", Result: map[string]interface{}{"result": payload}}, nil
		}}
		provider, err := rastermedia.Bind(format.FormatIMG, "HFA", runtime, engineplugin.ConnectionInfo{"host": "runtime"}, 7, workflowaccess.SourcePlan{SchemaVersion: workflowaccess.SchemaVersion, Source: workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "img", Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/input.img"}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := provider.DescribeMedia(context.Background()); err == nil {
			t.Fatal("accepted incomplete IMG inspection")
		}
	}
}
