package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func TestMetadataVisibilityIsolatesUnresolvedItems(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	tenant, engine := uint(7), uint(9)
	svc := newResourceTreeTestServiceWithDB(t, db, tenant, engine)
	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: tenant, EngineID: engine, NodeType: "service", Name: "Objects"})
	bucket := createResourceTreeNode(t, db, models.MetaNode{TenantID: tenant, EngineID: engine, ParentNodeID: &root.ID, NodeType: "bucket", Name: "outputs", FullName: "outputs"})
	prefix := createResourceTreeNode(t, db, models.MetaNode{TenantID: tenant, EngineID: engine, ParentNodeID: &bucket.ID, NodeType: "prefix", Name: "mosaic", FullName: "outputs/mosaic"})
	valid := createResourceTreeItem(t, db, models.MetaItem{TenantID: tenant, EngineID: engine, NodeID: bucket.ID, ItemType: "object", Name: "visible.csv", FullName: "outputs/visible.csv", Fingerprint: "valid"})
	// A persisted whole item whose name was appended twice cannot prove identity.
	invalid := createResourceTreeItem(t, db, models.MetaItem{TenantID: tenant, EngineID: engine, NodeID: prefix.ID, ItemType: "object", Name: "mosaic", FullName: "outputs/mosaic/mosaic", Fingerprint: "invalid", Attributes: models.JSONMap{"item": map[string]any{"layout": "whole"}}})
	candidates := []models.MetaItemLite{}
	for _, item := range []models.MetaItem{valid, invalid} {
		candidates = append(candidates, models.MetaItemLite{ID: item.ID, TenantID: tenant, EngineID: engine})
	}
	candidates = append(candidates, models.MetaItemLite{ID: 99999, TenantID: tenant, EngineID: engine})
	failure := error(nil)
	ctx := WithMetadataVisibility(t.Context(), func(_ context.Context, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
		if failure != nil {
			return nil, failure
		}
		if len(targets) != 1 || targets[0].Segments[len(targets[0].Segments)-1].Name != valid.Name {
			t.Fatalf("unresolved identity reached authorization: %+v", targets)
		}
		return &authorization.SourceMetadataCheck{ObservedAt: time.Now(), Targets: []authorization.SourceMetadataTargetCheck{{Target: targets[0], Visible: true}}}, nil
	})
	items, err := svc.FilterMetadataItems(ctx, tenant, candidates)
	if err != nil || len(items) != 1 || items[0].ID != valid.ID {
		t.Fatalf("unresolved item blocked valid sibling: %+v %v", items, err)
	}
	tree, err := svc.GetTree(ctx, tenant, engine, -1)
	raw, _ := json.Marshal(tree)
	if err != nil || tree.Metadata["item_count"] != 1 || strings.Contains(string(raw), "mosaic") {
		t.Fatalf("unresolved tree item leaked or blocked sibling: %s %v", raw, err)
	}
	failure = errors.New("authority unavailable")
	if items, err = svc.FilterMetadataItems(ctx, tenant, candidates); !errors.Is(err, failure) || items != nil {
		t.Fatalf("authority failure accepted partial items: %+v %v", items, err)
	}
	if tree, err = svc.GetTree(ctx, tenant, engine, -1); !errors.Is(err, failure) || tree != nil {
		t.Fatalf("authority failure accepted partial tree: %+v %v", tree, err)
	}
	failure = nil
	if err := db.Exec("DROP TABLE meta.meta_node").Error; err != nil {
		t.Fatal(err)
	}
	if items, err = svc.FilterMetadataItems(ctx, tenant, candidates); err == nil || errors.Is(err, gorm.ErrRecordNotFound) || items != nil {
		t.Fatalf("database failure treated as absent identity: %+v %v", items, err)
	}
}

