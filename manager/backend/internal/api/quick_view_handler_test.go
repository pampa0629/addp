package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestExecuteQuickViewActionRejectsLegacyAndInvalidExistingResultAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &QuickViewHandler{}
	router := gin.New()
	router.POST("/quick-view/actions", handler.ExecuteQuickViewAction)

	for _, body := range []string{
		`{"locator":"addp://engine/1/path/public/roads?type=table&item_id=1","action":"generate_vector_tile_cache","confirm_existing_result":true}`,
		`{"locator":"addp://engine/1/path/public/roads?type=table&item_id=1","action":"generate_vector_tile_cache","existing_result_action":"keep"}`,
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/quick-view/actions", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d, want 400; response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestQuickViewModel3DGLBActionPropagatesExistingResultConfirmation(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	repo := repository.NewModel3DGLBRepository(db)
	taskSvc := service.NewModel3DGLBTaskService(repo)
	locator := "addp://engine/26/path/models/building.ifc?type=file&item_id=77"
	fingerprint := commonModels.GenerateItemFingerprint(26, "models/building.ifc")
	task := &models.Model3DGLBTask{
		TenantID: 7,
		Name:     "GLB quick-view action confirmation",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"item_locator": locator, "source_engine_id": uint(26),
				"item_fingerprint": fingerprint, "item_id": uint(77), "format": "ifc",
			},
			"result": commonModels.JSONMap{},
		},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create model 3d GLB task: %v", err)
	}
	result := &models.Model3DGLB{
		TenantID: 7, ItemFingerprint: fingerprint, TaskID: &task.ID,
		Locator: locator, SourceEngineID: 26, SourceFormat: "ifc",
		StorageRef: "managed-result", Status: models.Model3DGLBStatusReady,
		Metadata: commonModels.JSONMap{},
	}
	if err := db.Create(result).Error; err != nil {
		t.Fatalf("create current model 3d GLB result: %v", err)
	}
	handler := &QuickViewHandler{model3DGLBTaskSvc: taskSvc}
	capability := &service.QuickViewCapability{
		TenantID: 7, ItemFingerprint: fingerprint, Locator: locator,
		SourceKind: service.QuickViewSourceKindModel3D,
	}
	source := service.QuickViewSource{
		EngineID: 26,
		Model3D:  &service.Model3DGLBSource{Format: "ifc", SourceSizeBytes: 1024},
	}

	if _, _, err := handler.createAndExecuteModel3DGLBTask(context.Background(), 1, capability, source, false); !errors.Is(err, service.ErrExistingResultActionRequired) {
		t.Fatalf("unconfirmed quick-view action error = %v", err)
	}
	var count int64
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&count).Error; err != nil {
		t.Fatalf("count unconfirmed executions: %v", err)
	}
	if count != 0 {
		t.Fatalf("unconfirmed quick-view action created %d executions", count)
	}

	reusedTaskID, executionID, err := handler.createAndExecuteModel3DGLBTask(context.Background(), 1, capability, source, true)
	if err != nil {
		t.Fatalf("confirmed quick-view action: %v", err)
	}
	if reusedTaskID != task.ID || executionID == "" {
		t.Fatalf("confirmed quick-view action task=%d execution=%q, want task %d", reusedTaskID, executionID, task.ID)
	}
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&count).Error; err != nil {
		t.Fatalf("count confirmed executions: %v", err)
	}
	if count != 1 {
		t.Fatalf("confirmed quick-view action execution count = %d, want 1", count)
	}
}

func TestQuickViewFeatureCollectionConvertsWKTToGeoJSON(t *testing.T) {
	itemID := uint(99)
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         "addp://engine/26/path/shp/farmland.shp?type=file&item_id=99",
			ItemID:          &itemID,
			ItemFingerprint: "fingerprint-99",
		},
	}
	tablePreview := &models.TablePreview{
		Rows: []map[string]interface{}{
			{"id": 1, "name": "alpha", "geometry": "POINT (120 30)"},
		},
		Total:           1,
		Page:            1,
		PageSize:        1,
		GeometryColumn:  "geometry",
		GeometryColumns: []string{"geometry"},
		SourceSRID:      4326,
	}

	collection, err := quickViewFeatureCollection(result, tablePreview, "")
	if err != nil {
		t.Fatalf("quickViewFeatureCollection returned error: %v", err)
	}
	if collection["type"] != "FeatureCollection" {
		t.Fatalf("type = %v, want FeatureCollection", collection["type"])
	}
	actualFeatures, ok := collection["features"].([]gin.H)
	if !ok {
		t.Fatalf("features type = %T, want []gin.H", collection["features"])
	}
	if len(actualFeatures) != 1 {
		t.Fatalf("features length = %d, want 1", len(actualFeatures))
	}
	geometry, ok := actualFeatures[0]["geometry"].(gin.H)
	if !ok {
		t.Fatalf("geometry type = %T, want gin.H", actualFeatures[0]["geometry"])
	}
	if geometry["type"] != "Point" {
		t.Fatalf("geometry.type = %v, want Point", geometry["type"])
	}
	metadata, ok := collection["metadata"].(gin.H)
	if !ok {
		t.Fatalf("metadata type = %T, want gin.H", collection["metadata"])
	}
	if metadata["locator"] != result.Metadata.Locator {
		t.Fatalf("metadata.locator = %v, want %s", metadata["locator"], result.Metadata.Locator)
	}
	if metadata["item_fingerprint"] != result.Metadata.ItemFingerprint {
		t.Fatalf("metadata.item_fingerprint = %v, want %s", metadata["item_fingerprint"], result.Metadata.ItemFingerprint)
	}
}

