package scanruntime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	"github.com/jonas-p/go-shp"
)

type boundaryScanProvider struct {
	filesystemScanTestProvider
	object bool
	reads  *int
}

type boundaryIndexer struct{ deleted []string }

func (i *boundaryIndexer) DeleteItemFromIndex(tenantID, engineID uint, fingerprint string) {
	i.deleted = append(i.deleted, fmt.Sprintf("%d/%d/%s", tenantID, engineID, fingerprint))
}
func (*boundaryIndexer) IndexTechnicalMetadata(context.Context, *commonModels.Engine, uint, *models.MetaNode, *models.MetaItem) bool {
	return true
}
func (*boundaryIndexer) IndexCatalogContent(context.Context, *commonModels.Engine, uint, *models.MetaItem, string, bool) bool {
	return true
}

func (p boundaryScanProvider) Type() string { return "content-boundary-test" }
func (p boundaryScanProvider) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	if p.object {
		return plugin.ObjectCatalogModel()
	}
	return plugin.FileCatalogModel()
}
func (p boundaryScanProvider) Capabilities() plugin.EngineCapabilities {
	if p.object {
		return plugin.NewObjectCapabilities(p.Type())
	}
	return plugin.EngineCapabilities{}
}
func (p boundaryScanProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if p.object && target.StringPath() == "bucket" {
		entry := plugin.ObjectBucketCatalogEntry(plugin.ObjectRootPath(26), "bucket")
		return &entry, nil
	}
	return nil, nil
}
func (p boundaryScanProvider) OpenContent(ctx context.Context, conn plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	*p.reads++
	return p.filesystemScanTestProvider.OpenContent(ctx, conn, target, opts)
}

func boundaryShapefileContent(t *testing.T) map[string]string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "roads")
	w, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetFields([]shp.Field{shp.StringField("NAME", 16)}); err != nil {
		t.Fatal(err)
	}
	row := w.Write(&shp.Point{X: 1, Y: 2})
	if err := w.WriteAttribute(int(row), 0, "road"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
		t.Fatal(err)
	}
	content := map[string]string{}
	for _, ext := range []string{".shp", ".shx", ".dbf"} {
		data, err := os.ReadFile(base + ext)
		if err != nil {
			t.Fatal(err)
		}
		content["roads"+ext] = string(data)
	}
	content["roads.cpg"] = "UTF-8"
	content["readme.txt"] = "independent"
	return content
}

