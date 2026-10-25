package scanruntime

import (
	"context"
	"fmt"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func (s *FilesystemCatalogRuntime) scanDirectoryForTest(ctx context.Context, reader plugin.ContentReadableProvider, provider plugin.EngineCatalogProvider, conn plugin.ConnectionInfo, resource *commonModels.Engine, tenant uint, path string, node *models.MetaNode, _ bool, term, depth string, force bool) (int, scanflow.ExtractionCounts, error) {
	if scanflow.FileProgressFrom(ctx) == nil {
		ctx, _ = scanflow.WithFileProgress(ctx, nil)
	}
	plan, err := s.prepareFileScan(ctx, reader, provider, conn, resource, []string{path}, depth)
	if err != nil {
		return 0, scanflow.ExtractionCounts{}, err
	}
	return s.scanPreparedDirectory(ctx, reader, provider, conn, resource, tenant, path, node, term, depth, force, plan)
}

type recursiveScanReporter struct {
	snapshots []models.ScanProgress
	onReport  func(models.ScanProgress)
}

func (r *recursiveScanReporter) SetTotal(int)                                     {}
func (r *recursiveScanReporter) Advance(string, int, int, map[string]interface{}) {}
func (r *recursiveScanReporter) Message(string)                                   {}
func (r *recursiveScanReporter) Report(p models.ScanProgress) {
	r.snapshots = append(r.snapshots, p)
	if r.onReport != nil {
		r.onReport(p)
	}
}

type countingFileCatalog struct {
	filesystemScanTestProvider
	calls          map[string]int
	recursiveCalls int
}

func (p *countingFileCatalog) ListChildren(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.calls[path.StringPath()]++
	if opts.Recursive {
		p.recursiveCalls++
	}
	return p.filesystemScanTestProvider.ListChildren(ctx, conn, path, opts)
}
func progressFile(path string) plugin.EngineCatalogEntry {
	name := path[strings.LastIndex(path, "/")+1:]
	size := int64(16)
	return plugin.EngineCatalogEntry{Name: name, Path: plugin.FileItemPath(26, path), Term: "file", Kind: "file", Role: plugin.EngineCatalogRoleLeaf, Storage: &plugin.EngineCatalogStorageFacts{Path: path, SizeBytes: &size}}
}
func progressDirectory(path string) plugin.EngineCatalogEntry {
	name := path[strings.LastIndex(path, "/")+1:]
	return plugin.EngineCatalogEntry{Name: name, Path: plugin.FileDirectoryPath(26, path), Term: "dir", Kind: "dir", Role: plugin.EngineCatalogRoleBranch, Storage: &plugin.EngineCatalogStorageFacts{Path: path}}
}
func TestRecursiveFileScanSharesInventoryAndCountsCompositeItems(t *testing.T) {
	for _, depth := range []string{"basic", "deep"} {
		t.Run(depth, func(t *testing.T) {
			db := openObjectCatalogScanTestDB(t)
			repo := metaRepo.NewScanRepository(db)
			runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
			provider := &countingFileCatalog{calls: map[string]int{}, filesystemScanTestProvider: filesystemScanTestProvider{
				entriesByPath: map[string][]plugin.EngineCatalogEntry{
					"3d":     {progressDirectory("3d/gis"), progressDirectory("3d/doc"), progressFile("3d/.DS_Store")},
					"3d/gis": {progressFile("3d/gis/roads.shp"), progressFile("3d/gis/roads.shx"), progressFile("3d/gis/roads.dbf")},
					"3d/doc": {progressFile("3d/doc/readme.txt")},
				}, content: "not a shapefile", contentByPath: map[string]string{"3d/doc/readme.txt": "hello"}}}
			plugin.Register(provider)
			t.Cleanup(func() { plugin.Unregister(provider.Type()) })
			recorder := &recursiveScanReporter{}
			result, err := runtime.ScanPaths(context.Background(), &commonModels.Engine{ID: 26, EngineType: provider.Type()}, 1, []string{"3d"}, depth, true, recorder)
			if (depth == "deep") != (err != nil) {
				t.Fatalf("depth=%s err=%v", depth, err)
			}
			if provider.recursiveCalls != 0 || len(provider.calls) != 3 {
				t.Fatalf("repeated inventory: %+v", provider)
			}
			for path, calls := range provider.calls {
				if calls != 1 {
					t.Fatalf("%s listed %d times", path, calls)
				}
			}
			last := recorder.snapshots[len(recorder.snapshots)-1]
			if last.Phase != "finalizing" || last.FilesDiscovered != 4 || last.DirectoriesDiscovered != 3 || last.ItemsPlanned != 2 || last.ItemsCompleted != 2 {
				t.Fatalf("counts=%+v result=%+v", last, result)
			}
			if depth == "basic" && (last.ItemsSucceeded != 2 || last.ItemsFailed != 0) {
				t.Fatalf("basic=%+v", last)
			}
			if depth == "deep" && (last.ItemsSucceeded != 1 || last.ItemsFailed != 1) {
				t.Fatalf("deep=%+v", last)
			}
			progressedBeforeEnd := false
			for _, p := range recorder.snapshots {
				if depth == "basic" && (p.Operation == "hashing" || p.Operation == "enriching") {
					t.Fatalf("basic scan reported deep content work: %+v", p)
				}
				if p.Phase == "processing" && p.ItemsCompleted == 1 {
					progressedBeforeEnd = true
				}
			}
			if !progressedBeforeEnd {
				t.Fatal("recursive progress only reported after the root completed")
			}
		})
	}
}

func TestWholeLayoutInventoryIncludesMembersButPlansOneItem(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	provider := &countingFileCatalog{calls: map[string]int{}, filesystemScanTestProvider: filesystemScanTestProvider{
		entriesByPath: map[string][]plugin.EngineCatalogEntry{
			"3d":       {progressFile("3d/tileset.json"), progressDirectory("3d/tiles")},
			"3d/tiles": {progressFile("3d/tiles/root.b3dm"), progressFile("3d/tiles/other.b3dm")},
		}, contentByPath: map[string]string{"3d/tileset.json": `{"asset":{"version":"1.1"},"geometricError":200,"root":{"boundingVolume":{"region":[1,0.5,1.1,0.6,0,120]},"geometricError":0,"content":{"uri":"tiles/root.b3dm"}}}`},
	}}
	plugin.Register(provider)
	t.Cleanup(func() { plugin.Unregister(provider.Type()) })
	recorder := &recursiveScanReporter{}
	_, err := runtime.ScanPaths(context.Background(), &commonModels.Engine{ID: 26, EngineType: provider.Type()}, 1, []string{"3d"}, "deep", true, recorder)
	if err != nil {
		t.Fatal(err)
	}
	p := recorder.snapshots[len(recorder.snapshots)-1]
	if p.FilesDiscovered != 3 || p.DirectoriesDiscovered != 2 || p.ItemsPlanned != 1 || p.ItemsCompleted != 1 || p.ItemsSucceeded != 1 {
		t.Fatalf("whole member/item counts: %+v", p)
	}
	if provider.calls["3d"] != 1 || provider.calls["3d/tiles"] != 1 {
		t.Fatalf("whole scope enumerated again: %+v", provider.calls)
	}
}

func TestRecursiveCancellationDoesNotCountUnstartedItems(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), metaRepo.NewScanRepository(db), nil)
	provider := &countingFileCatalog{calls: map[string]int{}, filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{
		"3d": {progressFile("3d/first.txt"), progressFile("3d/second.txt")},
	}}}
	plugin.Register(provider)
	t.Cleanup(func() { plugin.Unregister(provider.Type()) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := &recursiveScanReporter{onReport: func(p models.ScanProgress) {
		if p.ItemsCompleted == 1 {
			cancel()
		}
	}}
	_, err := runtime.ScanPaths(ctx, &commonModels.Engine{ID: 26, EngineType: provider.Type()}, 1, []string{"3d"}, "basic", true, recorder)
	if err == nil {
		t.Fatal("cancelled scope succeeded")
	}
	p := recorder.snapshots[len(recorder.snapshots)-1]
	if p.ItemsPlanned != 2 || p.ItemsCompleted != 1 || p.ItemsSucceeded != 1 || p.ItemsFailed != 0 {
		t.Fatalf("unstarted items counted: %+v", p)
	}
}

func TestNestedLogicalItemsAreProcessedOnceByCanonicalParent(t *testing.T) {
	for _, depth := range []string{"basic", "deep"} {
		t.Run(depth, func(t *testing.T) {
			db := openObjectCatalogScanTestDB(t)
			repo := metaRepo.NewScanRepository(db)
			indexer := &boundaryIndexer{}
			runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, indexer)
			provider := &gltfFileStructureFixture{primary: "3d/gltf/model/scene.gltf", allowContent: true, filesystemScanTestProvider: filesystemScanTestProvider{
				entriesByPath: map[string][]plugin.EngineCatalogEntry{
					"3d":            {progressDirectory("3d/gltf")},
					"3d/gltf":       {progressDirectory("3d/gltf/model")},
					"3d/gltf/model": {progressFile("3d/gltf/model/scene.gltf"), progressFile("3d/gltf/model/mesh.bin"), progressFile("3d/gltf/model/readme.txt")},
				}, contentByPath: map[string]string{"3d/gltf/model/scene.gltf": `{"asset":{"version":"2.0"},"buffers":[{"uri":"mesh.bin","byteLength":4}]}`, "3d/gltf/model/mesh.bin": "0000", "3d/gltf/model/readme.txt": "independent"}}}
			provider.raw = []byte(provider.contentByPath[provider.primary])
			*provider.entriesByPath["3d/gltf/model"][0].Storage.SizeBytes = int64(len(provider.raw))
			*provider.entriesByPath["3d/gltf/model"][1].Storage.SizeBytes = 4
			plugin.Register(provider)
			t.Cleanup(func() { plugin.Unregister(provider.Type()) })
			// Existing primary identity must be retained, and old claimed component removed.
			_, node, err := runtime.ensureFilesystemScanRoot(1, &commonModels.Engine{ID: 26, EngineType: provider.Type()}, provider, "3d/gltf/model")
			if err != nil {
				t.Fatal(err)
			}
			primary, err := repo.UpsertItemWithDepth(1, 26, node, "file", "scene.gltf", "3d/gltf/model/scene.gltf", models.JSONMap{"item": map[string]interface{}{"layout": "single"}}, nil, nil, nil, "basic")
			if err != nil {
				t.Fatal(err)
			}
			_, err = repo.UpsertItemWithDepth(1, 26, node, "file", "mesh.bin", "3d/gltf/model/mesh.bin", models.JSONMap{"item": map[string]interface{}{"layout": "single"}}, nil, nil, nil, "basic")
			if err != nil {
				t.Fatal(err)
			}
			wantItems, wantRemoved := int64(2), 1
			for pass := 0; pass < 2; pass++ {
				recorder := &recursiveScanReporter{}
				result, err := runtime.ScanPaths(context.Background(), &commonModels.Engine{ID: 26, EngineType: provider.Type()}, 1, []string{"3d"}, depth, true, recorder)
				if err != nil {
					t.Fatal(err)
				}
				snapshot := recorder.snapshots[len(recorder.snapshots)-1]
				var count int64
				db.Model(&models.MetaItem{}).Where("engine_id = 26").Count(&count)
				if snapshot.ItemsPlanned != wantItems || snapshot.ItemsCompleted != wantItems || int64(result.Items) != wantItems || count != wantItems {
					t.Fatalf("pass %d duplicate plan/reconciliation: %+v result=%+v count=%d", pass, snapshot, result, count)
				}
				got, found, err := repo.FindItemByFullName(1, 26, "3d/gltf/model/scene.gltf")
				if err != nil || !found || got.ID != primary.ID || got.NodeID != node.ID {
					t.Fatalf("unstable canonical identity: %+v %v", got, err)
				}
			}
			if len(indexer.deleted) != wantRemoved {
				t.Fatal(fmt.Sprintf("component cleanup duplicated: %+v", indexer.deleted))
			}
		})
	}
}