func TestPGUserResourceTreeFiltersBeforeDepthSearchAndAncestors(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	tenant, engine := uint(7), uint(9)
	svc := newResourceTreeTestServiceWithDB(t, db, tenant, engine)
	svc.engineService.cacheEngine(tenant, &commonModels.Engine{ID: engine, TenantID: &tenant, Name: "PG", EngineType: "postgresql", LifecycleState: "active"})
	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: tenant, EngineID: engine, NodeType: "server", Name: "PG", FullName: "", Depth: 1})
	visibleSchema := createResourceTreeNode(t, db, models.MetaNode{TenantID: tenant, EngineID: engine, ParentNodeID: &root.ID, NodeType: "schema", Name: "public", FullName: "public", Depth: 2})
	hiddenSchema := createResourceTreeNode(t, db, models.MetaNode{TenantID: tenant, EngineID: engine, ParentNodeID: &root.ID, NodeType: "schema", Name: "secret_schema", FullName: "secret_schema", Depth: 2})
	allowed := createResourceTreeItem(t, db, models.MetaItem{TenantID: tenant, EngineID: engine, NodeID: visibleSchema.ID, ItemType: "table", Name: "visible_table", FullName: "public.visible_table", Fingerprint: "visible-fp"})
	createResourceTreeItem(t, db, models.MetaItem{TenantID: tenant, EngineID: engine, NodeID: visibleSchema.ID, ItemType: "table", Name: "hidden_table", FullName: "public.hidden_table", Fingerprint: "hidden-fp"})
	hidden := createResourceTreeItem(t, db, models.MetaItem{TenantID: tenant, EngineID: engine, NodeID: hiddenSchema.ID, ItemType: "table", Name: "hidden_table", FullName: "secret_schema.hidden_table", Fingerprint: "hidden2-fp"})
	allow := true
	calls := 0
	ctx := WithMetadataVisibility(t.Context(), func(_ context.Context, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
		calls++
		if len(targets) != 3 {
			t.Fatalf("did not check complete candidate set: %d", len(targets))
		}
		result := &authorization.SourceMetadataCheck{ObservedAt: time.Now()}
		for _, target := range targets {
			result.Targets = append(result.Targets, authorization.SourceMetadataTargetCheck{Target: target, Visible: allow && target.Segments[len(target.Segments)-1].Name == "visible_table"})
		}
		return result, nil
	})
	tree, err := svc.GetTree(ctx, tenant, engine, 1)
	if err != nil || len(tree.Children) != 1 || tree.Children[0].Label != "public" || !tree.Children[0].HasChildren || len(tree.Children[0].Children) != 0 {
		t.Fatalf("depth after filtering: %+v %v", tree, err)
	}
	if tree.Metadata["item_count"] != 1 {
		t.Fatalf("unfiltered count: %+v", tree.Metadata)
	}
	results, err := svc.Search(ctx, tenant, engine, "table", nil, 1)
	if err != nil || results.Total != 1 || len(results.Results) != 1 || results.Results[0].Label != "visible_table" {
		t.Fatalf("search leaked or paginated first: %+v %v", results, err)
	}
	assertResourceTreeItemBoundary(t, results.Results[0])
	node, err := svc.GetNode(ctx, tenant, engine, (&resourcetree.ResourceLocator{EngineID: engine, Path: []string{"public"}, Type: resourcetree.TypeSchema, NodeID: &visibleSchema.ID}).ToURI())
	if err != nil || len(node.Children) != 1 {
		t.Fatalf("node children: %+v %v", node, err)
	}
	assertResourceTreeItemBoundary(t, node)
	for _, item := range []models.MetaItem{allowed, hidden} {
		schema := "public"
		if item.ID == hidden.ID {
			schema = "secret_schema"
		}
		locator := (&resourcetree.ResourceLocator{EngineID: engine, Path: []string{schema, item.Name}, Type: resourcetree.TypeTable, ItemID: &item.ID}).ToURI()
		ancestors, err := svc.GetAncestors(ctx, tenant, engine, locator)
		if item.ID == allowed.ID {
			if err != nil || len(ancestors.Ancestors) != 3 {
				t.Fatalf("visible ancestors: %+v %v", ancestors, err)
			}
			for _, ancestor := range ancestors.Ancestors {
				assertResourceTreeItemBoundary(t, ancestor)
			}
		} else if ancestors != nil || !errors.Is(err, metaErrors.ErrNodeNotFound) {
			t.Fatalf("hidden ancestors escaped: %+v %v", ancestors, err)
		}
	}
	allow = false
	tree, err = svc.GetTree(ctx, tenant, engine, -1)
	if err != nil || len(tree.Children) != 0 || tree.HasChildren || tree.Metadata["item_count"] != 0 || calls != 6 {
		t.Fatalf("revocation was cached: %+v calls=%d err=%v", tree, calls, err)
	}
}

