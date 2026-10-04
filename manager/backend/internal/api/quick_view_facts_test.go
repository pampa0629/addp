package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/preview"
)

func quickViewMetaFixture(t *testing.T, locator string, attrs commonModels.JSONMap) *preview.PreviewResolverRequest {
	t.Helper()
	loc, err := resourcetree.ParseURI(locator)
	if err != nil {
		t.Fatal(err)
	}
	tenant := uint(7)
	return &preview.PreviewResolverRequest{
		Locator: loc, TenantID: &tenant, MetaItemID: loc.ItemID,
		Engine:          &commonModels.Engine{ID: loc.EngineID, EngineType: "nfs"},
		Metadata:        &commonModels.MetaNode{EngineID: loc.EngineID, Attributes: attrs},
		ItemFingerprint: "fp-facts",
	}
}

func TestQuickViewSourceFromMetaPreservesFormatFacts(t *testing.T) {
	for _, tc := range []struct{ dataType, format, layout string }{
		{"media", "tiff", "single"},
		{"media", "tiff", "multi"},
		{"model_3d", "osgb", "single"},
		{"model_3d", "osgb_scene", "whole"},
		{"model_3d", "obj", "single"},
		{"model_3d", "glb", "single"},
		{"model_3d", "ifc", "single"},
		{"gaussian_splat", "ply", "single"},
		{"point_cloud", "copc", "single"},
		{"document", "pptx", "single"},
	} {
		t.Run(tc.dataType+"/"+tc.format+"/"+tc.layout, func(t *testing.T) {
			attrs := commonModels.JSONMap{
				"item":    commonModels.JSONMap{"data_type": tc.dataType, "format": tc.format, "layout": tc.layout},
				"storage": commonModels.JSONMap{"total_size": int64(8192)},
				"type_info": commonModels.JSONMap{
					"media":          commonModels.JSONMap{"width": 2048, "height": 1024, "band_count": 3},
					"gaussian_splat": commonModels.JSONMap{"splat_count": 256},
				},
				"format_info": commonModels.JSONMap{"tiff": commonModels.JSONMap{"profile": "geotiff"}},
			}
			locator := "addp://engine/26/path/samples/item." + tc.format + "?type=file&item_id=99"
			if tc.layout == "multi" {
				locator = "addp://engine/26/path/addp/image/item.tiff?type=object&item_id=99"
			}
			req := quickViewMetaFixture(t, locator, attrs)
			source := quickViewSourceFromMeta(req)
			if source.EngineID != 26 || source.Identity.TenantID != 7 || source.Identity.ItemFingerprint != "fp-facts" {
				t.Fatalf("lost identity: %#v", source)
			}
			if source.DirectFlatGeobuf || source.CanTile || source.FlatGeobufURL != "" {
				t.Fatalf("non-vector facts expose vector route: %#v", source)
			}
			var stream string
			switch tc.dataType {
			case "media":
				if source.Raster == nil || source.Raster.SizeBytes != 8192 || source.Raster.Width != 2048 || source.Raster.Height != 1024 || source.Raster.Profile != "geotiff" {
					t.Fatalf("raster facts: %#v", source.Raster)
				}
				stream = source.Raster.PreviewURL
			case "model_3d":
				if source.Model3D == nil || source.Model3D.Format != tc.format || source.Model3D.Layout != tc.layout || source.Model3D.SourceSizeBytes != 8192 {
					t.Fatalf("model facts: %#v", source.Model3D)
				}
				stream = source.Model3D.PreviewURL
			case "gaussian_splat":
				if source.GaussianSplat == nil || source.GaussianSplat.SplatCount != 256 || source.GaussianSplat.SourceSizeBytes != 8192 {
					t.Fatalf("splat facts: %#v", source.GaussianSplat)
				}
				stream = source.GaussianSplat.PreviewURL
			case "point_cloud":
				if source.PointCloud == nil {
					t.Fatal("missing point cloud facts")
				}
				stream = source.PointCloud.PreviewURL
			case "document":
				if source.PPTX == nil || source.PPTX.Format != "pptx" {
					t.Fatalf("document facts: %#v", source.PPTX)
				}
				return
			}
			u, err := url.Parse(stream)
			if err != nil || u.Path != "/api/v1/manager/storage-stream" || u.Query().Get("locator") != req.Locator.ToURI() || u.Query().Get("storage_ref") != strings.Join(req.Locator.Path, "/") || u.Query().Has("engine_id") {
				t.Fatalf("non-canonical source URL: %q, %v", stream, err)
			}
		})
	}
}

