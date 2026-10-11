package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/taskprovider"
	"github.com/addp/develop/backend/internal/models"
)

func rasterTileManifestFixture(completed int) map[string]interface{} {
	tiles := map[string]interface{}{}
	windows := [][]int{{0, 0, 4, 3}, {4, 0, 3, 3}, {0, 3, 4, 2}, {4, 3, 3, 2}}
	for _, window := range windows[:completed] {
		key := fmt.Sprintf("r%d_c%d", window[1], window[0])
		tiles[key] = map[string]interface{}{"name": key + ".tif", "window": window,
			"width": window[2], "height": window[3], "size_bytes": 1234, "uploaded_files": 1,
			"artifact_type": "raster", "format": "tiff", "profile": "cog"}
	}
	manifest := map[string]interface{}{"artifact_type": "raster_tiles", "source_width": 7, "source_height": 5,
		"tile_size": []int{4, 3}, "columns": 2, "rows": 2, "total_tiles": 4, "completed_tiles": completed, "tiles": tiles}
	if completed < 4 {
		window := windows[completed]
		manifest["failed_tile"] = fmt.Sprintf("r%d_c%d", window[1], window[0])
		manifest["publication"] = "unknown"
	}
	return manifest
}

func encodeRasterTileManifest(t *testing.T, manifest map[string]interface{}) string {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestRasterTileActualTargetsAndStableOutputs(t *testing.T) {
	for _, kind := range []string{"directory", "prefix"} {
		for _, completed := range []int{2, 4} {
			t.Run(fmt.Sprintf("%s/%d", kind, completed), func(t *testing.T) {
				scope := workflowProducedTarget(2, resourcetree.ResourceType(kind), []string{"business", "tiles"})
				scope.BatchScope, scope.TaskID, scope.WriteMode = true, "save", "create"
				status := "success"
				if completed < 4 {
					status = "failed"
				}
				targets, err := resolveWorkflowRasterTileTargets([]WorkflowProducedTarget{scope}, &WorkflowResponse{
					Status: status, AllResults: map[string]string{"save": encodeRasterTileManifest(t, rasterTileManifestFixture(completed))}})
				if err != nil || len(targets) != completed {
					t.Fatalf("targets=%#v err=%v", targets, err)
				}
				outputs := workflowExecutionOutputs(targets)
				children := outputs["save"].(map[string]interface{})["tiles"].(map[string]interface{})
				if len(children) != completed {
					t.Fatalf("outputs=%#v", outputs)
				}
				for _, target := range targets {
					if target.BatchScope || target.Layout != "" || target.Locator == scope.Locator || target.TileKey == "" {
						t.Fatalf("target=%#v", target)
					}
					wantType := "file"
					if kind == "prefix" {
						wantType = "object"
					}
					if target.Type != wantType {
						t.Fatalf("type=%s", target.Type)
					}
					loc, err := resourcetree.ParseURI(target.Locator)
					if err != nil || !reflect.DeepEqual(loc.Path, []string{"business", "tiles", target.TileKey + ".tif"}) {
						t.Fatalf("locator=%#v %v", loc, err)
					}
					opts := workflowProducedTargetScanOptions(target)
					if kind == "prefix" && !reflect.DeepEqual(opts.Targets, []string{target.Locator}) {
						t.Fatalf("scan=%#v", opts)
					}
					if kind == "directory" && (len(opts.RefGroups) != 1 || opts.RefGroups[0].Primary != strings.Join(target.Path, "/")) {
						t.Fatalf("scan=%#v", opts)
					}
				}
				if len(collectLineageRefs(outputs, "output")) != completed {
					t.Fatalf("lineage outputs=%#v", outputs)
				}
			})
		}
	}
}

func TestRasterTileRejectsMalformedPublicationManifest(t *testing.T) {
	cases := map[string]func(map[string]interface{}){
		"private-root": func(m map[string]interface{}) { m["workspace"] = "/private" },
		"private-tile": func(m map[string]interface{}) {
			m["tiles"].(map[string]interface{})["r0_c0"].(map[string]interface{})["locator"] = "addp://engine/99/path/escape?type=file"
		},
		"private-band": func(m map[string]interface{}) {
			m["tiles"].(map[string]interface{})["r0_c0"].(map[string]interface{})["bands"] = []interface{}{map[string]interface{}{"access_plan": "private"}}
		},
		"count":      func(m map[string]interface{}) { m["total_tiles"] = 5 },
		"completion": func(m map[string]interface{}) { m["completed_tiles"] = 3 },
		"grid":       func(m map[string]interface{}) { m["tile_size"] = []int{0, 3} },
		"source":     func(m map[string]interface{}) { m["source_width"] = int64(2147483648) },
		"escape": func(m map[string]interface{}) {
			m["tiles"].(map[string]interface{})["r0_c0"].(map[string]interface{})["name"] = "../escape.tif"
		},
		"size": func(m map[string]interface{}) {
			m["tiles"].(map[string]interface{})["r0_c0"].(map[string]interface{})["size_bytes"] = 0
		},
		"window": func(m map[string]interface{}) {
			m["tiles"].(map[string]interface{})["r0_c0"].(map[string]interface{})["window"] = []int{0, 0, 7, 5}
		},
		"format": func(m map[string]interface{}) {
			m["tiles"].(map[string]interface{})["r0_c0"].(map[string]interface{})["format"] = "vrt"
		},
		"profile": func(m map[string]interface{}) {
			m["tiles"].(map[string]interface{})["r0_c0"].(map[string]interface{})["profile"] = "other"
		},
		"missing":              func(m map[string]interface{}) { delete(m["tiles"].(map[string]interface{}), "r0_c0") },
		"success-with-failure": func(m map[string]interface{}) { m["publication"] = "unknown" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := rasterTileManifestFixture(4)
			change(manifest)
			if _, err := workflowRasterTileTargets(WorkflowProducedTarget{}, encodeRasterTileManifest(t, manifest), true); err == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
	partial := rasterTileManifestFixture(2)
	partial["failed_tile"] = "r3_c4"
	if _, err := workflowRasterTileTargets(WorkflowProducedTarget{}, encodeRasterTileManifest(t, partial), false); err == nil {
		t.Fatal("accepted invalid failed tile")
	}
	delete(partial["tiles"].(map[string]interface{}), "r0_c0")
	partial["completed_tiles"] = 1
	partial["failed_tile"] = "r0_c4"
	if _, err := workflowRasterTileTargets(WorkflowProducedTarget{}, encodeRasterTileManifest(t, partial), false); err == nil {
		t.Fatal("accepted skipped earlier tile")
	}
}

func TestRasterTileAdapterScopeAndContract(t *testing.T) {
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		2: {ID: 2, EngineType: "minio", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"endpoint": "target:9000", "access_key": "key", "secret_key": "secret"}},
	})
	workflow := map[string]interface{}{"tasks": []interface{}{map[string]interface{}{
		"id": "save", "operator": "raster_save_tiles", "depends_on": []interface{}{}, "params": map[string]interface{}{
			"target_parent_locator": "addp://engine/2/path/business/results?type=prefix", "target_name": "tiles", "write_mode": "create", "input_tiles": map[string]interface{}{"$ref": "tile"},
		},
	}}}
	prepared, scopes, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, "geopython_workflow", workflow)
	if err != nil || len(scopes) != 1 {
		t.Fatalf("scopes=%#v %v", scopes, err)
	}
	if !scopes[0].BatchScope || scopes[0].Type != "prefix" {
		t.Fatalf("scope=%#v", scopes)
	}
	plan := firstTaskParams(t, prepared)["access_plan"].(commonModels.JSONMap)
	if plan["target"].(commonModels.JSONMap)["kind"] != "directory" {
		t.Fatalf("plan=%#v", plan)
	}
	operator := commonModels.OperatorDescriptor{ID: "raster_save_tiles", EngineType: "geopython_workflow", DisplayName: "栅格切片批量保存"}
	public := publicWorkflowOperators("geopython_workflow", []commonModels.OperatorDescriptor{operator})
	for _, param := range public[0].PublicParameters {
		if param.Name == "target_resource" && param.UIConfig["target_name_kind"] != "directory" {
			t.Fatalf("param=%#v", param)
		}
	}
	schema := workflowRasterTilesOutputSchema("tiles")
	contract := taskprovider.ExecutionContract{InputDefaults: map[string]interface{}{}, InputUISchema: map[string]interface{}{}, InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, OutputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"save": schema}, "additionalProperties": false}}
	encoded, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateExecutionContract(payload); err != nil {
		t.Fatal(err)
	}
	output := map[string]interface{}{"save": map[string]interface{}{"tiles": map[string]interface{}{
		"r0_c0": map[string]interface{}{"resource": map[string]interface{}{"locator": "addp://engine/2/path/business/tiles/r0_c0.tif?type=object", "type": "object", "write_mode": "create"}},
	}}}
	if err := taskprovider.ValidateExecutionParameters(contract.OutputSchema, output, taskprovider.ParameterValidationOptions{}); err != nil {
		t.Fatal(err)
	}
	output["save"].(map[string]interface{})["resource"] = map[string]interface{}{"locator": "addp://engine/2/path/business/tiles?type=prefix"}
	if err := taskprovider.ValidateExecutionParameters(contract.OutputSchema, output, taskprovider.ParameterValidationOptions{}); err == nil {
		t.Fatal("accepted a scope as an output resource")
	}
	for _, name := range []string{"../escape", ".", "..", "a/b", "a\\b"} {
		firstTaskParams(t, workflow)["target_name"] = name
		if _, _, err := svc.preprocessWorkflowParamsWithTargets(context.Background(), 7, "geopython_workflow", workflow); err == nil {
			t.Fatalf("accepted target=%s", name)
		}
	}
}

