package service

import (
	"context"
	"reflect"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
)

func TestRasterImageSaveResolvesExactTarget(t *testing.T) {
	for _, engineType := range []string{"nfs", "minio"} {
		for _, tc := range []struct {
			profile, name, format, mime string
			valid                       bool
		}{
			{"png", "ordinary.PNG", "png", "image/png", true},
			{"jpeg", "ordinary.jpg", "jpeg", "image/jpeg", true},
			{"jpeg", "ordinary.JPEG", "jpeg", "image/jpeg", true},
			{"geotiff", "ordinary.tiff", "tiff", "image/tiff", true},
			{"cog", "ordinary.tif", "tiff", "image/tiff", true},
			{"", "ordinary.tif", "tiff", "image/tiff", true},
			{"png", "ordinary.tif", "", "", false},
			{"jpeg", "ordinary.png", "", "", false},
			{"geotiff", "ordinary.png", "", "", false},
			{"hfa", "ordinary.img", "", "", false},
			{"png", "ordinary", "", "", false},
		} {
			t.Run(engineType+"/"+tc.profile+"/"+tc.name, func(t *testing.T) {
				svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
					2: {ID: 2, EngineType: engineType, LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{
						"mount_path": "/data", "endpoint": "target:9000", "access_key": "target-key", "secret_key": "target-secret"}},
				})
				params := map[string]interface{}{"input_raster": map[string]interface{}{"$ref": "load"},
					"target_parent_locator": "addp://engine/2/path/business/results?type=prefix", "target_name": tc.name,
					"write_mode": "create", "jpeg_quality": 90}
				if tc.profile != "" {
					params["profile"] = tc.profile
				}
				workflow := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{
					"id": "save", "operator": "raster_save", "params": params}}}
				result, targets, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, "geopython_workflow", workflow)
				if !tc.valid {
					if err == nil {
						t.Fatal("inconsistent image target accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				tasks, _ := workflowTasksFromInterface(result["tasks"])
				resolved := tasks[0]["params"].(map[string]interface{})
				plan := resolved["access_plan"].(commonModels.JSONMap)
				target := plan["target"].(commonModels.JSONMap)
				if plan["source"] != nil || target["format"] != tc.format || target["name"] != tc.name || target["content_type"] != tc.mime {
					t.Fatalf("incorrect target-only plan: %#v", plan)
				}
				if resolved["jpeg_quality"] != float64(90) || len(targets) != 1 || targets[0].WriteMode != "create" || !strings.Contains(targets[0].Locator, tc.name) {
					t.Fatalf("incorrect saved parameters or stable output: %#v %#v", resolved, targets)
				}
			})
		}
	}
}

func TestRasterImagePublicPresentationKeepsOneRuntimeSchema(t *testing.T) {
	runtime := []commonModels.OperatorDescriptor{{ID: "raster_save", Name: "raster_save", Parameters: []commonModels.ParameterDescriptor{
		{Name: "access_plan", Type: "object"}, {Name: "input_raster", Type: "raster", ParamType: "input"},
		{Name: "profile", Type: "string", Default: "geotiff", Enum: []string{"geotiff", "cog", "png", "jpeg"}},
		{Name: "jpeg_quality", Type: "integer", Default: 90, ShowWhen: map[string]interface{}{"profile": "jpeg"}},
	}}}
	for _, lang := range []string{"zh-cn", "en"} {
		operators := publicWorkflowOperators(testWorkflowRuntimeEngine("geopython_workflow"), runtime)
		operators[0].LocalizeAdapterParameters("geopython_workflow", lang)
		counts := map[string]int{}
		for _, p := range operators[0].PublicParameters {
			counts[p.Name]++
			if p.Name == "target_resource" && (p.UIConfig["target_name_extension"] != "" || !strings.Contains(p.Description, "JPEG")) {
				t.Fatalf("image output is constrained to TIFF: %#v", p)
			}
			if p.Name == "profile" && (!reflect.DeepEqual(p.Enum, runtime[0].Parameters[2].Enum) || p.Default != "geotiff" || p.DisplayName == "") {
				t.Fatalf("runtime profile schema was replaced: %#v", p)
			}
			if p.Name == "jpeg_quality" && (p.Default != 90 || p.Type != "integer" || p.DisplayName == "" || !strings.Contains(p.Description, "90")) {
				t.Fatalf("JPEG quality schema/presentation invalid: %#v", p)
			}
		}
		if counts["profile"] != 1 || counts["jpeg_quality"] != 1 || counts["access_plan"] != 0 {
			t.Fatalf("duplicate/private image parameters: %#v", counts)
		}
	}
}