func TestQuickViewSourceFromMetaSpatialFacts(t *testing.T) {
	srid := 4326
	extent := datatype.BoundingBox{110, 20, 120, 30}
	definition := datatype.CRSDefinition{ID: "ADDP:CRS:custom", DefinitionEncoding: datatype.CRSDefinitionEncodingESRIWKT, Definition: `PROJCS["Custom_CRS"]`, Source: datatype.CRSDefinitionSourceSidecarPRJ}
	for _, custom := range []bool{false, true} {
		facts := &datatype.SpatialInfo{
			SRID: &srid, Extent: &extent,
			GeometryColumns: []datatype.GeometryColumnInfo{{Name: "shape", SRID: &srid}},
		}
		if custom {
			facts.SRID = nil
			facts.GeometryColumns[0].SRID = nil
			facts.GeometryColumns[0].CRSRef = definition.ID
			facts.CRSDefinitions = []datatype.CRSDefinition{definition}
		}
		req := quickViewMetaFixture(t, "addp://engine/26/path/vector/file.shp?type=file&item_id=99", commonModels.JSONMap{
			"capabilities": commonModels.JSONMap{"spatial": datatype.SpatialInfoPayload(facts)},
			"type_info":    commonModels.JSONMap{"table": commonModels.JSONMap{"row_count": 73090}},
		})
		source := quickViewSourceFromMeta(req)
		if source.SpatialMeta == nil || source.SpatialMeta.GeomColumn != "shape" || source.SpatialMeta.RecordCount != 73090 || !source.DirectFlatGeobuf || source.CanTile == custom {
			t.Fatalf("lost spatial facts: %#v", source)
		}
		if custom && !reflect.DeepEqual(source.SpatialMeta.SourceCRSDefinition, &definition) {
			t.Fatalf("lost CRS definition: %#v", source.SpatialMeta)
		}
		if !strings.Contains(source.FlatGeobufURL, "page_size=73090") {
			t.Fatalf("wrong full item URL: %q", source.FlatGeobufURL)
		}
		// Missing and zero counts must not manufacture a one-row content URL.
		delete(req.Metadata.Attributes, "type_info")
		source = quickViewSourceFromMeta(req)
		if source.SpatialMeta.RecordCount != -1 || source.FlatGeobufURL != "" {
			t.Fatalf("unknown count was invented: %#v", source)
		}
		zero := int64(0)
		req.ItemRowCount = &zero
		if source = quickViewSourceFromMeta(req); source.SpatialMeta.RecordCount != 0 || source.FlatGeobufURL != "" {
			t.Fatalf("zero count was invented: %#v", source)
		}
	}
}

func TestQuickViewSourceFromMetaDoesNotInventSpatialFacts(t *testing.T) {
	for _, attrs := range []commonModels.JSONMap{
		nil,
		{"item": commonModels.JSONMap{"data_type": "table"}},
		{"capabilities": commonModels.JSONMap{"spatial": commonModels.JSONMap{
			"geometry_columns": []map[string]interface{}{{"name": "shape_a", "srid": 4326}, {"name": "shape_b", "srid": 3857}},
		}}},
	} {
		req := quickViewMetaFixture(t, "addp://engine/26/path/public/roads?type=table&item_id=99", attrs)
		req.Engine.EngineType = "postgresql"
		source := quickViewSourceFromMeta(req)
		if source.DirectFlatGeobuf || source.CanTile || source.FlatGeobufURL != "" {
			t.Fatalf("incomplete facts expose content capability: %#v", source)
		}
	}
}

