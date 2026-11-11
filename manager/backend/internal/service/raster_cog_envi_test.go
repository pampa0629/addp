package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type rasterSnapshotFixture struct {
	item *commonModels.MetaItem
	err  error
}

func (f rasterSnapshotFixture) GetItemSnapshot(context.Context, uint, uint, uint, string) (*commonModels.MetaItem, error) {
	return f.item, f.err
}

func TestRasterCOGRejectsENVIIdentityDisguisedAsSingleFile(t *testing.T) {
	task := newRasterCOGTaskDefinition()
	if err := normalizeRasterCOGTask(task, "manager"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(task.Config)
	target, _ := asJSONMap(task.Config["target"])
	item := &commonModels.MetaItem{ID: 100, TenantID: 7, EngineID: 26, FullName: "rasters/large.tif", Fingerprint: stringFromConfig(target["item_fingerprint"]), Attributes: commonModels.JSONMap{"item": commonModels.JSONMap{"format": "envi"}}}
	svc := NewRasterCOGTaskService(nil)
	for _, meta := range []rasterSnapshotFixture{{item: item}, {err: errors.New("unavailable")}} {
		svc.sourceMeta = meta
		execution := &commonExecution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Source: "manager", TriggerType: "manual"}
		if _, err := svc.freezeRasterSourceExecution(t.Context(), &repository.FileTaskSnapshot{TaskID: 1, TenantID: 7, Version: 1, Config: raw}, execution, "addp_at_human"); err == nil {
			t.Fatal("single-file path bypassed actual identity or missing Meta")
		}
	}
	svc.sourceMeta = rasterSnapshotFixture{err: commonAPI.ErrNotFound}
	execution := &commonExecution.TaskExecution{}
	managed, err := svc.freezeRasterSourceExecution(t.Context(), &repository.FileTaskSnapshot{TaskID: 1, TenantID: 7, Version: 1, Config: raw}, execution, "")
	if err == nil || managed {
		t.Fatal("TIFF without a Meta snapshot bypassed source authorization", err)
	}
}

func TestENVICacheRequiresBothContentVersions(t *testing.T) {
	current := &RasterQuickViewSource{Format: "envi", HeaderSHA256: strings.Repeat("a", 64), RasterSHA256: strings.Repeat("b", 64)}
	result := &models.RasterCOG{Status: models.RasterCOGStatusReady, Metadata: commonModels.JSONMap{"source_version": commonModels.JSONMap{"header_sha256": current.HeaderSHA256, "raster_sha256": current.RasterSHA256}}}
	if !rasterCOGFactsMatch(result, QuickViewIdentity{}, current, 0) {
		t.Fatal("unchanged exact pair invalidated")
	}
	for _, change := range []string{"header_sha256", "raster_sha256"} {
		result.Metadata["source_version"].(commonModels.JSONMap)[change] = strings.Repeat("c", 64)
		if rasterCOGFactsMatch(result, QuickViewIdentity{}, current, 0) {
			t.Fatal("changed component reused COG", change)
		}
	}
}

type rasterInputOwnerFixture struct {
	prepared, finished bool
	preparation        repository.FileInputPreparation
	settled, cleaned   bool
}

func (o *rasterInputOwnerFixture) prepare(_ context.Context, p repository.FileInputPreparation) error {
	o.prepared = true
	o.preparation = p
	return nil
}
func (o *rasterInputOwnerFixture) finish(_ context.Context, p repository.FileInputPreparation, settled, cleaned bool) error {
	if !o.prepared || p.PreparationID != o.preparation.PreparationID {
		return errors.New("unowned finish")
	}
	o.finished = true
	o.settled = settled
	o.cleaned = cleaned
	return nil
}

func TestENVIStagingSharesExactManifestAndCleansBothFiles(t *testing.T) {
	for _, outcome := range []string{"success", "native failure", "changed bytes", "unknown put"} {
		t.Run(outcome, func(t *testing.T) {
			store := &stagingModelStore{}
			if outcome == "unknown put" {
				store.putErr = errors.New("unknown response")
			}
			owner := &rasterInputOwnerFixture{}
			stager := fileSourceStager{objectStore: store, minioEndpoint: "127.0.0.1:19000", minioAccessKey: "test", minioSecretKey: "secret", defaultBucket: "manager", taskType: commonExecution.TaskTypeRasterCOGGeneration}
			lease := commonExecution.Lease{TenantID: 7, ExecutionID: uuid.NewString(), Attempt: 1}
			main := stagingModelFile("原名 %.img", "1234")
			if outcome == "changed bytes" {
				main.Open = func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("12345")), nil }
			}
			consumed := false
			err := stager.withSource(t.Context(), lease, "envi", workflowaccess.KindFile, main.RelativePath, []fileStagingFile{main, stagingModelFile("原名 %.HDR", "ENVI")}, owner, func(_ context.Context, source workflowaccess.Source) error {
				consumed = true
				if !owner.prepared || len(owner.preparation.Objects) != 2 || source.Entrypoint != "" || source.Kind != "file" || len(source.RelatedFiles) != 1 || source.RelatedFiles[0].Role != "header" || !strings.HasSuffix(source.RelatedFiles[0].Access.Object, "/原名 %.HDR") {
					t.Fatal("lost exact pair")
				}
				if !strings.HasPrefix(source.Access.Object, "temp/raster-input/tenant_7/") {
					t.Fatal("wrong purpose namespace")
				}
				if outcome == "native failure" {
					return errors.New("native failed")
				}
				return nil
			})
			if (err == nil) != (outcome == "success") || !owner.finished || len(store.objects) != 0 || (outcome == "unknown put" && owner.settled) || (outcome != "unknown put" && !owner.cleaned) {
				t.Fatalf("lost cleanup/unknown writer evidence: outcome=%s err=%v owner=%+v objects=%v", outcome, err, owner, store.objects)
			}
			if (outcome == "changed bytes" || outcome == "unknown put") && consumed {
				t.Fatal("unproven pair reached native runtime")
			}
		})
	}
}

