package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
)

func TestVectorTilePublicContract(t *testing.T) {
	discovery := additionalConversionDiscovery(t, "geopython_workflow", "vector_to_pmtiles")
	discovery.listWorkflowOperators = func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
		return []commonModels.OperatorDescriptor{{ID: "vector_to_pmtiles", Name: "vector_to_pmtiles", Effects: []string{"read", "write"}, ExecutionModes: []string{"workflow", "direct"}, Parameters: []commonModels.ParameterDescriptor{
			{Name: "access_plan", Type: "object", Required: true}, {Name: "tile", Type: "object", Required: true}, {Name: "options", Type: "object"},
		}}}, nil
	}
	operators, err := discovery.GetOperatorsByWorkflowEngineIDForTenant(context.Background(), 12, 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, param := range operators[0].PublicParameters {
		if param.Name == "tile" || param.Name == "access_plan" || param.Name == "options" {
			t.Fatalf("private parameter exposed: %s", param.Name)
		}
	}
	operators[0].LocalizeAdapterParameters("geopython_workflow", "en")
	if parameterByName(t, operators[0].PublicParameters, "attributes").DisplayName != "Retained attributes" {
		t.Fatal("attribute label was not localized")
	}
	picker := parameterByName(t, operators[0].PublicParameters, "source_resource")
	if formats := picker.UIConfig["file_formats"].([]string); len(formats) != 2 || formats[0] != "shapefile" || formats[1] != "geojson" {
		t.Fatalf("source formats: %#v", formats)
	}
	workflow := additionalConversionWorkflow("vector_to_pmtiles", "geojson", "result.pmtiles")
	params := firstTaskParams(t, workflow)
	params["min_zoom"], params["max_zoom"], params["layer_name"], params["attributes"] = 0, 2, "parks", []interface{}{"name"}
	contract, err := discovery.WorkflowExecutionContractForTenant(context.Background(), 12, workflow, 7)
	if err != nil {
		t.Fatal(err)
	}
	if contract.OutputSchema["properties"].(map[string]interface{})["convert"] == nil {
		t.Fatal("stable resource output missing")
	}
	params["tile"] = map[string]interface{}{}
	validation, err := discovery.ValidateWorkflowForTenant(context.Background(), 12, workflow, 7)
	if err != nil || validation.Valid {
		t.Fatalf("private tile accepted: %+v %v", validation, err)
	}
}

func TestVectorTileAccessPlanUsesExactMetaFacts(t *testing.T) {
	harness := newWorkflowEngineServiceWithEnginesForTest(t, map[uint]commonModels.Engine{
		1: {ID: 1, EngineType: "nfs", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"mount_path": "/data"}},
		2: {ID: 2, EngineType: "minio", LifecycleState: "active", ConnectionInfo: commonModels.ConnectionInfo{"endpoint": "minio:9000", "access_key": "key", "secret_key": "secret"}},
	})
	item := commonModels.MetaItem{ID: 8, TenantID: 7, EngineID: 1, FullName: "source/scene.geojson", Attributes: map[string]interface{}{"item": map[string]interface{}{"format": "geojson", "layout": "single", "data_type": "table"}}}
	// The Meta spatial projection supplies the native SRID and extent, without a separate extent CRS.
	spatial := commonModels.SpatialMetadata{SRID: 4326, Extent: []float64{116, 39, 117, 40}, GeometryColumn: "shape", Fields: []datatype.FieldInfo{{Name: "name"}, {Name: "shape"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer addp_at_develop" {
			t.Error("missing service identity")
		}
		switch r.URL.Path {
		case "/api/v1/meta/items/by-catalog-path":
			_ = json.NewEncoder(w).Encode(item)
		case "/api/v1/meta/items/8/spatial":
			_ = json.NewEncoder(w).Encode(spatial)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	harness.service.SetMetaClient(commonClient.NewMetaClient(server.URL, staticServiceTokenSource("addp_at_develop")))
	workflow := additionalConversionWorkflow("vector_to_pmtiles", "geojson", "result.pmtiles")
	params := firstTaskParams(t, workflow)
	params["min_zoom"], params["max_zoom"], params["layer_name"], params["attributes"] = 0, 2, "parks", []interface{}{"name"}
	prepared, targets, err := harness.preprocessWorkflowParamsWithTargets(context.Background(), 7, "geopython_workflow", workflow)
	if err != nil {
		t.Fatal(err)
	}
	runtime := firstTaskParams(t, prepared)
	tile := runtime["tile"].(commonModels.JSONMap)
	if tile["source_srs"] != "EPSG:4326" || tile["extent_srid"] != 4326 || runtime["locator"] != nil {
		t.Fatalf("incorrect derived facts: %#v", runtime)
	}
	if len(targets) != 1 || targets[0].WriteMode != "create" {
		t.Fatalf("targets: %#v", targets)
	}
	targets[0].TaskID = "convert"
	facts := workflowLineageFacts(workflow, workflowExecutionOutputs(targets))
	if facts == nil || len(facts.Inputs) != 1 || len(facts.Outputs) != 1 || len(facts.Operations) != 1 || facts.Operations[0].Operator != "vector_to_pmtiles" {
		t.Fatalf("missing vector tile lineage: %#v", facts)
	}
	options := runtime["options"].(commonModels.JSONMap)
	if options["min_zoom"] != nil || options["attributes"] == nil || options["layer_name"] != "parks" {
		t.Fatalf("options: %#v", options)
	}
	spatial.SRID, spatial.Extent = 4549, []float64{500000, 3000000, 501000, 3001000}
	projected, _, err := harness.preprocessWorkflowParamsWithTargets(context.Background(), 7, "geopython_workflow", workflow)
	if err != nil {
		t.Fatal(err)
	}
	projectedTile := firstTaskParams(t, projected)["tile"].(commonModels.JSONMap)
	if projectedTile["source_srs"] != "EPSG:4549" || projectedTile["extent_srid"] != 4549 {
		t.Fatalf("extent must retain the native CRS: %#v", projectedTile)
	}
	spatial.Extent = []float64{116, 39, 117, 40}
	for _, change := range []func(){
		func() { params["attributes"] = []interface{}{"missing"} },
		func() { params["attributes"] = "*" },
		func() { params["attributes"] = []interface{}{"shape"} },
		func() { params["max_zoom"] = 1.5 },
		func() { item.ID = 9 },
		func() { item.TenantID = 8 },
		func() { item.Attributes["item"].(map[string]interface{})["format"] = "geopackage" },
		func() { item.Attributes["item"].(map[string]interface{})["layout"] = "whole" },
		func() { spatial.SRID = 0 },
	} {
		params["attributes"], params["max_zoom"], item.ID, item.TenantID, spatial.SRID = []interface{}{"name"}, 2, 8, 7, 4326
		item.Attributes["item"] = map[string]interface{}{"format": "geojson", "layout": "single", "data_type": "table"}
		change()
		if _, _, err := harness.preprocessWorkflowParamsWithTargets(context.Background(), 7, "geopython_workflow", workflow); err == nil {
			t.Fatal("invalid source facts or fields accepted")
		}
	}
}