func TestRasterTileRejectsOverlappingScopes(t *testing.T) {
	batch := WorkflowProducedTarget{EngineID: 2, Path: []string{"business", "tiles"}, BatchScope: true}
	for _, other := range []WorkflowProducedTarget{
		{EngineID: 2, Path: []string{"business", "tiles"}, BatchScope: true},
		{EngineID: 2, Path: []string{"business", "tiles", "r0_c0.tif"}},
		{EngineID: 2, Path: []string{"business"}, BatchScope: true},
	} {
		if err := rejectWorkflowBatchTargetOverlap([]WorkflowProducedTarget{batch, other}); err == nil {
			t.Fatal("accepted overlap")
		}
	}
	if err := rejectWorkflowBatchTargetOverlap([]WorkflowProducedTarget{batch, {EngineID: 2, Path: []string{"business", "tiles_other"}, BatchScope: true}}); err != nil {
		t.Fatal(err)
	}
}

func TestRasterTileAuthorizationIncludesTargetReadAndWrite(t *testing.T) {
	discovery := newWorkflowValidationTestService(t)
	descriptor := discovery.getRuntimeDescriptor
	discovery.getRuntimeDescriptor = func(ctx context.Context, tenantID, engineID uint) (*commonModels.EngineRuntimeDescriptor, error) {
		d, err := descriptor(ctx, tenantID, engineID)
		d.EngineType = "geopython_workflow"
		return d, err
	}
	discovery.listWorkflowOperators = func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
		return []commonModels.OperatorDescriptor{
			{ID: "raster_load", Name: "raster_load", EngineType: "geopython_workflow", Effects: []string{"read"}, ExecutionModes: []string{"workflow"}, Parameters: []commonModels.ParameterDescriptor{{Name: "access_plan", Type: "object"}}},
			{ID: "raster_save_tiles", Name: "raster_save_tiles", EngineType: "geopython_workflow", Effects: []string{"read", "write"}, ExecutionModes: []string{"workflow"}, Parameters: []commonModels.ParameterDescriptor{{Name: "access_plan", Type: "object"}}},
		}, nil
	}
	executor := &DevExecutor{operatorDiscovery: discovery}
	plan, err := executor.buildWorkflowExecutionAuthorizationPlan(context.Background(), &models.DevTask{DevType: "workflow", ExecutionConfig: models.DevTaskContent{"engine_id": float64(12)}, Content: models.DevTaskContent{"workflow_definition": map[string]interface{}{"tasks": []interface{}{
		map[string]interface{}{"id": "load", "operator": "raster_load", "params": map[string]interface{}{"locator": "addp://engine/1/path/source.tif?type=file"}},
		map[string]interface{}{"id": "save", "operator": "raster_save_tiles", "params": map[string]interface{}{"target_parent_locator": "addp://engine/2/path/business?type=bucket", "target_name": "tiles"}},
	}}}}, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint][]string{1: {"read"}, 2: {"read", "write"}, 12: {"read", "write"}}
	if !reflect.DeepEqual(plan.engineEffects, want) {
		t.Fatalf("effects=%#v", plan.engineEffects)
	}
}

