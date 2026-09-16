package scanruntime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanresource"
)

func TestObjectScanStateTracksFailedRanges(t *testing.T) {
	for _, scope := range []string{"addp", "addp/bad", "addp/empty", "addp/unreadable"} {
		t.Run(scope, func(t *testing.T) {
			provider := scanStateProvider{}
			pluginRegisterForTest(t, provider)
			db := openObjectCatalogScanTestDB(t)
			repo := metaRepo.NewScanRepository(db)
			resource := &commonModels.Engine{ID: 9, Name: "store", EngineType: provider.Type()}
			root, err := metaRepo.EnsureEngineCatalogRootNode(repo, 1, resource, provider)
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := repo.UpsertNode(1, 9, root, "bucket", "addp", strPtr("addp"), scanresource.ObjectBucketNodeAttributes("addp"))
			if err != nil {
				t.Fatal(err)
			}
			nodes := map[string]*models.MetaNode{"addp": bucket}
			at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			for _, prefix := range []string{"bad", "good", "empty", "unreadable"} {
				node, err := repo.EnsureObjectCatalogPrefixPath(1, 9, bucket, prefix)
				if err != nil {
					t.Fatal(err)
				}
				nodes[node.FullName] = node
			}
			for _, node := range nodes {
				if err := db.Model(node).Updates(map[string]interface{}{"scan_status": "completed", "scanned_depth": "basic", "scanned_at": at}).Error; err != nil {
					t.Fatal(err)
				}
			}
			runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
			_, err = runtime.ScanPaths(context.Background(), resource, 1, []string{scope}, nil, models.ScannedDepthDeep, true, nil)
			if (err == nil) != (scope == "addp/empty") {
				t.Fatalf("scope %s: err=%v", scope, err)
			}
			for name, before := range nodes {
				var got models.MetaNode
				if err := db.First(&got, before.ID).Error; err != nil {
					t.Fatal(err)
				}
				failed := name == scope && scope != "addp/empty" || scope == "addp" && name == "addp/bad"
				succeeded := scope == "addp" && name == "addp/good" || scope == "addp/empty" && name == scope
				if failed {
					if got.ScanStatus != "failed" || got.ScannedDepth != "basic" || got.ScannedAt == nil || !got.ScannedAt.Equal(at) {
						t.Fatalf("failed range %s: %#v", name, got)
					}
				} else if succeeded {
					if got.ScanStatus != "completed" || got.ScannedDepth != "deep" || got.ScannedAt == nil || !got.ScannedAt.After(at) {
						t.Fatalf("successful range %s: %#v", name, got)
					}
				} else if got.ScanStatus != "completed" || got.ScannedDepth != "basic" || got.ScannedAt == nil || !got.ScannedAt.Equal(at) {
					t.Fatalf("uncovered range %s was changed: %#v", name, got)
				}
			}
		})
	}
}

func TestObjectScanStateDoesNotUseBoundedFailureSamples(t *testing.T) {
	provider := scanStateProvider{paths: []string{"good/readme.md"}}
	for i := 0; i < 25; i++ {
		provider.paths = append(provider.paths, fmt.Sprintf("bad%02d/broken.xlsx", i))
	}
	provider.paths = append(provider.paths, strings.Repeat("long", 150)+"/broken.xlsx")
	pluginRegisterForTest(t, provider)
	db := openObjectCatalogScanTestDB(t)
	repo := metaRepo.NewScanRepository(db)
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	_, err := runtime.ScanPaths(context.Background(), &commonModels.Engine{ID: 9, EngineType: provider.Type()}, 1, []string{"addp"}, nil, "deep", true, nil)
	if err == nil {
		t.Fatal("expected broken workbook failures")
	}
	var nodes []models.MetaNode
	if err := db.Where("node_type IN ?", []string{"bucket", "prefix"}).Find(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 28 {
		t.Fatalf("covered nodes=%d", len(nodes))
	}
	for _, node := range nodes {
		if node.FullName == "addp/good" {
			continue
		}
		if node.ScanStatus != "failed" || node.ScannedDepth != "none" || node.ScannedAt != nil {
			t.Fatalf("failed target omitted from state: %#v", node)
		}
	}
}

type scanStateProvider struct {
	objectCatalogScanTestProvider
	paths []string
}

func (scanStateProvider) Type() string { return "object-scan-state-test" }
func (scanStateProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, p plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return nil, nil
}
func (s scanStateProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, p plugin.EngineCatalogPath, _ plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if p.StringPath() == "addp/unreadable" {
		return nil, fmt.Errorf("catalog unavailable")
	}
	var entries []plugin.EngineCatalogEntry
	paths := s.paths
	if paths == nil {
		paths = []string{"bad/broken.xlsx", "good/readme.md"}
	}
	for _, name := range paths {
		if p.StringPath() != "addp" && !strings.HasPrefix("addp/"+name, p.StringPath()+"/") {
			continue
		}
		size := int64(8)
		entries = append(entries, plugin.EngineCatalogEntry{Name: name[strings.LastIndex(name, "/")+1:], Path: plugin.ObjectItemPath(9, "addp", name), Role: plugin.EngineCatalogRoleLeaf, Kind: plugin.EngineCatalogKindObject, Storage: &plugin.EngineCatalogStorageFacts{Path: "addp/" + name, SizeBytes: &size}})
	}
	return entries, nil
}
func (scanStateProvider) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("invalid workbook")), nil
}
