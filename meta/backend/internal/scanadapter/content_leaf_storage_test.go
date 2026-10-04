package scanadapter_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/format/plugins/csv"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanruntime"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const exactLeafContent = "id,value\n1,A\n"

type contentStorageFactPlugin struct {
	preciseCatalogPlugin
	t      *testing.T
	strict bool
	target string
	opened []string
}

func (p *contentStorageFactPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}

func (p *contentStorageFactPlugin) ListChildren(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if p.strict {
		p.t.Fatal("precise content scan enumerated siblings")
	}
	return p.entries, nil
}

func (p *contentStorageFactPlugin) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if p.fail {
		return nil, errors.New("source target unavailable")
	}
	for _, entry := range p.entries {
		if reflect.DeepEqual(entry.Path, path) {
			return &entry, nil
		}
	}
	last := path.Segments[len(path.Segments)-1]
	if last.Term == plugin.EngineCatalogTermObject || last.Term == plugin.EngineCatalogTermFile {
		return nil, nil
	}
	return &plugin.EngineCatalogEntry{Name: last.Name, Path: path, Term: last.Term, Kind: last.Kind, Role: plugin.EngineCatalogRoleBranch}, nil
}

func (p *contentStorageFactPlugin) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	if p.strict && path.StringPath() != p.target {
		p.t.Fatalf("precise content scan opened sibling %s", path.StringPath())
	}
	p.opened = append(p.opened, path.StringPath())
	return io.NopCloser(strings.NewReader(exactLeafContent)), nil
}

func TestPreciseContentStorageFacts(t *testing.T) {
	testPreciseContentStorageFacts(t, func(t *testing.T) *gorm.DB { return metatest.OpenMetadataDB(t) })
}

func TestPreciseContentStorageFactsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	testPreciseContentStorageFacts(t, func(t *testing.T) *gorm.DB {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		t.Cleanup(func() { tx.Rollback() })
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS meta").Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
			t.Fatal(err)
		}
		return tx
	})
}