func TestRasterTileFailedHTTPRuntimeRetainsActualTargets(t *testing.T) {
	plugin.Register(plugin.NewHTTPWorkflowRuntimeProvider("geopython_workflow", "GeoPython Workflow"))
	raw := encodeRasterTileManifest(t, rasterTileManifestFixture(2))
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/operators" {
			json.NewEncoder(w).Encode(map[string]interface{}{"operators": []map[string]interface{}{{
				"id": "raster_save_tiles", "name": "raster_save_tiles", "display_name": "栅格切片批量保存", "engine_type": "geopython_workflow",
				"category": "栅格计算", "category_path": []string{"栅格计算"}, "description": "Save raster tiles",
				"parameters": []interface{}{}, "output_ports": []map[string]interface{}{{"name": "default", "type": "object", "is_default": true}},
				"execution_modes": []string{"workflow"}, "effects": []string{"read", "write"},
			}}})
			return
		}
		if r.URL.Path == "/api/workflow" {
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "pending", "execution_id": "tiles-failed"})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "failed", "execution_id": "tiles-failed", "error": "batch failed", "error_code": "EXECUTION_FAILED", "result": raw, "all_results": map[string]string{"save": raw}})
	}))
	defer runtime.Close()
	host, port, err := net.SplitHostPort(strings.TrimPrefix(runtime.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		91: {ID: 91, EngineType: "geopython_workflow", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"protocol": "http", "host": host, "port": port}},
		2:  {ID: 2, EngineType: "minio", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"endpoint": "minio:9000", "access_key": "a", "secret_key": "b"}},
	})
	resp, err := svc.ExecuteWorkflow(context.Background(), 7, map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "save", "operator": "raster_save_tiles", "depends_on": []interface{}{}, "params": map[string]interface{}{"target_parent_locator": "addp://engine/2/path/business?type=bucket", "target_name": "tiles"}}}}, nil, `{"engine_id":91}`)
	if err == nil || resp == nil || resp.Status != "failed" || len(resp.ProducedTargets) != 2 || resp.FinalResult != raw {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
}

