package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/minio"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	metaerrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metarepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanruntime"
	parquetgo "github.com/parquet-go/parquet-go"
	"gorm.io/gorm"
)

// Synthetic transport only: all identity, detection, persistence, refresh and
// authorization facts come from the production path, for both catalog models.
type wholeFactsProvider struct {
	minio.MinIOPlugin
	kind   string
	id     uint
	object bool
	files  map[string][]byte
	reads  int
}

func (p *wholeFactsProvider) Type() string { return p.kind }
func (p *wholeFactsProvider) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	if p.object {
		return plugin.ObjectCatalogModel()
	}
	return plugin.FileCatalogModel()
}
func (p *wholeFactsProvider) target(name string, directory bool) plugin.EngineCatalogPath {
	if !p.object {
		if directory {
			return plugin.FileDirectoryPath(p.id, name)
		}
		return plugin.FileItemPath(p.id, name)
	}
	bucket, key, _ := strings.Cut(name, "/")
	if directory {
		return plugin.ObjectDirectoryPath(p.id, bucket, key)
	}
	return plugin.ObjectItemPath(p.id, bucket, key)
}
func (p *wholeFactsProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.EngineCatalogPath, _ plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	prefix := parent.StringPath()
	if prefix != "" {
		prefix += "/"
	}
	names := map[string]bool{}
	for name := range p.files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		first, _, dir := strings.Cut(rel, "/")
		names[prefix+first] = dir
	}
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	entries := make([]plugin.EngineCatalogEntry, 0, len(keys))
	for _, name := range keys {
		dir := names[name]
		term, role := "file", plugin.EngineCatalogRoleLeaf
		if p.object {
			term = "object"
		}
		if dir {
			term = "directory"
			role = plugin.EngineCatalogRoleBranch
			if p.object {
				term = "prefix"
				if prefix == "" {
					term = "bucket"
				}
			}
		}
		size := int64(len(p.files[name]))
		entries = append(entries, plugin.EngineCatalogEntry{Name: path.Base(name), Path: p.target(name, dir), Term: term, Kind: term, Role: role, Storage: &plugin.EngineCatalogStorageFacts{Path: name, SizeBytes: &size}})
	}
	return entries, nil
}
func (p *wholeFactsProvider) ResolvePath(ctx context.Context, conn plugin.ConnectionInfo, target plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	parent := path.Dir(target.StringPath())
	if parent == "." {
		parent = ""
	}
	entries, _ := p.ListChildren(ctx, conn, p.target(parent, true), plugin.ListOptions{})
	for _, entry := range entries {
		if entry.Path.StringPath() == target.StringPath() {
			return &entry, nil
		}
	}
	return nil, fmt.Errorf("missing synthetic path %s", target.StringPath())
}
func (p *wholeFactsProvider) OpenContent(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	p.reads++
	content, ok := p.files[target.StringPath()]
	if !ok {
		return nil, fmt.Errorf("unexpected content %s", target.StringPath())
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}
func (p *wholeFactsProvider) OpenRange(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	p.reads++
	content, ok := p.files[target.StringPath()]
	if !ok || opts.Offset < 0 || opts.Offset > int64(len(content)) {
		return nil, fmt.Errorf("invalid content range")
	}
	end := int64(len(content))
	if opts.Length > 0 {
		end = min(end, opts.Offset+opts.Length)
	}
	return io.NopCloser(bytes.NewReader(content[opts.Offset:end])), nil
}

func TestScannedWholeAuthorizationFacts(t *testing.T) {
	exerciseScannedWholeAuthorizationFacts(t, metatest.OpenMetadataDB(t))
}

func exerciseScannedWholeAuthorizationFacts(t *testing.T, db *gorm.DB) {
	for index, kind := range []string{"minio", "s3", "nfs", "ftp", "hdfs"} {
		t.Run("whole_scanner/"+kind, func(t *testing.T) {
			p := &wholeFactsProvider{kind: kind, id: uint(8100 + index), object: kind == "minio" || kind == "s3", files: map[string][]byte{}}
			root := "results/统计 %"
			if p.object {
				root = "业务/" + root
			}
			var buffer bytes.Buffer
			writer := parquetgo.NewGenericWriter[struct{ Value int64 }](&buffer)
			if _, err := writer.Write([]struct{ Value int64 }{{Value: 7}}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"part-000.parquet", "part-001.parquet"} {
				p.files[root+"/"+name] = buffer.Bytes()
			}
			p.files[root+"/_SUCCESS"] = []byte("success")
			previous, oldErr := plugin.Get(kind)
			plugin.Register(p)
			t.Cleanup(func() {
				plugin.Unregister(kind)
				if oldErr == nil {
					plugin.Register(previous)
				}
			})
			repo := metarepo.NewScanRepository(db)
			engine := &commonmodels.Engine{ID: p.id, Name: kind, EngineType: kind}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			query := NewMetadataQueryService(db)
			var originalID, scopeID uint
			var originalHash string
			for _, depth := range []string{"basic", "deep", "basic"} {
				before := p.reads
				if p.object {
					_, err := scanruntime.NewObjectStorageCatalogRuntime(db, logger, repo, nil).ScanPaths(t.Context(), engine, 7, []string{root}, nil, depth, false, nil)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					_, err := scanruntime.NewFilesystemCatalogRuntime(db, logger, repo, nil).ScanPaths(t.Context(), engine, 7, []string{root}, depth, false, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				if depth == "basic" && p.reads != before {
					t.Fatal("basic whole scan read content")
				}
				var items []models.MetaItem
				if err := db.Where("tenant_id=? AND engine_id=?", 7, p.id).Find(&items).Error; err != nil {
					t.Fatal(err)
				}
				if len(items) != 1 {
					t.Fatalf("whole scope produced %d independent items", len(items))
				}
				item := items[0]
				var scope models.MetaNode
				if err := db.First(&scope, item.NodeID).Error; err != nil {
					t.Fatal(err)
				}
				if item.Name != "统计 %" || scope.Name != item.Name || scope.FullName != item.FullName || item.FullName != root {
					t.Fatalf("wrong real scope identity: %+v / %+v", item, scope)
				}
				facts, err := query.GetDataItemAuthorizationFacts(t.Context(), 7, item.Fingerprint)
				if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.Boundary.MetadataTarget == nil {
					t.Fatalf("scanner facts incomplete: %+v %v", facts, err)
				}
				if _, err := facts.ProveMetadataTarget(p.target(root, true)); err != nil {
					t.Fatal(err)
				}
				if _, err := facts.ApprovedBinding(); err == nil {
					t.Fatal("whole metadata identity became content permission")
				}
				if originalID == 0 {
					originalID, scopeID, originalHash = item.ID, scope.ID, *facts.BoundaryHash
				} else if item.ID != originalID || scope.ID != scopeID || *facts.BoundaryHash != originalHash {
					t.Fatal("scan depth changed whole identity/boundary")
				}
				if depth == "deep" {
					if item.RowCount == nil || *item.RowCount != 2 {
						t.Fatal("whole parquet row count missing")
					}
					p.files[root+"/part-002.parquet"] = buffer.Bytes()
				}
			}
			if _, err := scanruntime.NewItemRefreshRuntime(repo, nil, logger).RefreshKnownItemByIDWithPlugin(t.Context(), p, engine, 7, originalID, "", nil, "deep"); err != nil {
				t.Fatal(err)
			}
			var refreshed models.MetaItem
			if err := db.First(&refreshed, originalID).Error; err != nil {
				t.Fatal(err)
			}
			facts, err := query.GetDataItemAuthorizationFacts(t.Context(), 7, refreshed.Fingerprint)
			if err != nil || facts.BoundaryHash == nil || *facts.BoundaryHash != originalHash || refreshed.NodeID != scopeID || refreshed.RowCount == nil || *refreshed.RowCount != 3 {
				t.Fatalf("member growth/known refresh changed boundary: %+v %v", facts, err)
			}
			engineService := NewEngineService(db, nil)
			tenant := uint(7)
			engine.TenantID, engine.LifecycleState = &tenant, "active"
			engineService.cacheEngine(7, engine)
			treeService := NewResourceTreeService(engineService, query)
			tree, err := treeService.GetTree(t.Context(), 7, p.id, -1)
			if err != nil {
				t.Fatal(err)
			}
			assertResourceTreeItemBoundary(t, tree)
			var owner models.MetaNode
			if err := db.First(&owner, scopeID).Error; err != nil {
				t.Fatal(err)
			}
			parentLocator := ""
			var leaves []*resourcetree.TreeNode
			var visit func(*resourcetree.TreeNode)
			visit = func(n *resourcetree.TreeNode) {
				loc, err := resourcetree.ParseURI(n.Locator)
				if err != nil {
					t.Fatal(err)
				}
				if loc.ItemID != nil {
					leaves = append(leaves, n)
				}
				if loc.NodeID != nil && *loc.NodeID == scopeID {
					t.Fatal("internal scope exposed as ordinary node")
				}
				if loc.NodeID != nil && owner.ParentNodeID != nil && *loc.NodeID == *owner.ParentNodeID {
					parentLocator = n.Locator
				}
				for _, child := range n.Children {
					visit(child)
				}
			}
			visit(tree)
			if len(leaves) != 1 || leaves[0].Label != "统计 %" {
				t.Fatalf("wrong whole tree presentation: %+v", leaves)
			}
			lazy, err := treeService.GetNode(t.Context(), 7, p.id, parentLocator)
			if err != nil || len(lazy.Children) != 1 || lazy.Children[0].Locator != leaves[0].Locator {
				t.Fatalf("lazy tree differs from full tree: %+v %v", lazy, err)
			}
			scopeType := resourcetree.ResourceType("dir")
			if p.object {
				scopeType = resourcetree.ResourceType("prefix")
			}
			hidden := (&resourcetree.ResourceLocator{EngineID: p.id, Path: strings.Split(root, "/"), Type: scopeType, NodeID: &scopeID}).ToURI()
			if _, err := treeService.GetNode(t.Context(), 7, p.id, hidden); !errors.Is(err, metaerrors.ErrNodeNotFound) {
				t.Fatal("scope became an ordinary node", err)
			}
			if _, err := treeService.GetAncestors(t.Context(), 7, p.id, hidden); !errors.Is(err, metaerrors.ErrNodeNotFound) {
				t.Fatal("scope leaked through node ancestors", err)
			}
			ancestors, err := treeService.GetAncestors(t.Context(), 7, p.id, leaves[0].Locator)
			if err != nil {
				t.Fatal(err)
			}
			for _, ancestor := range ancestors.Ancestors {
				loc, err := resourcetree.ParseURI(ancestor.Locator)
				if err != nil || (loc.NodeID != nil && *loc.NodeID == scopeID) {
					t.Fatal("scope was not folded in ancestors", err)
				}
			}
			search, err := treeService.Search(t.Context(), 7, p.id, "统计", nil, 10)
			if err != nil || search.Total != 1 {
				t.Fatalf("whole search duplicated scope: %+v %v", search, err)
			}
			if err := db.Model(&models.MetaNode{}).Where("id=?", scopeID).Update("tenant_id", 8).Error; err != nil {
				t.Fatal(err)
			}
			facts, err = query.GetDataItemAuthorizationFacts(t.Context(), 7, refreshed.Fingerprint)
			if err != nil || facts.BasisState == authorizationfacts.AuthorizationBasisComplete || facts.Boundary != nil {
				t.Fatal("cross-tenant scope proved identity")
			}
		})
	}
}
