package service

import (
	"context"
	"testing"

	commonModels "github.com/addp/common/models"
)

func TestSurferGridReadUsesOneSourcePlanAndDoesNotOpenGridExport(t *testing.T) {
	for _, engineType := range []string{"nfs", "minio"} {
		t.Run(engineType, func(t *testing.T) {
			locatorType := "file"
			if engineType == "minio" {
				locatorType = "object"
			}
			svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
				1: {ID: 1, EngineType: engineType, LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{
					"mount_path": "/data", "endpoint": "source:9000", "access_key": "source-key", "secret_key": "source-secret"}},
			})
			workflow := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{
				"id": "load", "operator": "raster_load", "params": map[string]interface{}{
					"locator": "addp://engine/1/path/business/height.grd?type=" + locatorType, "source_crs": "EPSG:32650"}}}}
			result, targets, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, "geopython_workflow", workflow)
			if err != nil {
				t.Fatal(err)
			}
			tasks, _ := workflowTasksFromInterface(result["tasks"])
			params := tasks[0]["params"].(map[string]interface{})
			plan := params["access_plan"].(commonModels.JSONMap)
			source := plan["source"].(commonModels.JSONMap)
			if source["format"] != "grd" || source["kind"] != "file" || plan["target"] != nil || len(targets) != 0 || params["source_crs"] != "EPSG:32650" {
				t.Fatalf("read-only GRD plan changed: %#v, %#v", params, targets)
			}
		})
	}
	if _, allowed := workflowRasterAdapterSpec("raster_save").AccessPlan.TargetFormatValues["grd"]; allowed {
		t.Fatal("read support opened GRD output")
	}
}