func TestRasterTilesPublicValidationRequiresWorkflowReference(t *testing.T) {
	svc := newWorkflowValidationTestService(t)
	svc.listWorkflowOperators = func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
		return []commonModels.OperatorDescriptor{
			{ID: "source", Name: "source", ExecutionModes: []string{"workflow"}, Effects: []string{"read"}},
			{ID: "sink", Name: "sink", ExecutionModes: []string{"workflow"}, Effects: []string{"read"}, Parameters: []commonModels.ParameterDescriptor{{Name: "input_tiles", Type: "raster_tiles", ParamType: "input", Required: true}}},
		}, nil
	}
	for _, tc := range []struct {
		name  string
		value interface{}
		valid bool
	}{
		{"reference", map[string]interface{}{"$ref": "tile", "port": "default"}, true},
		{"forged-json", map[string]interface{}{"runtime_type": "raster_tiles", "tiles": []interface{}{}}, false},
		{"raw-directory", "/private/tiles", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := svc.ValidateWorkflowForTenant(context.Background(), 12, map[string]interface{}{"tasks": []interface{}{
				map[string]interface{}{"id": "tile", "operator": "source", "params": map[string]interface{}{}, "depends_on": []interface{}{}},
				map[string]interface{}{"id": "save", "operator": "sink", "params": map[string]interface{}{"input_tiles": tc.value}, "depends_on": []interface{}{"tile"}},
			}}, 7)
			if err != nil || result.Valid != tc.valid {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if !tc.valid && (len(result.Errors) != 1 || result.Errors[0].Code != "raster_reference_required") {
				t.Fatalf("errors=%#v", result.Errors)
			}
		})
	}
}