func TestQuickViewFeatureCollectionUsesRequestedGeometryColumn(t *testing.T) {
	tablePreview := &models.TablePreview{
		Rows: []map[string]interface{}{
			{
				"id": 1,
				"geom": map[string]interface{}{
					"type":        "Point",
					"coordinates": []interface{}{120.0, 30.0},
				},
			},
		},
		Total:           1,
		Page:            1,
		PageSize:        1,
		GeometryColumns: []string{"geom"},
		SourceSRID:      4326,
	}

	collection, err := quickViewFeatureCollection(nil, tablePreview, "geom")
	if err != nil {
		t.Fatalf("quickViewFeatureCollection returned error: %v", err)
	}
	actualFeatures, ok := collection["features"].([]gin.H)
	if !ok {
		t.Fatalf("features type = %T, want []gin.H", collection["features"])
	}
	if len(actualFeatures) != 1 {
		t.Fatalf("features length = %d, want 1", len(actualFeatures))
	}
	properties, ok := actualFeatures[0]["properties"].(gin.H)
	if !ok {
		t.Fatalf("properties type = %T, want gin.H", actualFeatures[0]["properties"])
	}
	if _, exists := properties["geom"]; exists {
		t.Fatal("geometry column leaked into feature properties")
	}
}

func TestQuickViewSourceFromPreviewCarriesCRSDefinition(t *testing.T) {
	definition := &datatype.CRSDefinition{
		ID:                 "ADDP:CRS:custom",
		DefinitionEncoding: datatype.CRSDefinitionEncodingESRIWKT,
		Definition:         `PROJCS["Custom_CRS"]`,
		Source:             datatype.CRSDefinitionSourceSidecarPRJ,
	}
	tablePreview := &models.TablePreview{
		EngineID:            26,
		EngineType:          "nfs",
		GeometryColumn:      "geometry",
		GeometryColumns:     []string{"geometry"},
		SourceCRS:           definition.ID,
		SourceCRSDefinition: definition,
		Extent:              []float64{1, 2, 3, 4},
		Total:               127,
	}

	source := quickViewSourceFromPreview(
		"addp://engine/26/path/shp/custom-crs.shp?type=file&item_id=99",
		nil,
		&preview.PreviewResult{},
		tablePreview,
	)

	if source.SpatialMeta == nil {
		t.Fatal("SpatialMeta is nil")
	}
	if source.SpatialMeta.SourceCRS != definition.ID || source.SpatialMeta.SourceCRSDefinition != definition {
		t.Fatalf("CRS = %q/%#v, want %q/%#v", source.SpatialMeta.SourceCRS, source.SpatialMeta.SourceCRSDefinition, definition.ID, definition)
	}
}