func TestQuickViewSourceFromMetaPostgresUsesExactMetadataCount(t *testing.T) {
	req := quickViewMetaFixture(t, "addp://engine/26/path/public/roads?type=table&item_id=99", commonModels.JSONMap{
		"capabilities": commonModels.JSONMap{"spatial": commonModels.JSONMap{
			"geometry_columns": []map[string]interface{}{{"name": "shape", "srid": 4490}},
		}},
		"type_info": commonModels.JSONMap{"table": commonModels.JSONMap{"row_count": 999}},
	})
	req.Engine.EngineType = "postgresql"
	count := int64(12)
	req.ItemRowCount = &count
	source := quickViewSourceFromMeta(req)
	if source.Schema != "public" || source.Table != "roads" || !source.CanTile || source.SpatialMeta.RecordCount != 12 || source.SpatialMeta.SRID != 4490 {
		t.Fatalf("wrong source facts: %#v", source)
	}
}

func TestQuickViewSourceForLocatorReadsOnlyOwnerFacts(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		engineTenant, itemEngine uint
		missing                  bool
		wantErr                  bool
	}{
		{"spatial", 7, 26, false, false},
		{"caller-path-not-authoritative", 7, 26, false, false},
		{"wrong-tenant", 8, 26, false, true},
		{"wrong-item-engine", 7, 99, false, true},
		{"unscanned", 7, 26, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet {
					t.Errorf("capability caused a write: %s %s", r.Method, r.URL.Path)
				}
				switch r.URL.Path {
				case "/api/v1/system/engines/26":
					json.NewEncoder(w).Encode(map[string]interface{}{"id": 26, "tenant_id": tc.engineTenant, "engine_type": "unregistered-facts-only", "lifecycle_state": "active", "connection_status": "online"})
				case "/api/v1/meta/items/99":
					if tc.missing {
						w.WriteHeader(404)
						return
					}
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id": 99, "engine_id": tc.itemEngine, "tenant_id": 7, "item_type": "table", "name": "roads", "full_name": "public.roads", "row_count": 12,
						"attributes": map[string]interface{}{"capabilities": map[string]interface{}{"spatial": map[string]interface{}{"geometry_columns": []map[string]interface{}{{"name": "shape", "srid": 4326}}, "srid": 4326}}},
					})
				default:
					t.Errorf("unexpected owner request: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			token := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "facts-test-token", nil })
			resolver := preview.NewPreviewResolver(preview.NewPreviewRegistry(), client.NewSystemClient(server.URL, token), client.NewMetaClient(server.URL, token))
			h := &QuickViewHandler{previewResolver: resolver}
			tenant := uint(7)
			locator := "addp://engine/26/path/public/roads?type=table&item_id=99"
			if tc.name == "caller-path-not-authoritative" {
				locator = "addp://engine/26/path/other/unrelated?type=table&item_id=99"
			}
			source, err := h.quickViewSourceForLocator(context.Background(), &tenant, locator)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && (source.SpatialMeta == nil || source.SpatialMeta.GeomColumn != "shape" || source.SpatialMeta.RecordCount != 12) {
				t.Fatalf("owner facts not used: %#v", source)
			}
			if !tc.wantErr && source.Identity.Locator != "addp://engine/26/path/public/roads?type=table&item_id=99" {
				t.Fatalf("caller path replaced owner identity: %#v", source.Identity)
			}
			// No runtime/plugin/provider exists for this engine. Any preview execution fails.
			if requests > 2 {
				t.Fatalf("unexpected additional work: %d requests", requests)
			}
		})
	}
}
