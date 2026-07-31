package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/twpayne/go-geom"
)

func TestWorkflowGeometryBatchReprojectProviderInvokesDirectRuntimeAndNormalizesEWKB(t *testing.T) {
	const engineType = "acme_geo_workflow"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
			writeJSON(t, w, map[string]interface{}{
				"operators": []map[string]interface{}{{
					"id":              "vector_reproject",
					"name":            "vector_reproject",
					"display_name":    "Vector Reproject",
					"engine_type":     engineType,
					"category":        "spatial_transform",
					"category_path":   []string{"spatial", "transform"},
					"description":     "Reproject geometry batch",
					"parameters":      []interface{}{},
					"output_ports":    []interface{}{},
					"execution_modes": []string{"workflow", "direct"},
					"effects":         []string{"read"},
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/operators/vector_reproject/invoke":
			var req engineplugin.OperatorInvokeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode invoke request: %v", err)
			}
			if req.BinaryPayload == nil {
				t.Fatal("invoke request missing binary payload")
			}
			if req.BinaryPayload.ContentType != "application/vnd.apache.arrow.stream" || req.BinaryPayload.Encoding != "arrow" {
				t.Fatalf("binary payload = %#v, want arrow stream", req.BinaryPayload)
			}
			if req.BinaryPayload.Metadata["geometry_encoding"] != "ewkb" {
				t.Fatalf("geometry encoding metadata = %#v, want ewkb", req.BinaryPayload.Metadata)
			}
			decoded, err := commonSpatial.DecodeGeometryBatchArrow(req.BinaryPayload.Data)
			if err != nil {
				t.Fatalf("decode request geometry batch: %v", err)
			}
			if decoded.GeometryColumn != "geom" || decoded.GeometryEncoding != commonSpatial.GeometryBatchArrowEncodingEWKB ||
				decoded.SourceCRS != "EPSG:3857" || decoded.TargetCRS != "EPSG:4326" {
				t.Fatalf("decoded request = %#v, want geom EWKB EPSG:3857 -> EPSG:4326", decoded)
			}

			transformed, err := commonSpatial.GeomToEWKB(geom.NewPointFlat(geom.XY, []float64{10, 0}), 4326)
			if err != nil {
				t.Fatalf("encode transformed geometry: %v", err)
			}
			responsePayload, err := commonSpatial.EncodeGeometryBatchArrow([][]byte{transformed}, commonSpatial.GeometryBatchArrowOptions{
				GeometryColumn:   "geom",
				GeometryEncoding: commonSpatial.GeometryBatchArrowEncodingEWKB,
				SourceCRS:        "EPSG:4326",
				TargetCRS:        "EPSG:4326",
			})
			if err != nil {
				t.Fatalf("encode response geometry batch: %v", err)
			}
			writeJSON(t, w, map[string]interface{}{
				"status": "success",
				"binary_payload": engineplugin.BinaryPayload{
					ContentType: "application/vnd.apache.arrow.stream",
					Encoding:    "arrow",
					Name:        "geometry_batch",
					Data:        responsePayload,
					Metadata: map[string]interface{}{
						"geometry_column":   "geom",
						"geometry_encoding": "ewkb",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(endpoint.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	capabilitiesBytes, err := json.Marshal(engineplugin.NewWorkflowCapabilities(engineType, engineplugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	capabilities := commonModels.JSONString(capabilitiesBytes)
	engine := commonModels.Engine{
		EngineType:   engineType,
		Name:         "ACME Geo Workflow",
		Capabilities: &capabilities,
		ConnectionInfo: commonModels.ConnectionInfo{
			"protocol": endpoint.Scheme,
			"host":     host,
			"port":     port,
		},
	}
	provider := newWorkflowGeometryBatchReprojectProvider(engine, "vector_reproject")
	source, err := commonSpatial.GeomToEWKB(geom.NewPointFlat(geom.XY, []float64{1113194.9079327357, 0}), 3857)
	if err != nil {
		t.Fatalf("encode source geometry: %v", err)
	}

	result, err := provider.ReprojectGeometryBatch(context.Background(), [][]byte{source}, "EPSG:3857", "EPSG:4326", "geom")
	if err != nil {
		t.Fatalf("ReprojectGeometryBatch returned error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("result count = %d, want 1", len(result))
	}
	geometry, err := commonSpatial.ParseGeometryBytes(result[0])
	if err != nil {
		t.Fatalf("parse result geometry: %v", err)
	}
	point, ok := geometry.(*geom.Point)
	if !ok {
		t.Fatalf("result geometry = %T, want *geom.Point", geometry)
	}
	if geometry.SRID() != 4326 {
		t.Fatalf("result SRID = %d, want 4326", geometry.SRID())
	}
	if coords := point.FlatCoords(); coords[0] != 10 || coords[1] != 0 {
		t.Fatalf("result coords = %#v, want [10 0]", coords)
	}
}

func TestWorkflowGeometryBatchReprojectProviderRejectsNonEWKBResponse(t *testing.T) {
	const engineType = "acme_geo_workflow"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
			writeJSON(t, w, map[string]interface{}{
				"operators": []map[string]interface{}{{
					"id":              "vector_reproject",
					"name":            "vector_reproject",
					"display_name":    "Vector Reproject",
					"engine_type":     engineType,
					"category":        "spatial_transform",
					"category_path":   []string{"spatial", "transform"},
					"description":     "Reproject geometry batch",
					"parameters":      []interface{}{},
					"output_ports":    []interface{}{},
					"execution_modes": []string{"workflow", "direct"},
					"effects":         []string{"read"},
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/operators/vector_reproject/invoke":
			transformed, err := commonSpatial.GeomToWKB(geom.NewPointFlat(geom.XY, []float64{10, 0}))
			if err != nil {
				t.Fatalf("encode transformed geometry: %v", err)
			}
			responsePayload, err := commonSpatial.EncodeGeometryBatchArrow([][]byte{transformed}, commonSpatial.GeometryBatchArrowOptions{
				GeometryColumn:   "geom",
				GeometryEncoding: commonSpatial.GeometryBatchArrowEncodingWKB,
				SourceCRS:        "EPSG:4326",
				TargetCRS:        "EPSG:4326",
			})
			if err != nil {
				t.Fatalf("encode response geometry batch: %v", err)
			}
			writeJSON(t, w, map[string]interface{}{
				"status": "success",
				"binary_payload": engineplugin.BinaryPayload{
					ContentType: "application/vnd.apache.arrow.stream",
					Encoding:    "arrow",
					Name:        "geometry_batch",
					Data:        responsePayload,
					Metadata: map[string]interface{}{
						"geometry_column":   "geom",
						"geometry_encoding": "wkb",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	engine := workflowReprojectTestEngine(t, server.URL, engineType)
	provider := newWorkflowGeometryBatchReprojectProvider(engine, "vector_reproject")
	source, err := commonSpatial.GeomToEWKB(geom.NewPointFlat(geom.XY, []float64{1113194.9079327357, 0}), 3857)
	if err != nil {
		t.Fatalf("encode source geometry: %v", err)
	}

	_, err = provider.ReprojectGeometryBatch(context.Background(), [][]byte{source}, "EPSG:3857", "EPSG:4326", "geom")
	if err == nil {
		t.Fatal("ReprojectGeometryBatch returned nil error for WKB response")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("write JSON response: %v", err)
	}
}

func workflowReprojectTestEngine(t *testing.T, endpointURL, engineType string) commonModels.Engine {
	t.Helper()
	endpoint, err := url.Parse(endpointURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(endpoint.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	capabilitiesBytes, err := json.Marshal(engineplugin.NewWorkflowCapabilities(engineType, engineplugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	capabilities := commonModels.JSONString(capabilitiesBytes)
	return commonModels.Engine{
		EngineType:   engineType,
		Name:         "ACME Geo Workflow",
		Capabilities: &capabilities,
		ConnectionInfo: commonModels.ConnectionInfo{
			"protocol": endpoint.Scheme,
			"host":     host,
			"port":     port,
		},
	}
}