// Covers the actual directory and explicit refs persistence routes with real format parsing.
func TestContentBoundaryStableAcrossScanDepths(t *testing.T) {
	fixture := boundaryShapefileContent(t)
	for _, object := range []bool{false, true} {
		for _, refs := range []bool{false, true} {
			for _, dir := range []string{"", "gis"} {
				for _, legacy := range []bool{false, true} {
					t.Run(fmt.Sprintf("object=%t/refs=%t/dir=%s/legacy=%t", object, refs, dir, legacy), func(t *testing.T) {
						db := openObjectCatalogScanTestDB(t)
						repo := metaRepo.NewScanRepository(db)
						reads := 0
						provider := boundaryScanProvider{object: object, reads: &reads}
						provider.contentByPath = map[string]string{}
						var files []metaitem.StorageFileRef
						var resources []scanresource.StorageResource
						group := models.ScanRefGroup{}
						qualify := func(p string) string {
							if object {
								return "bucket/" + p
							}
							return p
						}
						for _, name := range []string{"roads.shp", "roads.shx", "roads.dbf", "roads.cpg", "readme.txt"} {
							p := path.Join(dir, name)
							provider.contentByPath[qualify(p)] = fixture[name]
							files = append(files, metaitem.StorageFileRef{Name: name, Path: p, Size: int64(len(fixture[name]))})
							resources = append(resources, objectResourceForTest(26, "bucket", p, int64(len(fixture[name])), ""))
							if name != "readme.txt" {
								group.Refs = append(group.Refs, models.ScanRef{Path: qualify(p)})
							}
						}
						group.Primary = qualify(path.Join(dir, "roads.shp"))
						pluginRegisterForTest(t, provider)
						log := slog.New(slog.NewTextHandler(io.Discard, nil))
						indexer := &boundaryIndexer{}
						fileRuntime := NewFilesystemCatalogRuntime(db, log, repo, indexer)
						objectRuntime := NewObjectStorageCatalogRuntime(db, log, repo, indexer)
						term, nodeName, nodeType := "file", dir, "dir"
						if object {
							term, nodeName, nodeType = "object", "bucket", "bucket"
						}
						node, err := repo.UpsertNode(1, 26, nil, nodeType, nodeName, &nodeName, models.JSONMap{})
						if err != nil {
							t.Fatal(err)
						}
						// Seed the previous per-file result and records that this claim must preserve.
						for _, name := range []string{"roads.shp", "roads.shx", "roads.dbf", "roads.cpg", "roads.prj", "readme.txt"} {
							if !legacy && name != "roads.prj" && name != "readme.txt" {
								continue
							}
							fullName := qualify(path.Join(dir, name))
							attrs := models.JSONMap{"item": map[string]interface{}{"layout": "single"}}
							if _, err := repo.UpsertItemWithDepth(1, 26, node, term, name, fullName, attrs, nil, nil, nil, models.ScannedDepthDeep); err != nil {
								t.Fatal(err)
							}
						}
						original, _, err := repo.FindItemByFullName(1, 26, group.Primary)
						if err != nil {
							t.Fatal(err)
						}
						var id uint
						var fingerprint string
						var firstRefs interface{}
						for i, depth := range []string{"basic", "deep", "basic"} {
							before := reads
							if refs {
								if object {
									_, err = objectRuntime.ScanRefGroups(context.Background(), &commonModels.Engine{ID: 26, EngineType: provider.Type()}, 1, []models.ScanRefGroup{group}, depth, false, nil)
								} else {
									_, err = fileRuntime.ScanRefGroups(context.Background(), &commonModels.Engine{ID: 26, EngineType: provider.Type()}, 1, []models.ScanRefGroup{group}, depth, false, nil)
								}
							} else if object {
								_, _, err = objectRuntime.persistObjectResources(context.Background(), &commonModels.Engine{ID: 26, EngineType: provider.Type()}, 1, 26, node, resources, newObjectCatalogScanState(), true, depth, false, dir, nil, term)
							} else {
								provider.entriesByPath = map[string][]plugin.EngineCatalogEntry{}
								for _, f := range files {
									provider.entriesByPath[dir] = append(provider.entriesByPath[dir], plugin.EngineCatalogEntry{Name: f.Name, Path: plugin.FileItemPath(26, f.Path), Kind: "file", Term: "file", Role: plugin.EngineCatalogRoleLeaf, Storage: &plugin.EngineCatalogStorageFacts{Path: f.Path, SizeBytes: &f.Size}})
								}
								_, _, err = fileRuntime.scanDirectory(context.Background(), provider, provider, nil, &commonModels.Engine{ID: 26, EngineType: provider.Type()}, 1, dir, node, dir == "", term, depth, false)
							}
							if err != nil {
								t.Fatal(err)
							}
							if depth == "basic" && reads != before {
								t.Fatalf("basic opened %d content streams", reads-before)
							}
							item, found, err := repo.FindItemByFullName(1, 26, group.Primary)
							if err != nil || !found {
								t.Fatalf("primary missing: %v", err)
							}
							wantPaths := []string{}
							for _, ref := range group.Refs {
								wantPaths = append(wantPaths, ref.Path)
							}
							assertShapefileLogicalItem(t, item.Attributes, wantPaths, []string{qualify(path.Join(dir, "roads.prj"))})
							itemRefs := commonJSON.Section(item.Attributes, "item")["refs"]
							if i == 0 {
								if original != nil && (original.ID != item.ID || original.Fingerprint != item.Fingerprint) {
									t.Fatal("component convergence replaced primary identity")
								}
								id, fingerprint, firstRefs = item.ID, item.Fingerprint, itemRefs
								if item.ScannedDepth != "basic" {
									t.Fatalf("old single depth retained: %s", item.ScannedDepth)
								}
							} else if item.ID != id || item.Fingerprint != fingerprint || !reflect.DeepEqual(firstRefs, itemRefs) {
								t.Fatal("item identity or refs changed with scan depth")
							}
							if i > 0 && (item.ScannedDepth != "deep" || len(commonJSON.InterfaceSlice(commonJSON.Section(item.Attributes, "type_info.table")["fields"])) == 0) {
								t.Fatal("deep facts were not enriched/preserved")
							}
							var all []models.MetaItem
							if err := db.Unscoped().Where("tenant_id = 1 AND engine_id = 26").Find(&all).Error; err != nil {
								t.Fatal(err)
							}
							if len(all) != 3 {
								t.Fatalf("items = %v, want primary, unseen prj and readme", all)
							}
							wantDeleted := []string{}
							if legacy {
								for _, name := range []string{"roads.shx", "roads.dbf", "roads.cpg"} {
									wantDeleted = append(wantDeleted, fmt.Sprintf("1/26/%s", commonModels.GenerateItemFingerprint(26, qualify(path.Join(dir, name)))))
								}
							}
							if len(indexer.deleted) != len(wantDeleted) {
								t.Fatalf("index deletions = %v, want %v", indexer.deleted, wantDeleted)
							}
							for _, fingerprint := range wantDeleted {
								if !containsString(indexer.deleted, fingerprint) {
									t.Fatalf("missing exact component index deletion %s", fingerprint)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestContentBoundaryDoesNotClaimRecursiveSiblingRefs(t *testing.T) {
	files := []metaitem.StorageFileRef{{Name: "roads.shp", Path: "gis/roads.shp"}, {Name: "roads.shx", Path: "gis/roads.shx"}}
	recursive := append(append([]metaitem.StorageFileRef{}, files...), metaitem.StorageFileRef{Name: "roads.dbf", Path: "gis/attributes/roads.dbf"})
	result, err := metaitem.ResolveItems(context.Background(), metaitem.DirectoryResolveInput{DirPath: "gis", Files: files, RecursiveFiles: recursive})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 0 || len(result.Claims) != 0 {
		t.Fatalf("claimed incomplete or cross-directory refs: %#v", result)
	}
}

func TestBasicContentBoundaryDoesNotReadManifest(t *testing.T) {
	reads := 0
	provider := boundaryScanProvider{reads: &reads, filesystemScanTestProvider: filesystemScanTestProvider{content: "invalid"}}
	files := []metaitem.StorageFileRef{{Name: "mosaic.addp.json", Path: "gis/mosaic.addp.json"}, {Name: "a.tif", Path: "gis/a.tif"}}
	result, err := metaitem.ResolveItems(context.Background(), metaitem.DirectoryResolveInput{DirPath: "gis", Files: files, ContentReader: scanflow.ContentReaderForDepth(provider, "basic")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Exclusive || reads != 0 {
		t.Fatal("basic recognized a content-dependent manifest")
	}
}