func TestQuickViewSourceFromPreviewDetectsRasterTIFFObject(t *testing.T) {
	locator := "addp://engine/26/path/rasters/small.tif?type=file&item_id=99"
	tablePreview := &models.TablePreview{
		EngineID:   26,
		EngineType: "nfs",
		Object: &models.ObjectPreview{
			EngineID: 26,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "media",
					"format":    "tiff",
				},
				"storage": map[string]interface{}{
					"total_size":    int64(8 * 1024 * 1024),
					"physical_path": "rasters/small.tif",
				},
				"type_info": map[string]interface{}{
					"media": map[string]interface{}{
						"width":      int64(2048),
						"height":     int64(2048),
						"band_count": int64(3),
					},
				},
				"format_info": map[string]interface{}{
					"tiff": map[string]interface{}{
						"profile": "geotiff",
					},
				},
				"capabilities": map[string]interface{}{
					"spatial": map[string]interface{}{
						"srid":        4326,
						"extent":      []interface{}{110.0, 20.0, 120.0, 30.0},
						"extent_srid": 4326,
					},
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-small-tiff",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.Raster == nil {
		t.Fatal("Raster is nil, want TIFF raster source")
	}
	if source.DirectFlatGeobuf || source.FlatGeobufURL != "" || source.CanTile {
		t.Fatalf("source routing = direct_flatgeobuf:%v flatgeobuf_url:%q can_tile:%v, want raster-only", source.DirectFlatGeobuf, source.FlatGeobufURL, source.CanTile)
	}
	if source.EngineID != 26 || source.Identity.Locator != locator || source.Identity.ItemFingerprint != "fp-small-tiff" {
		t.Fatalf("source identity = engine:%d locator:%q fingerprint:%q", source.EngineID, source.Identity.Locator, source.Identity.ItemFingerprint)
	}
	if source.Raster.Profile != "geotiff" || source.Raster.Width != 2048 || source.Raster.Height != 2048 {
		t.Fatalf("raster facts = %#v, want geotiff 2048x2048", source.Raster)
	}
	if !strings.Contains(source.Raster.PreviewURL, "/api/v1/manager/storage-stream?") {
		t.Fatalf("preview_url = %q, want manager storage stream URL", source.Raster.PreviewURL)
	}
	if !strings.Contains(source.Raster.PreviewURL, "locator=") || !strings.Contains(source.Raster.PreviewURL, "storage_ref=rasters%2Fsmall.tif") {
		t.Fatalf("preview_url = %q, want file storage_ref", source.Raster.PreviewURL)
	}
}

func TestQuickViewSourceFromPreviewDetectsSingleOSGBObject(t *testing.T) {
	locator := "addp://engine/26/path/3d/single-osgb/Tile_4_L20_00010t3.osgb?type=file&item_id=10282"
	tablePreview := &models.TablePreview{
		EngineID:   26,
		EngineType: "nfs",
		Object: &models.ObjectPreview{
			EngineID: 26,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "model_3d",
					"format":    "osgb",
					"layout":    "single",
				},
				"storage": map[string]interface{}{
					"total_size": int64(612396),
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-single-osgb",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.Model3D == nil {
		t.Fatal("Model3D is nil, want single OSGB quick view source")
	}
	if source.Raster != nil || source.DirectFlatGeobuf || source.FlatGeobufURL != "" || source.CanTile {
		t.Fatalf("source routing = raster:%v direct_flatgeobuf:%v flatgeobuf_url:%q can_tile:%v, want model3d-only", source.Raster != nil, source.DirectFlatGeobuf, source.FlatGeobufURL, source.CanTile)
	}
	if source.EngineID != 26 || source.Identity.Locator != locator || source.Identity.ItemFingerprint != "fp-single-osgb" {
		t.Fatalf("source identity = engine:%d locator:%q fingerprint:%q", source.EngineID, source.Identity.Locator, source.Identity.ItemFingerprint)
	}
	if source.Model3D.Format != "osgb" || source.Model3D.Layout != "single" || source.Model3D.SourceSizeBytes != 612396 {
		t.Fatalf("model3d facts = %#v, want single osgb facts", source.Model3D)
	}
}

func TestQuickViewSourceFromPreviewDetectsOSGBSceneWholeItem(t *testing.T) {
	locator := "addp://engine/26/path/3d/辽庆州白塔OSGB?type=file&item_id=10281"
	tablePreview := &models.TablePreview{
		EngineID:   26,
		EngineType: "nfs",
		Object: &models.ObjectPreview{
			EngineID: 26,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "model_3d",
					"format":    "osgb_scene",
					"layout":    "whole",
				},
				"storage": map[string]interface{}{
					"total_size": int64(1749426479),
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "75333d5dac9eb53a52f6104b263183d1935619f556665750fdc621c29c4ffea8",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.Model3D == nil {
		t.Fatal("Model3D is nil, want OSGB Scene model3d tiles source")
	}
	if source.DirectFlatGeobuf || source.FlatGeobufURL != "" || source.CanTile {
		t.Fatalf("source routing = direct_flatgeobuf:%v flatgeobuf_url:%q can_tile:%v, want model3d-only", source.DirectFlatGeobuf, source.FlatGeobufURL, source.CanTile)
	}
	if source.EngineID != 26 || source.Model3D.Format != "osgb_scene" || source.Model3D.Layout != "whole" || source.Model3D.SourceSizeBytes != 1749426479 {
		t.Fatalf("model3d source = engine:%d facts:%#v, want OSGB Scene whole item facts", source.EngineID, source.Model3D)
	}
}

func TestQuickViewSourceFromPreviewDetectsSingleOBJObject(t *testing.T) {
	locator := "addp://engine/26/path/3d/obj/AssaultRifle/AssaultRifle_01.obj?type=file&item_id=10366"
	tablePreview := &models.TablePreview{
		EngineID:   26,
		EngineType: "nfs",
		Object: &models.ObjectPreview{
			EngineID: 26,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "model_3d",
					"format":    "obj",
					"layout":    "single",
				},
				"storage": map[string]interface{}{
					"total_size": int64(2048),
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-obj",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.Model3D == nil {
		t.Fatal("Model3D is nil, want single OBJ quick view source")
	}
	if source.Raster != nil || source.DirectFlatGeobuf || source.FlatGeobufURL != "" || source.CanTile {
		t.Fatalf("source routing = raster:%v direct_flatgeobuf:%v flatgeobuf_url:%q can_tile:%v, want model3d-only", source.Raster != nil, source.DirectFlatGeobuf, source.FlatGeobufURL, source.CanTile)
	}
	if source.Model3D.Format != "obj" || source.Model3D.Layout != "single" || source.Model3D.SourceSizeBytes != 2048 {
		t.Fatalf("model3d facts = %#v, want single obj facts", source.Model3D)
	}
}

func TestQuickViewSourceFromPreviewDetectsDirectGLBObject(t *testing.T) {
	locator := "addp://engine/26/path/3d/glb/ABeautifulGame.glb?type=file&item_id=10320"
	previewURL := "/api/v1/manager/storage-stream?engine_id=26&storage_ref=3d%2Fglb%2FABeautifulGame.glb"
	tablePreview := &models.TablePreview{
		EngineID:   26,
		EngineType: "nfs",
		Object: &models.ObjectPreview{
			EngineID: 26,
			URL:      previewURL,
			Content: &models.ObjectPreviewContent{
				URL: previewURL,
			},
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "model_3d",
					"format":    "glb",
					"layout":    "single",
				},
				"storage": map[string]interface{}{
					"total_size": int64(53670500),
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-glb",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.Model3D == nil {
		t.Fatal("Model3D is nil, want direct GLB model3d source")
	}
	if source.Raster != nil || source.DirectFlatGeobuf || source.FlatGeobufURL != "" || source.CanTile {
		t.Fatalf("source routing = raster:%v direct_flatgeobuf:%v flatgeobuf_url:%q can_tile:%v, want model3d-only", source.Raster != nil, source.DirectFlatGeobuf, source.FlatGeobufURL, source.CanTile)
	}
	if source.Model3D.Format != "glb" || source.Model3D.Layout != "single" || source.Model3D.SourceSizeBytes != 53670500 {
		t.Fatalf("model3d facts = %#v, want direct glb facts", source.Model3D)
	}
	if source.Model3D.PreviewURL != previewURL {
		t.Fatalf("preview_url = %q, want %q", source.Model3D.PreviewURL, previewURL)
	}
}

func TestQuickViewSourceFromPreviewDetectsSingleIFCObject(t *testing.T) {
	locator := "addp://engine/26/path/3d/ifc/building.ifc?type=file&item_id=10988"
	tablePreview := &models.TablePreview{
		EngineID:   26,
		EngineType: "nfs",
		Object: &models.ObjectPreview{
			EngineID: 26,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "model_3d",
					"format":    "ifc",
					"layout":    "single",
				},
				"storage": map[string]interface{}{
					"total_size": int64(4096),
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-ifc",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.Model3D == nil {
		t.Fatal("Model3D is nil, want single IFC quick view source")
	}
	if source.Raster != nil || source.DirectFlatGeobuf || source.FlatGeobufURL != "" || source.CanTile {
		t.Fatalf("source routing = raster:%v direct_flatgeobuf:%v flatgeobuf_url:%q can_tile:%v, want model3d-only", source.Raster != nil, source.DirectFlatGeobuf, source.FlatGeobufURL, source.CanTile)
	}
	if source.Model3D.Format != "ifc" || source.Model3D.Layout != "single" || source.Model3D.SourceSizeBytes != 4096 {
		t.Fatalf("model3d facts = %#v, want single ifc facts", source.Model3D)
	}
}

func TestQuickViewSourceFromPreviewDetectsGaussianSplatPLYObject(t *testing.T) {
	locator := "addp://engine/26/path/3d/gaussian/model.ply?type=file&item_id=10901"
	tablePreview := &models.TablePreview{
		EngineID:   26,
		EngineType: "nfs",
		Object: &models.ObjectPreview{
			EngineID: 26,
			URL:      "/api/v1/manager/storage-stream?engine_id=26&storage_ref=3d%2Fgaussian%2Fmodel.ply",
			Content: &models.ObjectPreviewContent{
				URL: "/api/v1/manager/storage-stream?engine_id=26&storage_ref=3d%2Fgaussian%2Fmodel.ply",
			},
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "gaussian_splat",
					"format":    "ply",
					"layout":    "single",
				},
				"type_info": map[string]interface{}{
					"gaussian_splat": map[string]interface{}{
						"splat_count": int64(256),
					},
				},
				"storage": map[string]interface{}{
					"total_size": int64(8192),
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-gaussian-ply",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.GaussianSplat == nil {
		t.Fatal("GaussianSplat is nil, want gaussian splat KSplat source")
	}
	if source.Model3D != nil || source.Raster != nil || source.DirectFlatGeobuf || source.FlatGeobufURL != "" || source.CanTile {
		t.Fatalf("source routing = model3d:%v raster:%v direct_flatgeobuf:%v flatgeobuf_url:%q can_tile:%v, want gaussian-only", source.Model3D != nil, source.Raster != nil, source.DirectFlatGeobuf, source.FlatGeobufURL, source.CanTile)
	}
	if source.GaussianSplat.Format != "ply" || source.GaussianSplat.SplatCount != 256 || source.GaussianSplat.SourceSizeBytes != 8192 {
		t.Fatalf("gaussian facts = %#v, want ply gaussian facts", source.GaussianSplat)
	}
	if source.GaussianSplat.PreviewURL != "/api/v1/manager/storage-stream?engine_id=26&storage_ref=3d%2Fgaussian%2Fmodel.ply" {
		t.Fatalf("preview_url = %q, want object content URL", source.GaussianSplat.PreviewURL)
	}
}

func TestApplyLocatorQuickViewURLsSetsRasterMosaicTileTemplate(t *testing.T) {
	capability := &service.QuickViewCapability{
		Locator:      "addp://engine/26/path/mosaics/srtm?type=directory&item_id=99",
		RenderSource: service.QuickViewRenderSourceRasterMosaic,
		QuickView: service.QuickViewRenderInfo{
			RenderSource: service.QuickViewRenderSourceRasterMosaic,
		},
	}

	applyLocatorQuickViewURLs(capability)

	if !strings.Contains(capability.QuickView.TileURLTemplate, "/api/v1/manager/raster_mosaic/tiles/{z}/{x}/{y}.png?") {
		t.Fatalf("tile_url_template = %q, want raster mosaic tile endpoint", capability.QuickView.TileURLTemplate)
	}
	if !strings.Contains(capability.QuickView.TileURLTemplate, "locator=") {
		t.Fatalf("tile_url_template = %q, want encoded locator", capability.QuickView.TileURLTemplate)
	}
	if !strings.Contains(capability.QuickView.TileURLTemplate, "gamma=0.6") {
		t.Fatalf("tile_url_template = %q, want default gamma", capability.QuickView.TileURLTemplate)
	}
}

func TestQuickViewCapabilityJSONCarriesBackendStateControls(t *testing.T) {
	capability := service.QuickViewCapability{
		TenantID:          7,
		ItemFingerprint:   "fp-ifc",
		Locator:           "addp://engine/26/path/3d/ifc/building.ifc?type=file&item_id=10988",
		SourceKind:        service.QuickViewSourceKindModel3D,
		CanUseQuickView:   false,
		PreferredMode:     models.PreviewModeBasicPreview,
		RecommendedMode:   models.PreviewModeBasicPreview,
		ActiveMode:        models.PreviewModeBasicPreview,
		Status:            service.QuickViewStatusUnavailable,
		UnavailableReason: "requires_glb_generation",
		AvailableActions:  []string{service.QuickViewActionGenerateModel3DGLB},
	}

	body, err := json.Marshal(capability)
	if err != nil {
		t.Fatalf("marshal quick view capability: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal quick view capability: %v", err)
	}
	if payload["source_kind"] != service.QuickViewSourceKindModel3D {
		t.Fatalf("source_kind = %#v, want model_3d", payload["source_kind"])
	}
	actions, ok := payload["available_actions"].([]interface{})
	if !ok || len(actions) != 1 || actions[0] != service.QuickViewActionGenerateModel3DGLB {
		t.Fatalf("available_actions = %#v, want GLB generation action", payload["available_actions"])
	}
}

func TestQuickViewCapabilityJSONCarriesEmptyAvailableActions(t *testing.T) {
	capability := service.QuickViewCapability{
		TenantID:         7,
		SourceKind:       service.QuickViewSourceKindVector,
		CanUseQuickView:  false,
		PreferredMode:    models.PreviewModeBasicPreview,
		RecommendedMode:  models.PreviewModeBasicPreview,
		ActiveMode:       models.PreviewModeBasicPreview,
		Status:           service.QuickViewStatusUnavailable,
		AvailableActions: []string{},
	}

	body, err := json.Marshal(capability)
	if err != nil {
		t.Fatalf("marshal quick view capability: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal quick view capability: %v", err)
	}
	actions, ok := payload["available_actions"].([]interface{})
	if !ok || len(actions) != 0 {
		t.Fatalf("available_actions = %#v, want empty array", payload["available_actions"])
	}
}

func TestModel3DGLBTaskConfigFromQuickViewUsesCapabilityIdentity(t *testing.T) {
	capability := &service.QuickViewCapability{
		TenantID:        7,
		ItemFingerprint: "fp-ifc",
		Locator:         "addp://engine/26/path/3d/ifc/building.ifc?type=file&item_id=10441",
		SourceKind:      service.QuickViewSourceKindModel3D,
	}
	source := service.QuickViewSource{
		EngineID: 26,
		Model3D: &service.Model3DGLBSource{
			Format:          "ifc",
			SourceSizeBytes: 1234,
		},
	}

	config, err := model3DGLBTaskConfigFromQuickView(capability, source)
	if err != nil {
		t.Fatalf("model3DGLBTaskConfigFromQuickView returned error: %v", err)
	}
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		t.Fatalf("config.source = %#v, want JSON map", config["source"])
	}
	if sourceMap["item_locator"] != capability.Locator {
		t.Fatalf("item_locator = %#v, want %s", sourceMap["item_locator"], capability.Locator)
	}
	if sourceMap["source_engine_id"] != uint(26) {
		t.Fatalf("source_engine_id = %#v, want 26", sourceMap["source_engine_id"])
	}
	if sourceMap["item_fingerprint"] != "fp-ifc" {
		t.Fatalf("item_fingerprint = %#v, want fp-ifc", sourceMap["item_fingerprint"])
	}
	if sourceMap["item_id"] != uint(10441) {
		t.Fatalf("item_id = %#v, want 10441", sourceMap["item_id"])
	}
	if sourceMap["format"] != "ifc" {
		t.Fatalf("format = %#v, want ifc", sourceMap["format"])
	}
	if sourceMap["source_size_bytes"] != int64(1234) {
		t.Fatalf("source_size_bytes = %#v, want 1234", sourceMap["source_size_bytes"])
	}
}

func TestRasterCOGTaskConfigFromQuickViewUsesCapabilityIdentity(t *testing.T) {
	capability := &service.QuickViewCapability{
		TenantID:        7,
		ItemFingerprint: "fp-raster",
		Locator:         "addp://engine/26/path/rasters/dem.tif?type=file&item_id=42",
		SourceKind:      service.QuickViewSourceKindRaster,
	}
	source := service.QuickViewSource{
		EngineID: 26,
		Raster: &service.RasterQuickViewSource{
			Profile:    "tiff",
			SizeBytes:  8192,
			Width:      512,
			Height:     256,
			BandCount:  1,
			SourceSRID: 4326,
			Extent:     []float64{110, 20, 120, 30},
			ExtentSRID: 4326,
		},
	}

	config, err := rasterCOGTaskConfigFromQuickView(capability, source)
	if err != nil {
		t.Fatalf("rasterCOGTaskConfigFromQuickView returned error: %v", err)
	}
	target, ok := asJSONMap(config["target"])
	if !ok {
		t.Fatalf("config.target = %#v, want JSON map", config["target"])
	}
	if target["locator"] != capability.Locator {
		t.Fatalf("locator = %#v, want %s", target["locator"], capability.Locator)
	}
	if target["source_engine_id"] != uint(26) {
		t.Fatalf("source_engine_id = %#v, want 26", target["source_engine_id"])
	}
	if target["item_fingerprint"] != "fp-raster" {
		t.Fatalf("item_fingerprint = %#v, want fp-raster", target["item_fingerprint"])
	}
	if target["item_id"] != uint(42) {
		t.Fatalf("item_id = %#v, want 42", target["item_id"])
	}
	raster, ok := asJSONMap(config["raster"])
	if !ok {
		t.Fatalf("config.raster = %#v, want JSON map", config["raster"])
	}
	if raster["source_profile"] != "tiff" || raster["width"] != int64(512) || raster["height"] != int64(256) {
		t.Fatalf("raster config = %#v, want source profile and dimensions", raster)
	}
	cog, ok := asJSONMap(config["cog"])
	if !ok {
		t.Fatalf("config.cog = %#v, want JSON map", config["cog"])
	}
	if cog["compression"] != "DEFLATE" || cog["blocksize"] != 512 {
		t.Fatalf("cog config = %#v, want default COG options", cog)
	}
}

func TestGaussianSplatKSplatTaskConfigFromQuickViewUsesCapabilityIdentity(t *testing.T) {
	capability := &service.QuickViewCapability{
		TenantID:        7,
		ItemFingerprint: "fp-gaussian",
		Locator:         "addp://engine/26/path/3dgs/sample.ply?type=file&item_id=109",
		SourceKind:      service.QuickViewSourceKindGaussianSplat,
	}
	source := service.QuickViewSource{
		EngineID: 26,
		GaussianSplat: &service.GaussianSplatKSplatSource{
			Format:          "ply",
			SourceSizeBytes: 4096,
		},
	}

	config, err := gaussianSplatKSplatTaskConfigFromQuickView(capability, source)
	if err != nil {
		t.Fatalf("gaussianSplatKSplatTaskConfigFromQuickView returned error: %v", err)
	}
	sourceMap, ok := asJSONMap(config["source"])
	if !ok {
		t.Fatalf("config.source = %#v, want JSON map", config["source"])
	}
	if sourceMap["item_locator"] != capability.Locator {
		t.Fatalf("item_locator = %#v, want %s", sourceMap["item_locator"], capability.Locator)
	}
	if sourceMap["source_engine_id"] != uint(26) {
		t.Fatalf("source_engine_id = %#v, want 26", sourceMap["source_engine_id"])
	}
	if sourceMap["item_fingerprint"] != "fp-gaussian" {
		t.Fatalf("item_fingerprint = %#v, want fp-gaussian", sourceMap["item_fingerprint"])
	}
	if sourceMap["item_id"] != uint(109) {
		t.Fatalf("item_id = %#v, want 109", sourceMap["item_id"])
	}
	if sourceMap["format"] != "ply" {
		t.Fatalf("format = %#v, want ply", sourceMap["format"])
	}
	if sourceMap["source_size_bytes"] != int64(4096) {
		t.Fatalf("source_size_bytes = %#v, want 4096", sourceMap["source_size_bytes"])
	}
}

func TestRasterMosaicTileStyleQueryParsing(t *testing.T) {
	gamma, ok := parseOptionalPositiveFloat("0.7")
	if !ok || gamma != 0.7 {
		t.Fatalf("gamma = %v ok=%v, want 0.7 true", gamma, ok)
	}
	if _, ok := parseOptionalPositiveFloat("bad"); ok {
		t.Fatal("invalid gamma ok = true, want false")
	}

	minValue, maxValue, ok := parseOptionalDisplayRange("10", "4200")
	if !ok || minValue == nil || *minValue != 10 || maxValue == nil || *maxValue != 4200 {
		t.Fatalf("display range = %v %v ok=%v, want 10 4200 true", minValue, maxValue, ok)
	}
	if _, _, ok := parseOptionalDisplayRange("4200", "10"); ok {
		t.Fatal("invalid display range ok = true, want false")
	}

	invert, ok := parseOptionalBool("true")
	if !ok || !invert {
		t.Fatalf("invert = %v ok=%v, want true true", invert, ok)
	}
	if _, ok := parseOptionalBool("maybe"); ok {
		t.Fatal("invalid invert ok = true, want false")
	}
}

func TestQuickViewSourceFromPreviewDetectsRasterTIFFObjectCatalogItem(t *testing.T) {
	locator := "addp://engine/9/path/addp/image/srtm_40_01.tif?type=object&item_id=254"
	tablePreview := &models.TablePreview{
		EngineID:   9,
		EngineType: "minio",
		Object: &models.ObjectPreview{
			EngineID: 9,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"layout":    "multi",
					"data_type": "media",
					"format":    "tiff",
					"refs": []interface{}{
						map[string]interface{}{"path": "addp/image/srtm_40_01.tif", "role": "main", "primary": true},
						map[string]interface{}{"path": "addp/image/srtm_40_01.tfw", "role": "world_file"},
						map[string]interface{}{"path": "addp/image/srtm_40_01.hdr", "role": "header"},
						map[string]interface{}{"path": "addp/image/srtm_40_01.tif.aux.xml", "role": "auxiliary_metadata"},
					},
				},
				"storage": map[string]interface{}{
					"bucket":        "addp",
					"path":          "image/",
					"name":          "srtm_40_01.tif",
					"physical_path": "addp/image/srtm_40_01.tif",
					"total_size":    int64(160),
				},
				"type_info": map[string]interface{}{
					"media": map[string]interface{}{
						"width":      int64(3601),
						"height":     int64(3601),
						"band_count": int64(1),
					},
				},
				"format_info": map[string]interface{}{
					"tiff": map[string]interface{}{
						"profile": "geotiff",
					},
				},
				"capabilities": map[string]interface{}{
					"spatial": map[string]interface{}{
						"srid":        4326,
						"extent":      []interface{}{40.0, 0.0, 41.0, 1.0},
						"extent_srid": 4326,
					},
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-object-tiff",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.Raster == nil {
		t.Fatal("Raster is nil, want object catalog TIFF raster source")
	}
	if source.DirectFlatGeobuf || source.CanTile {
		t.Fatalf("source routing = direct_flatgeobuf:%v can_tile:%v, want raster-only", source.DirectFlatGeobuf, source.CanTile)
	}
	if source.EngineID != 9 || source.Identity.Locator != locator || source.Identity.ItemFingerprint != "fp-object-tiff" {
		t.Fatalf("source identity = engine:%d locator:%q fingerprint:%q", source.EngineID, source.Identity.Locator, source.Identity.ItemFingerprint)
	}
	if source.Raster.Profile != "geotiff" || source.Raster.SizeBytes != 160 || source.Raster.Width != 3601 || source.Raster.Height != 3601 {
		t.Fatalf("raster facts = %#v, want object GeoTIFF facts", source.Raster)
	}
	if !strings.Contains(source.Raster.PreviewURL, "/api/v1/manager/storage-stream?") {
		t.Fatalf("preview_url = %q, want manager storage stream URL", source.Raster.PreviewURL)
	}
	if !strings.Contains(source.Raster.PreviewURL, "locator=") || !strings.Contains(source.Raster.PreviewURL, "storage_ref=addp%2Fimage%2Fsrtm_40_01.tif") {
		t.Fatalf("preview_url = %q, want object bucket/path storage_ref", source.Raster.PreviewURL)
	}
}

func TestQuickViewSourceFromPreviewAllowsTileCacheGenerationForSpatialFile(t *testing.T) {
	locator := "addp://engine/26/path/shp/farmland.shp?type=file&item_id=100"
	tablePreview := &models.TablePreview{
		EngineType:      "nfs",
		Total:           73090,
		GeometryColumn:  "geometry",
		GeometryColumns: []string{"geometry"},
		SourceSRID:      4326,
		SRID:            4326,
		Extent:          []float64{110, 20, 120, 30},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-farmland",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if !source.DirectFlatGeobuf {
		t.Fatal("direct_flatgeobuf = false, want true for vector spatial file")
	}
	if !source.CanTile {
		t.Fatal("can_tile = false, want true for spatial file with numeric SRID and extent")
	}
	if source.Schema != "" || source.Table != "" {
		t.Fatalf("schema/table = %q/%q, want empty for file source", source.Schema, source.Table)
	}
	if source.EngineID != 26 {
		t.Fatalf("engine_id = %d, want locator engine id 26", source.EngineID)
	}
	if source.SpatialMeta == nil || source.SpatialMeta.GeomColumn != "geometry" || source.SpatialMeta.SRID != 4326 {
		t.Fatalf("spatial_meta = %#v, want geometry column and numeric SRID", source.SpatialMeta)
	}
}

func TestVectorTileCacheTaskConfigFromQuickViewUsesLocatorIdentityForFile(t *testing.T) {
	locator := "addp://engine/26/path/shp/farmland.shp?type=file&item_id=100"
	capability := &service.QuickViewCapability{
		TenantID:             7,
		ItemFingerprint:      "fp-farmland",
		Locator:              locator,
		CanGenerateTileCache: true,
		RenderFacts: &service.QuickViewRenderFacts{
			ZoomRecommendation: &service.ZoomRecommendation{
				MinZoom: 2,
				MaxZoom: 10,
			},
		},
	}
	source := service.QuickViewSource{
		EngineID:         26,
		DirectFlatGeobuf: true,
		SpatialMeta: &service.SpatialMetadataResult{
			GeomColumn:  "geometry",
			SRID:        4326,
			Extent:      []float64{110, 20, 120, 30},
			ExtentSRID:  4326,
			RecordCount: 73090,
		},
	}

	config, err := vectorTileCacheTaskConfigFromQuickView(capability, source)
	if err != nil {
		t.Fatalf("build vector tile cache config: %v", err)
	}
	target, _ := asJSONMap(config["target"])
	if target["source_engine_id"] != uint(26) || target["locator"] != locator || target["item_fingerprint"] != "fp-farmland" {
		t.Fatalf("target identity = %#v, want locator identity", target)
	}
	if target["source_kind"] != "file" || target["full_name"] != "shp/farmland.shp" {
		t.Fatalf("target locator facts = %#v, want file/full_name", target)
	}
	if _, ok := target["schema"]; ok {
		t.Fatalf("schema is present for file target: %#v", target)
	}
	if _, ok := target["table"]; ok {
		t.Fatalf("table is present for file target: %#v", target)
	}
	tile, _ := asJSONMap(config["tile"])
	if tile["format"] != "mvt" || tile["target_srid"] != commonSpatial.SRIDWebMercator || tile["source_srid"] != 4326 {
		t.Fatalf("tile config = %#v, want mvt 4326->3857", tile)
	}
	options, _ := asJSONMap(config["options"])
	if options["geometry_column"] != "geometry" {
		t.Fatalf("geometry_column = %v, want geometry", options["geometry_column"])
	}
}

func TestPositivePathIntAcceptsMVTSuffixOnY(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "y", Value: "52.mvt"}}

	if got := positivePathInt(c, "y", 0, 0); got != 52 {
		t.Fatalf("positivePathInt() = %d, want 52", got)
	}
	if c.IsAborted() {
		t.Fatal("positivePathInt aborted valid MVT tile y parameter")
	}
}

func TestPositivePathIntAcceptsGinParamNameWithMVTSuffix(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "y.mvt", Value: "419.mvt"}}

	if got := positivePathInt(c, "y", 0, 0); got != 419 {
		t.Fatalf("positivePathInt() = %d, want 419", got)
	}
	if c.IsAborted() {
		t.Fatal("positivePathInt aborted valid Gin MVT tile y parameter")
	}
}

func TestApplyTileResponseHeadersExposesRealtimeTimeoutRecommendation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	applyTileResponseHeaders(c, &service.TileResponse{
		Data:                  []byte{},
		FromCache:             false,
		Duration:              2500 * time.Millisecond,
		RenderSource:          service.QuickViewRenderSourceRealtimeTile,
		Status:                service.TileStatusTimeout,
		PerformanceMode:       service.RealtimeTilePerformanceReady3857Target,
		TimeoutBudget:         2500 * time.Millisecond,
		TimeoutRecommendation: service.RealtimeTileRecommendationTileCacheGeneration,
		TimeoutRetryPolicy:    service.RealtimeTileTimeoutRetryTTL,
		TimeoutRetryAfter:     45 * time.Second,
	})

	headers := w.Header()
	if got := headers.Get("X-ADDP-Tile-Performance-Mode"); got != service.RealtimeTilePerformanceReady3857Target {
		t.Fatalf("performance header = %s, want %s", got, service.RealtimeTilePerformanceReady3857Target)
	}
	if got := headers.Get("X-ADDP-Tile-Recommendation"); got != service.RealtimeTileRecommendationTileCacheGeneration {
		t.Fatalf("recommendation header = %s, want %s", got, service.RealtimeTileRecommendationTileCacheGeneration)
	}
	if got := headers.Get("X-ADDP-Tile-Retry-Policy"); got != service.RealtimeTileTimeoutRetryTTL {
		t.Fatalf("retry policy header = %s, want %s", got, service.RealtimeTileTimeoutRetryTTL)
	}
	if got := headers.Get("X-ADDP-Tile-Timeout-Budget-MS"); got != "2500" {
		t.Fatalf("timeout budget header = %s, want 2500", got)
	}
	if got := headers.Get("Retry-After"); got != "45" {
		t.Fatalf("Retry-After = %s, want 45", got)
	}
	if got := headers.Get("Access-Control-Expose-Headers"); !strings.Contains(got, "X-ADDP-Tile-Recommendation") ||
		!strings.Contains(got, "X-ADDP-Tile-Timeout-Budget-MS") ||
		!strings.Contains(got, "X-ADDP-Tile-Retry-Policy") ||
		!strings.Contains(got, "Retry-After") {
		t.Fatalf("expose headers = %q, want tile recommendation headers exposed", got)
	}
}