func TestENVICOGFreezesOnlyCurrentNativeRasterFacts(t *testing.T) {
	for _, change := range []string{"none", "unknown location", "wrong units CRS", "false dimensions", "changed version"} {
		t.Run(change, func(t *testing.T) {
			tenant := uint(7)
			main, header := "imagery/grid.img", "imagery/grid.HDR"
			item := &commonModels.MetaItem{ID: 100, TenantID: tenant, EngineID: 26, Name: "grid.img", FullName: main, ItemType: "file", Fingerprint: commonModels.GenerateItemFingerprint(26, main), Attributes: commonModels.JSONMap{
				"item":         commonModels.JSONMap{"format": "envi", "data_type": "media", "layout": "multi", "refs": []any{map[string]any{"path": main, "role": "raster", "required": true, "primary": true}, map[string]any{"path": header, "role": "header", "required": true, "primary": false}}},
				"storage":      commonModels.JSONMap{"physical_path": main},
				"type_info":    commonModels.JSONMap{"media": commonModels.JSONMap{"width": json.Number("256"), "height": json.Number("256"), "size_bytes": json.Number("196900")}},
				"format_info":  commonModels.JSONMap{"envi": commonModels.JSONMap{"profile": "envi-standard-pair/v1", "scan_complete": true, "composition_complete": true, "header_path": header, "header_sha256": strings.Repeat("a", 64), "raster_sha256": strings.Repeat("b", 64), "band_count": json.Number("3")}},
				"capabilities": commonModels.JSONMap{"spatial": commonModels.JSONMap{"srid": json.Number("4326"), "extent": []any{json.Number("113"), json.Number("27.9744"), json.Number("113.0256"), json.Number("28")}}},
			}}
			cfg := commonModels.JSONMap{"target": commonModels.JSONMap{"source_engine_id": 26, "item_id": 100, "item_fingerprint": item.Fingerprint, "locator": "addp://engine/26/path/imagery/grid.img?type=file&item_id=100"}, "raster": commonModels.JSONMap{"source_format": "envi", "source_profile": "envi-standard-pair/v1", "source_size_bytes": 196900, "header_sha256": strings.Repeat("a", 64), "raster_sha256": strings.Repeat("b", 64), "width": 256, "height": 256, "band_count": 3, "source_srid": 4326, "extent_srid": 4326, "extent": []float64{113., 27.9744, 113.0256, 28.}}}
			switch change {
			case "unknown location":
				delete(item.Attributes["capabilities"].(commonModels.JSONMap), "spatial")
			case "wrong units CRS":
				cfg["raster"].(commonModels.JSONMap)["source_srid"] = 3857
			case "false dimensions":
				cfg["raster"].(commonModels.JSONMap)["width"] = 512
			case "changed version":
				cfg["raster"].(commonModels.JSONMap)["raster_sha256"] = strings.Repeat("c", 64)
			}
			raw, _ := json.Marshal(cfg)
			svc := NewRasterCOGTaskService(nil)
			svc.SetSourceAuthorizationClients(rasterSnapshotFixture{item: item}, &glbSourceEngineFixture{engine: &commonModels.Engine{ID: 26, TenantID: &tenant, EngineType: "nfs", Version: 5, LifecycleState: "active", ConnectionStatus: "online"}}, &commonClient.SystemExecutionAuthorizationClient{}, &commonClient.SystemServiceClient{})
			execution := &commonExecution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Source: "manager", TriggerType: "manual"}
			ctx := commonExecution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "31", nil))
			managed, err := svc.freezeRasterSourceExecution(ctx, &repository.FileTaskSnapshot{TaskID: 11, TenantID: 7, Version: 3, Config: raw}, execution, "addp_at_human")
			if change != "none" {
				if err == nil {
					t.Fatal("caller replaced trusted native facts", change)
				}
				return
			}
			if err != nil || !managed || execution.ActorPrincipalID == nil || *execution.ActorPrincipalID != 31 {
				t.Fatal("exact native source did not freeze with its User", err)
			}
			frozen, _ := json.Marshal(execution.ExecutionConfig)
			composition, err := enviCompositionFromMeta(7, 26, 100, item.Fingerprint, "addp://engine/26/path/imagery/grid.img?type=file&item_id=100", item)
			if err != nil {
				t.Fatal(err)
			}
			scope, err := commonExecution.NewManagerFileReadScope(frozen, "11", "3", "5", composition)
			if err != nil || scope.Purpose() != commonExecution.TaskTypeRasterCOGGeneration {
				t.Fatal("wrong frozen purpose", err)
			}
		})
	}
}