func TestPGTreeWhitelistAndIncompleteDecisionFailClosed(t *testing.T) {
	itemID := uint(1)
	leaf := &resourcetree.TreeNode{ID: "item", Label: "table", Type: "table", Locator: (&resourcetree.ResourceLocator{EngineID: 9, Path: []string{"public", "table"}, Type: resourcetree.TypeTable, ItemID: &itemID}).ToURI(), Metadata: map[string]interface{}{"row_count": 999, "sample": "private", "comment": "private", "statistics": "private", "physical_path": "private", "item_count": 99, "item_id": 1, "full_name": "public.table"}}
	root := &resourcetree.TreeNode{ID: "root", Type: "server", Locator: resourcetree.EngineRootLocatorForType(9, resourcetree.TypeServer), Children: []*resourcetree.TreeNode{leaf}, Metadata: map[string]interface{}{"item_count": 88, "secret": "private"}}
	for _, mode := range []string{"allow", "error", "wrong-target", "missing"} {
		t.Run(mode, func(t *testing.T) {
			result, err := filterMetadataResourceTree(t.Context(), root, 9, visibilityTestTargetResolver(root, "postgresql"), func(_ context.Context, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
				if mode == "error" {
					return nil, commonapi.ErrForbidden
				}
				response := &authorization.SourceMetadataCheck{ObservedAt: time.Now(), Targets: []authorization.SourceMetadataTargetCheck{{Target: targets[0], Visible: true}}}
				if mode == "wrong-target" {
					response.Targets[0].Target.EngineID = 10
				}
				if mode == "missing" {
					response.Targets = nil
				}
				return response, nil
			})
			if mode != "allow" {
				if err == nil || result != nil {
					t.Fatal("partial/unavailable decision leaked tree")
				}
				return
			}
			encoded, _ := json.Marshal(result)
			if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "row_count") || result.Metadata["item_count"] != 1 {
				t.Fatalf("unsafe projection: %s %v", encoded, err)
			}
			if _, exists := result.Children[0].Metadata["item_count"]; exists {
				t.Fatalf("visible leaf has a node count: %s", encoded)
			}
			if root.Metadata["item_count"] != 88 || leaf.Metadata["row_count"] != 999 {
				t.Fatal("shared facts mutated")
			}
		})
	}
}

func TestPGTreeVisibilityBatchesAndDiscardsPartialObservations(t *testing.T) {
	root := &resourcetree.TreeNode{ID: "root", Type: "server", Locator: resourcetree.EngineRootLocatorForType(9, resourcetree.TypeServer)}
	for id := uint(1); id <= 201; id++ {
		itemID := id
		root.Children = append(root.Children, &resourcetree.TreeNode{ID: fmt.Sprint(id), Type: "table", Locator: (&resourcetree.ResourceLocator{EngineID: 9, Path: []string{"public", fmt.Sprintf("table_%03d", id)}, Type: resourcetree.TypeTable, ItemID: &itemID}).ToURI()})
	}
	for _, failSecond := range []bool{false, true} {
		t.Run(fmt.Sprintf("second_batch_failure_%v", failSecond), func(t *testing.T) {
			calls := 0
			result, err := filterMetadataResourceTree(t.Context(), root, 9, visibilityTestTargetResolver(root, "postgresql"), func(_ context.Context, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
				calls++
				want := 200
				if calls == 2 {
					want = 1
				}
				if len(targets) != want || calls > 2 {
					t.Fatalf("unexpected batch %d size %d", calls, len(targets))
				}
				if failSecond && calls == 2 {
					return nil, commonapi.ErrForbidden
				}
				observation := &authorization.SourceMetadataCheck{ObservedAt: time.Now()}
				for _, target := range targets {
					observation.Targets = append(observation.Targets, authorization.SourceMetadataTargetCheck{Target: target, Visible: true})
				}
				return observation, nil
			})
			if calls != 2 {
				t.Fatalf("calls=%d", calls)
			}
			if failSecond {
				if err == nil || result != nil {
					t.Fatal("first batch leaked after second batch failed")
				}
			} else if err != nil || len(result.Children) != 201 || result.Metadata["item_count"] != 201 {
				t.Fatalf("complete batch result: %+v %v", result, err)
			}
		})
	}
}

