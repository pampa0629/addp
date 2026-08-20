package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

func TestWorkflowCRSDefinitionConverterInvokesDirectRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/operators" {
			writeJSON(t, w, map[string]interface{}{"operators": []map[string]interface{}{{
				"id":              "crs_to_projjson",
				"name":            "crs_to_projjson",
				"display_name":    "CRS to PROJJSON",
				"engine_type":     "acme_geo_workflow",
				"category":        "spatial_transform",
				"category_path":   []string{"spatial", "transform"},
				"description":     "Convert CRS definition",
				"parameters":      []interface{}{},
				"output_ports":    []interface{}{},
				"execution_modes": []string{"direct"},
				"effects":         []string{"read"},
			}}})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/operators/crs_to_projjson/invoke" {
			http.NotFound(w, r)
			return
		}
		var request plugin.OperatorInvokeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.BinaryPayload != nil {
			t.Fatal("crs_to_projjson must not send binary payload")
		}
		if request.Params["crs_ref"] != "EPSG:3857" || request.Params["definition_encoding"] != "wkt" || request.Params["definition"] != "PROJCS[...]" {
			t.Fatalf("params = %#v, want EPSG:3857 WKT", request.Params)
		}
		writeJSON(t, w, map[string]interface{}{
			"status": "success",
			"result": map[string]interface{}{
				"crs_ref":             "EPSG:3857",
				"definition_encoding": "projjson",
				"definition":          `{"type":"ProjectedCRS","name":"WGS 84 / Pseudo-Mercator","id":{"authority":"EPSG","code":3857}}`,
			},
		})
	}))
	defer server.Close()

	engine := workflowReprojectTestEngine(t, server.URL, "acme_geo_workflow")
	resolveCalls := 0
	converter := newWorkflowCRSDefinitionConverter(func(context.Context) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
		resolveCalls++
		return engine, commonModels.OperatorDescriptor{Name: "crs_to_projjson"}, nil
	})
	definition, err := converter.ConvertCRSDefinition(context.Background(), "EPSG:3857", &datatype.CRSDefinition{
		ID:                 "EPSG:3857",
		DefinitionEncoding: datatype.CRSDefinitionEncodingWKT,
		Definition:         "PROJCS[...]",
		Source:             datatype.CRSDefinitionSourcePostGISSpatialRefSys,
	}, datatype.CRSDefinitionEncodingPROJJSON)
	if err != nil {
		t.Fatalf("ConvertCRSDefinition failed: %v", err)
	}
	if resolveCalls != 1 {
		t.Fatalf("resolve calls = %d, want 1", resolveCalls)
	}
	if definition.ID != "EPSG:3857" || definition.DefinitionEncoding != datatype.CRSDefinitionEncodingPROJJSON || definition.Source != datatype.CRSDefinitionSourceNormalizationRuntime {
		t.Fatalf("definition = %#v, want normalized EPSG:3857 PROJJSON", definition)
	}
}

func TestCRSDefinitionFromOperatorResultRejectsMissingType(t *testing.T) {
	_, err := crsDefinitionFromOperatorResult(&plugin.OperatorInvokeResult{Result: map[string]interface{}{
		"result": map[string]interface{}{
			"crs_ref":             "EPSG:3857",
			"definition_encoding": "projjson",
			"definition":          `{}`,
		},
	}})
	if err == nil {
		t.Fatal("expected invalid PROJJSON error")
	}
}
