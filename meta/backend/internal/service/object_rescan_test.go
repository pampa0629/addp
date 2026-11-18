package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"reflect"
	"strings"
	"sync"
	"testing"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/s3"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

type rescanObjectCatalog struct {
	s3.S3Plugin
	keys []string
	fail string
}

func (p *rescanObjectCatalog) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if !opts.Recursive {
		return nil, errors.New("expected recursive object enumeration")
	}
	var entries []plugin.EngineCatalogEntry
	for _, key := range p.keys {
		if strings.HasPrefix("bucket/"+key, parent.StringPath()+"/") {
			entries = append(entries, rescanObjectEntry(parent.EngineID, key))
		}
	}
	if parent.StringPath() == p.fail {
		return entries, errors.New("object enumeration interrupted")
	}
	return entries, nil
}

func (p *rescanObjectCatalog) ResolvePath(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if path.Segments[len(path.Segments)-1].Kind != plugin.EngineCatalogKindObject {
		return p.S3Plugin.ResolvePath(ctx, conn, path)
	}
	for _, key := range p.keys {
		if path.StringPath() == "bucket/"+key {
			entry := rescanObjectEntry(path.EngineID, key)
			return &entry, nil
		}
	}
	return nil, nil
}

func (p *rescanObjectCatalog) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	for _, key := range p.keys {
		if path.StringPath() == "bucket/"+key {
			return io.NopCloser(strings.NewReader("test text")), nil
		}
	}
	return nil, errors.New("object absent")
}

func (p *rescanObjectCatalog) OpenRange(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	return p.OpenContent(ctx, conn, path, opts)
}

func rescanObjectEntry(engineID uint, key string) plugin.EngineCatalogEntry {
	return plugin.ObjectLeafCatalogEntry(plugin.ObjectDirectoryPath(engineID, "bucket", strings.TrimSuffix(key, path.Base(key))), plugin.StorageObjectFacts{
		Name: path.Base(key), Path: "bucket/" + key, Size: 9, ContentType: "text/plain",
	})
}

func TestObjectNodeRescanReconcilesMissingItems(t *testing.T) {
	exerciseObjectNodeRescan(t, func(t *testing.T) *gorm.DB { return metatest.OpenMetadataDB(t) })
}