func TestFileAndObjectMetadataViewsShareCurrentVisibility(t *testing.T) {
	for _, kind := range []string{"nfs", "minio"} {
		t.Run(kind, func(t *testing.T) {
			db := metatest.OpenMetadataDB(t)
			tenant, engine := uint(7), uint(9)
			svc := newResourceTreeTestServiceWithDB(t, db, tenant, engine)
			svc.engineService.cacheEngine(tenant, &commonModels.Engine{ID: engine, TenantID: &tenant, Name: "Files", EngineType: kind, LifecycleState: "active"})
			rootTerm, branchTerm, itemType := "root", "dir", "file"
			if kind == "minio" {
				rootTerm, branchTerm, itemType = "service", "bucket", "object"
			}
			root := createResourceTreeNode(t, db, models.MetaNode{TenantID: tenant, EngineID: engine, NodeType: rootTerm, Name: "Files", FullName: "", Depth: 1})
			folder := createResourceTreeNode(t, db, models.MetaNode{TenantID: tenant, EngineID: engine, ParentNodeID: &root.ID, NodeType: branchTerm, Name: "outputs", FullName: "outputs", Depth: 2})
			candidates := []models.MetaItemLite{}
			for _, name := range []string{"nodes.gpkg", "hidden.gpkg"} {
				item := createResourceTreeItem(t, db, models.MetaItem{TenantID: tenant, EngineID: engine, NodeID: folder.ID, ItemType: itemType, Name: name, FullName: "outputs/" + name, Fingerprint: commonModels.GenerateItemFingerprint(engine, "outputs/"+name), Attributes: models.JSONMap{"item": map[string]any{"layout": "single", "format": "geopackage", "data_type": "container"}, "statistics": map[string]any{"private": "private sample"}}})
				candidates = append(candidates, models.MetaItemLite{ID: item.ID, TenantID: tenant, EngineID: engine, NodeID: folder.ID, ItemType: itemType, Name: item.Name, FullName: item.FullName, Attributes: map[string]any(item.Attributes)})
			}
			allow := true
			ctx := WithMetadataVisibility(t.Context(), func(_ context.Context, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
				if len(targets) != 2 {
					t.Fatalf("candidate set incomplete: %d", len(targets))
				}
				result := &authorization.SourceMetadataCheck{ObservedAt: time.Now()}
				for _, target := range targets {
					if target.Segments[0].Term != rootTerm {
						t.Fatal("wrong engine root")
					}
					result.Targets = append(result.Targets, authorization.SourceMetadataTargetCheck{Target: target, Visible: allow && target.Segments[len(target.Segments)-1].Name == "nodes.gpkg"})
				}
				return result, nil
			})
			items, err := svc.FilterMetadataItems(ctx, tenant, candidates)
			if err != nil || len(items) != 1 || items[0].Name != "nodes.gpkg" {
				t.Fatalf("items: %+v %v", items, err)
			}
			raw, _ := json.Marshal(items)
			if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "hidden.gpkg") {
				t.Fatal("metadata list leaked content or hidden item")
			}
			count, err := svc.CountVisibleMetadataItems(ctx, tenant)
			if err != nil || count != 1 {
				t.Fatal("count differs from list", count, err)
			}
			tree, err := svc.GetTree(ctx, tenant, engine, -1)
			raw, _ = json.Marshal(tree)
			if err != nil || tree.Metadata["item_count"] != 1 || strings.Contains(string(raw), "hidden.gpkg") || strings.Contains(string(raw), "private") {
				t.Fatalf("tree: %s %v", raw, err)
			}
			if err := svc.CheckMetadataBranch(ctx, tenant, engine, "outputs"); err != nil {
				t.Fatal(err)
			}
			allow = false
			count, err = svc.CountVisibleMetadataItems(ctx, tenant)
			if err != nil || count != 0 {
				t.Fatal("revoked count retained", count, err)
			}
			tree, err = svc.GetTree(ctx, tenant, engine, -1)
			if err != nil || len(tree.Children) != 0 || tree.Metadata["item_count"] != 0 {
				t.Fatal("revoked tree retained", err)
			}
		})
	}
}

func visibilityTestTargetResolver(root *resourcetree.TreeNode, engineType string) func(context.Context, uint) (plugin.EngineCatalogPath, error) {
	return func(_ context.Context, itemID uint) (plugin.EngineCatalogPath, error) {
		model, err := dbbridge.EngineCatalogModel(engineType)
		if err != nil {
			return plugin.EngineCatalogPath{}, err
		}
		var result plugin.EngineCatalogPath
		var walk func(*resourcetree.TreeNode) error
		walk = func(node *resourcetree.TreeNode) error {
			loc, err := resourcetree.ParseURI(node.Locator)
			if err != nil {
				return err
			}
			if loc.ItemID != nil && *loc.ItemID == itemID {
				result, err = resourcetree.EngineCatalogPathFromLocator(model, loc)
				return err
			}
			for _, child := range node.Children {
				if err := walk(child); err != nil {
					return err
				}
			}
			return nil
		}
		if err := walk(root); err != nil {
			return result, err
		}
		if result.EngineID == 0 {
			return result, commonapi.ErrForbidden
		}
		return result, nil
	}
}