func testPreciseContentStorageFacts(t *testing.T, openDB func(*testing.T) *gorm.DB) {
	for _, kind := range []string{"object", "file"} {
		t.Run(kind, func(t *testing.T) {
			db := openDB(t)
			repo := metaRepo.NewScanRepository(db)
			model, parts, scope := plugin.FileCatalogModel(), []string{"results", "A.csv"}, "results"
			if kind == "object" {
				model, parts, scope = plugin.ObjectCatalogModel(), []string{"bucket", "results", "A.csv"}, "bucket/results"
			}
			p := &contentStorageFactPlugin{t: t, preciseCatalogPlugin: preciseCatalogPlugin{preciseCatalogBasePlugin: preciseCatalogBasePlugin{engineType: "content-facts-" + kind, model: model}}}
			plugin.Register(p)
			t.Cleanup(func() { plugin.Unregister(p.Type()) })
			resource := &commonModels.Engine{ID: 961, EngineType: p.Type(), Name: "exact content source"}
			locator := &resourcetree.ResourceLocator{EngineID: resource.ID, Type: resourcetree.ResourceType(kind), Path: parts}
			targetPath, err := resourcetree.EngineCatalogPathFromLocator(model, locator)
			if err != nil {
				t.Fatal(err)
			}
			size := int64(len(exactLeafContent))
			modified := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
			for _, name := range []string{"A.csv", "B.csv"} {
				itemPath := targetPath
				itemPath.Segments = append([]plugin.EngineCatalogSegment(nil), targetPath.Segments...)
				itemPath.Segments[len(itemPath.Segments)-1].Name = name
				last := itemPath.Segments[len(itemPath.Segments)-1]
				p.entries = append(p.entries, plugin.EngineCatalogEntry{Name: name, Path: itemPath, Term: last.Term, Kind: last.Kind, Role: plugin.EngineCatalogRoleLeaf, UpdatedAt: &modified, Storage: &plugin.EngineCatalogStorageFacts{Path: itemPath.StringPath(), SizeBytes: &size, ContentType: "text/csv", ETag: "source-etag"}})
			}
			p.target = targetPath.StringPath()
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			contentScanner := scanruntime.NewRuntimeEngineCatalogContentScanner(scanruntime.NewObjectStorageCatalogRuntime(db, log, repo, nil), scanruntime.NewFilesystemCatalogRuntime(db, log, repo, nil))
			dispatcher := scanadapter.NewEngineCatalogScanDispatcher(db, repo, log, nil, nil, nil, contentScanner)
			req := scanflow.DispatchRequest{Context: t.Context(), Resource: resource, EnginePlugin: p, TenantID: 961, CatalogPaths: []string{scope}, ScanDepth: "basic", Force: true}
			// A first exact scan must create only its own item and structural parents.
			p.strict = true
			first := req
			first.CatalogPaths = nil
			first.Targets = []string{locator.ToURI()}
			result, err := dispatcher.Dispatch(first)
			if err != nil || result.Items != 1 {
				t.Fatalf("first precise content scan: %+v %v", result, err)
			}
			var firstItem models.MetaItem
			if err := db.Where("tenant_id = ? AND engine_id = ?", 961, resource.ID).First(&firstItem).Error; err != nil {
				t.Fatal(err)
			}
			if firstItem.Name != "A.csv" || firstItem.SizeBytes == nil || *firstItem.SizeBytes != size {
				t.Fatalf("first precise scan lost source size: %+v", firstItem)
			}
			var count int64
			if err := db.Model(&models.MetaItem{}).Where("tenant_id = ? AND engine_id = ?", 961, resource.ID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("first precise scan persisted siblings: %d %v", count, err)
			}
			var parents []models.MetaNode
			if err := db.Where("tenant_id = ? AND engine_id = ?", 961, resource.ID).Find(&parents).Error; err != nil {
				t.Fatal(err)
			}
			for _, parent := range parents {
				if parent.ScannedAt != nil || parent.ScannedDepth != models.ScannedDepthNone || parent.ScanStatus != "pending" {
					t.Fatalf("first precise scan completed parent range: %+v", parent)
				}
			}
			p.strict = false
			result, err = dispatcher.Dispatch(req)
			if err != nil || result.Items != 2 {
				t.Fatalf("full content baseline: %+v %v", result, err)
			}
			readItem := func(name string) models.MetaItem {
				var item models.MetaItem
				if err := db.Where("tenant_id = ? AND engine_id = ? AND name = ?", 961, resource.ID, name).First(&item).Error; err != nil {
					t.Fatal(err)
				}
				return item
			}
			baseline, siblingBefore := readItem("A.csv"), readItem("B.csv")
			var before []models.MetaNode
			if err := db.Where("tenant_id = ? AND engine_id = ?", 961, resource.ID).Order("id").Find(&before).Error; err != nil {
				t.Fatal(err)
			}
			p.strict = true
			req.CatalogPaths = nil
			req.Targets = []string{locator.ToURI()}
			for _, depth := range []string{"basic", "deep", "deep"} {
				req.ScanDepth = depth
				p.opened = nil
				result, err := dispatcher.Dispatch(req)
				if err != nil || result.Items != 1 {
					t.Fatalf("precise %s scan: %+v %v", depth, result, err)
				}
				item := readItem("A.csv")
				if item.SizeBytes == nil || *item.SizeBytes != size || commonJSON.Int64(item.Attributes, "storage", "total_size") != size {
					t.Fatalf("precise %s storage facts lost: size=%v storage=%v", depth, item.SizeBytes, item.Attributes["storage"])
				}
				if item.DataUpdatedAt == nil || !item.DataUpdatedAt.Equal(modified) {
					t.Fatalf("source modification time lost: %v", item.DataUpdatedAt)
				}
				if commonJSON.String(item.Attributes, "storage", "content_type") != "text/csv" {
					t.Fatalf("source MIME lost: %v", item.Attributes["storage"])
				}
				if kind == "object" && commonJSON.String(item.Attributes, "storage", "etag") != "source-etag" {
					t.Fatalf("source ETag lost: %v", item.Attributes["storage"])
				}
				if item.Fingerprint != baseline.Fingerprint || item.NodeID != baseline.NodeID || item.FullName != baseline.FullName {
					t.Fatal("precise/full storage identity differs")
				}
				storage := item.Attributes["storage"].(map[string]interface{})
				for key, value := range baseline.Attributes["storage"].(map[string]interface{}) {
					if !reflect.DeepEqual(storage[key], value) {
						t.Fatalf("precise %s/full storage fact %s differs: current=%v baseline=%v", depth, key, storage[key], value)
					}
				}
				if depth == "deep" && commonJSON.String(item.Attributes, "storage", "content_hash") == "" {
					t.Fatal("deep content scan lost content hash")
				}
				if item.ScannedDepth != depth {
					t.Fatalf("scan depth=%s want %s", item.ScannedDepth, depth)
				}
				if depth == "basic" && len(p.opened) != 0 {
					t.Fatalf("basic content scan opened content: %v", p.opened)
				}
				if depth == "deep" && len(p.opened) == 0 {
					t.Fatal("deep content scan skipped enhancement")
				}
			}
			p.fail = true
			if _, err := dispatcher.Dispatch(req); err == nil {
				t.Fatal("unavailable exact source reported success")
			}
			var after []models.MetaNode
			if err := db.Where("tenant_id = ? AND engine_id = ?", 961, resource.ID).Order("id").Find(&after).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("precise content scan changed parent range facts")
			}
			if !reflect.DeepEqual(siblingBefore, readItem("B.csv")) {
				t.Fatal("precise content scan changed sibling metadata")
			}
		})
	}
}