func exerciseObjectNodeRescan(t *testing.T, openDB func(*testing.T) *gorm.DB) {
	t.Helper()
	for _, depth := range []string{"basic", "deep"} {
		for _, mode := range []string{"partial_deletion", "empty_prefix", "empty_bucket", "failed_enumeration", "precise_object"} {
			t.Run(depth+"/"+mode, func(t *testing.T) {
				const tenantID, engineID uint = 70, 90
				const removedKey, keptKey, outsideKey = "a_b/nested/removed.txt", "a_b/kept.txt", "axb/outside.txt"
				provider := &rescanObjectCatalog{keys: []string{removedKey, keptKey, outsideKey}}
				previous, err := plugin.Get(provider.Type())
				if err != nil {
					t.Fatal(err)
				}
				plugin.Register(provider)
				t.Cleanup(func() { plugin.Register(previous) })
				db := openDB(t)
				tree := newResourceTreeTestServiceWithDB(t, db, tenantID, engineID)
				fingerprint := func(key string) string { return commonmodels.GenerateItemFingerprint(engineID, "bucket/"+key) }
				indexed := map[string]bool{}
				deletionCalls := map[string]int{}
				dropDeleteResponse := false
				var indexLock sync.Mutex
				manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					indexLock.Lock()
					defer indexLock.Unlock()
					switch r.Method + " " + r.URL.Path {
					case "POST /api/v1/manager/runtime/content-documents/batch_update":
						var batch commonclient.ManagerContentBatch
						if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
							t.Error(err)
						}
						for _, document := range batch.Documents {
							indexed[document.DocumentID] = true
						}
					case "DELETE /api/v1/manager/runtime/content-documents":
						query := r.URL.Query()
						operation := query.Get("operation_id")
						if len(query) != 3 || query.Get("engine_id") != "90" || len(query["document_id"]) == 0 || operation == "" {
							t.Errorf("expected exact Manager deletion, got %v", query)
						}
						deletionCalls[operation]++
						if deletionCalls[operation] == 1 {
							for _, id := range query["document_id"] {
								delete(indexed, id)
							}
						}
						if dropDeleteResponse {
							dropDeleteResponse = false
							conn, _, err := http.NewResponseController(w).Hijack()
							if err != nil {
								t.Error(err)
							} else {
								_ = conn.Close()
							}
							return
						}
					default:
						t.Errorf("unexpected Manager request %s %s", r.Method, r.URL.Path)
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				defer manager.Close()
				client := commonclient.NewManagerContentClient(manager.URL, commonclient.ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
					if tenant != tenantID {
						t.Errorf("index tenant = %d", tenant)
					}
					return "fixture", nil
				}), manager.Client())
				scanService := NewScanService(db, tree.engineService)
				scanService.SetContentIndexClient(client)
				scan := func(opts scanflow.Options) error {
					opts.Context, opts.TenantID, opts.EngineID = t.Context(), tenantID, engineID
					_, err := scanService.ScanEngineWithOptions(opts)
					return err
				}
				if err := scan(scanflow.Options{CatalogPaths: []string{"bucket"}, ScanDepth: "basic", Force: true}); err != nil {
					t.Fatal(err)
				}
				var initial models.MetaItem
				if err := db.Where("fingerprint = ?", fingerprint(removedKey)).First(&initial).Error; err != nil {
					t.Fatal(err)
				}
				indexLock.Lock()
				if len(indexed) != 3 {
					t.Errorf("initial search projections = %v", indexed)
				}
				indexLock.Unlock()
				scope := "bucket/a_b"
				opts := scanflow.Options{CatalogPaths: []string{scope}, ScanDepth: depth}
				want := []string{keptKey, outsideKey}
				provider.keys = []string{keptKey, outsideKey}
				switch mode {
				case "empty_prefix":
					// The same key as the prefix belongs to the bucket, not the prefix node.
					provider.keys, want = []string{"a_b", outsideKey}, []string{"a_b", outsideKey}
					if err := scan(scanflow.Options{CatalogPaths: []string{scope}, ScanDepth: "basic"}); err != nil {
						t.Fatal(err)
					}
					var prefix models.MetaNode
					if err := db.Where("full_name = ? AND node_type = ?", scope, "prefix").First(&prefix).Error; err != nil {
						t.Fatal(err)
					}
					opts.CatalogPaths = nil
					if depth == "basic" {
						opts.NodeID = prefix.ID
					} else {
						locator := resourcetree.ResourceLocator{EngineID: engineID, NodeID: &prefix.ID, Path: strings.Split(scope, "/"), Type: "prefix"}
						opts.Targets = []string{locator.ToURI()}
					}
				case "empty_bucket":
					opts.CatalogPaths, provider.keys, want = []string{"bucket"}, nil, nil
				case "failed_enumeration":
					provider.fail = scope
					want = append(want, removedKey)
				case "precise_object":
					opts.CatalogPaths = []string{"bucket/" + keptKey}
					want = append(want, removedKey)
				}
				if err := scan(opts); (err != nil) != (mode == "failed_enumeration") {
					t.Fatalf("rescan error = %v", err)
				}
				indexLock.Lock()
				if len(deletionCalls) != 0 || !indexed[initial.Fingerprint] {
					t.Error("scan must leave delivery to the independent worker")
				}
				indexLock.Unlock()
				// Recreate the service after the Meta transaction, before any Manager call.
				scanService = NewScanService(db, tree.engineService)
				scanService.SetContentIndexClient(client)
				if err := scanService.deliverObjectIndexChanges(t.Context()); err != nil {
					t.Fatal(err)
				}
				root, err := tree.GetTree(t.Context(), tenantID, engineID, -1)
				if err != nil {
					t.Fatal(err)
				}
				wantIndex := map[string]bool{}
				for _, key := range want {
					wantIndex[fingerprint(key)] = true
				}
				facts := NewMetadataQueryService(db)
				visible := map[string]bool{}
				var recordItems func(*resourcetree.TreeNode)
				recordItems = func(node *resourcetree.TreeNode) {
					if _, ok := node.Metadata["item_id"]; ok {
						visible[node.Metadata["full_name"].(string)] = true
					}
					for _, child := range node.Children {
						recordItems(child)
					}
				}
				recordItems(root)
				for _, key := range []string{removedKey, keptKey, outsideKey, "a_b"} {
					id := fingerprint(key)
					if visible["bucket/"+key] != wantIndex[id] {
						t.Errorf("resource tree item %q visibility = %t, want %t", key, visible["bucket/"+key], wantIndex[id])
					}
					result, err := facts.QueryDataItemAuthorizationFacts(t.Context(), tenantID, authorizationfacts.AuthorizationFactsQuery{Fingerprints: []string{id}})
					if err != nil || (result.Data[0].Facts != nil) != wantIndex[id] {
						t.Errorf("search facts for %q = %v, error = %v, want visible=%t", key, result, err, wantIndex[id])
					}
				}
				indexLock.Lock()
				if !reflect.DeepEqual(indexed, wantIndex) {
					t.Errorf("Manager index projections = %v, want %v", indexed, wantIndex)
				}
				indexLock.Unlock()
				var stored models.MetaItem
				if err := db.Unscoped().First(&stored, initial.ID).Error; err != nil || stored.DeletedAt.Valid == wantIndex[initial.Fingerprint] {
					t.Fatalf("old Item soft-deletion state = %v, err = %v", stored.DeletedAt, err)
				}
				if mode == "partial_deletion" {
					provider.keys = append(provider.keys, removedKey)
					if err := scan(opts); err != nil {
						t.Fatal(err)
					}
					if err := db.First(&stored, initial.ID).Error; err != nil || stored.Fingerprint != initial.Fingerprint {
						t.Fatalf("Item identity was not restored: %v, err = %v", stored, err)
					}
					if err := scanService.deliverObjectIndexChanges(t.Context()); err != nil {
						t.Fatal(err)
					}
					// Remove the same object again, lose the Manager reply, then
					// restore it while the deletion is still unconfirmed in Meta.
					provider.keys = []string{keptKey, outsideKey}
					if err := scan(opts); err != nil {
						t.Fatal(err)
					}
					indexLock.Lock()
					dropDeleteResponse = true
					indexLock.Unlock()
					if err := scanService.deliverObjectIndexChanges(t.Context()); err == nil {
						t.Fatal("expected lost Manager response")
					}
					var pending models.ObjectIndexDeletion
					if err := db.Where("fingerprint = ?", initial.Fingerprint).Take(&pending).Error; err != nil || pending.Confirmed {
						t.Fatalf("lost reply settled Meta deletion: %+v, %v", pending, err)
					}
					provider.keys = append(provider.keys, removedKey)
					if err := scan(opts); err != nil {
						t.Fatal(err)
					}
					root, err := tree.GetTree(t.Context(), tenantID, engineID, -1)
					if err != nil || findResourceTreeNodeForTest(root, "bucket/"+removedKey) == nil {
						t.Fatalf("restored object absent from tree: %v", err)
					}
					current, err := facts.GetDataItemAuthorizationFacts(t.Context(), tenantID, initial.Fingerprint)
					if err != nil || current == nil || current.AfterDeleteOperationID != pending.OperationID {
						t.Fatalf("current search observation lost the restoration identity: %+v, %v", current, err)
					}
					indexLock.Lock()
					if indexed[initial.Fingerprint] {
						t.Error("indexed restoration overtook the unconfirmed deletion")
					}
					indexLock.Unlock()
					if err := scanService.deliverObjectIndexChanges(t.Context()); err != nil {
						t.Fatal(err)
					}
					indexLock.Lock()
					if !indexed[initial.Fingerprint] || deletionCalls[pending.OperationID] != 2 {
						t.Errorf("restoration did not follow the original deletion receipt: index=%v calls=%v", indexed, deletionCalls)
					}
					indexLock.Unlock()
				}
			})
		}
	}
}
