package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
)

func TestGRIBMapCacheIdentityIncludesProfileMessageAndCurrentSourceVersion(t *testing.T) {
	version := "sha256:" + strings.Repeat("a", 64)
	info := map[string]interface{}{"scan_complete": true, "encoding": "GRIB2", "source_version": version}
	attrs := map[string]interface{}{
		"item":        map[string]interface{}{"format": "grib", "layout": "single", "data_type": "container"},
		"format_info": map[string]interface{}{"grib": info},
		"type_info": map[string]interface{}{"container": map[string]interface{}{"resource_count": 1, "children": []map[string]interface{}{
			{"name": "message-1", "format": "grib", "child_kind": "array", "native": map[string]interface{}{"map_profile": GRIBTemperatureMapProfile}},
			{"name": "message-2", "format": "grib", "child_kind": "array", "native": map[string]interface{}{"map_profile": GRIBPrecipitationMapProfile}},
			{"name": "message-3", "format": "grib", "child_kind": "array", "native": map[string]interface{}{}},
		}}},
	}
	first := GRIBRasterQuickViewSource(attrs, "message-1")
	second := GRIBRasterQuickViewSource(attrs, "message-2")
	if first == nil || second == nil || GRIBRasterQuickViewSource(attrs, "message-3") != nil || GRIBRasterQuickViewSource(attrs, "message-01") != nil {
		t.Fatal("GRIB map did not enforce the selected native profile")
	}
	result := &models.RasterCOG{ID: 52, Status: models.RasterCOGStatusReady, SourceProfile: GRIBTemperatureMapProfile,
		Metadata: commonModels.JSONMap{"grib_map": commonModels.JSONMap{"message": first.Message, "source_version": first.SourceVersion}}}
	if !rasterCOGFactsMatch(result, QuickViewIdentity{}, first, 14) || rasterCOGFactsMatch(result, QuickViewIdentity{}, second, 14) {
		t.Fatal("raster COG crossed GRIB message identity")
	}
	mapInfo := renderInfoFromRasterCOG(result, first)
	if mapInfo.RasterMessage != first.Message || mapInfo.RasterSourceVersion != version || mapInfo.MapProfile != GRIBTemperatureMapProfile ||
		mapInfo.PreviewURL != "/api/v1/manager/raster_cog/52/content?source_version="+version || mapInfo.ExtentSRID != 0 {
		t.Fatalf("unexpected GRIB map delivery: %+v", mapInfo)
	}
	mismatched := *first
	mismatched.Profile = GRIBPrecipitationMapProfile
	if rasterCOGFactsMatch(result, QuickViewIdentity{}, &mismatched, 14) {
		t.Fatal("raster COG crossed GRIB map profiles")
	}
	result.SourceProfile = GRIBPrecipitationMapProfile
	result.Metadata["grib_map"] = commonModels.JSONMap{"message": second.Message, "source_version": second.SourceVersion}
	precipitation := renderInfoFromRasterCOG(result, second)
	if second.Profile != GRIBPrecipitationMapProfile || !rasterCOGFactsMatch(result, QuickViewIdentity{}, second, 14) ||
		precipitation.MapProfile != GRIBPrecipitationMapProfile || precipitation.PreviewURL != mapInfo.PreviewURL {
		t.Fatalf("unexpected precipitation delivery: %+v", precipitation)
	}
	info["source_version"] = "sha256:" + strings.Repeat("b", 64)
	if rasterCOGFactsMatch(result, QuickViewIdentity{}, GRIBRasterQuickViewSource(attrs, "message-2"), 14) {
		t.Fatal("raster COG crossed committed GRIB source version")
	}
}

func TestGRIBCOGStagesOnlyItsExactContainer(t *testing.T) {
	for _, fixture := range []struct {
		name, taskType, kind      string
		addIndex, conversionFails bool
	}{
		{name: "single GRIB", taskType: commonExecution.TaskTypeRasterCOGGeneration, kind: workflowaccess.KindFile},
		{name: "conversion failure", taskType: commonExecution.TaskTypeRasterCOGGeneration, kind: workflowaccess.KindFile, conversionFails: true},
		{name: "GLB purpose", taskType: commonExecution.TaskTypeModel3DGLBGeneration, kind: workflowaccess.KindFile},
		{name: "directory", taskType: commonExecution.TaskTypeRasterCOGGeneration, kind: workflowaccess.KindDirectory},
		{name: "external index", taskType: commonExecution.TaskTypeRasterCOGGeneration, kind: workflowaccess.KindFile, addIndex: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			store := &stagingModelStore{}
			owner := &rasterInputOwnerFixture{}
			stager := fileSourceStager{
				objectStore: store, taskType: fixture.taskType, defaultBucket: "manager",
				minioEndpoint: "127.0.0.1:19000", minioAccessKey: "test", minioSecretKey: "secret",
			}
			lease := commonExecution.Lease{TenantID: 7, ExecutionID: uuid.NewString(), Attempt: 1}
			main := stagingModelFile("forecast.GRIB2", "example GRIB bytes")
			main.Role = "container"
			files := []fileStagingFile{main}
			if fixture.addIndex {
				files = append(files, stagingModelFile("forecast.GRIB2.idx", "index"))
			}
			conversionErr := errors.New("conversion failed")
			consumed := false
			err := stager.withSource(t.Context(), lease, "grib", fixture.kind, main.RelativePath, files, owner, func(_ context.Context, source workflowaccess.Source) error {
				consumed = true
				if source.Format != "grib" || source.Kind != workflowaccess.KindFile || source.Entrypoint != "" || len(source.RelatedFiles) != 0 {
					t.Error("expected exact GRIB file without an entrypoint override or related files")
				}
				if !owner.prepared || len(owner.preparation.Objects) != 1 || store.objects["manager/"+source.Access.Object] != "example GRIB bytes" ||
					!strings.HasPrefix(source.Access.Object, "temp/raster-input/tenant_7/"+lease.ExecutionID+"/") ||
					!strings.HasSuffix(source.Access.Object, "/forecast.GRIB2") {
					t.Error("incorrect ownership or staged GRIB object")
				}
				if fixture.conversionFails {
					return conversionErr
				}
				return nil
			})
			wantConsumed := fixture.taskType == commonExecution.TaskTypeRasterCOGGeneration && fixture.kind == workflowaccess.KindFile && !fixture.addIndex
			if consumed != wantConsumed {
				t.Fatalf("consumed=%v, want %v: %v", consumed, wantConsumed, err)
			}
			if wantConsumed {
				if (fixture.conversionFails && !errors.Is(err, conversionErr)) || (!fixture.conversionFails && err != nil) ||
					!owner.finished || !owner.settled || !owner.cleaned || store.puts != 1 || len(store.objects) != 0 {
					t.Fatalf("missing conversion or cleanup evidence: err=%v owner=%+v objects=%v", err, owner, store.objects)
				}
			} else if err == nil || owner.prepared || store.puts != 0 {
				t.Fatalf("invalid input reached staging: err=%v owner=%+v puts=%d", err, owner, store.puts)
			}
		})
	}
}
