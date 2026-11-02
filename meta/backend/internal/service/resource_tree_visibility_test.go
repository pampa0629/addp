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
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
)

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
			result, err := filterPGResourceTree(t.Context(), root, 9, func(_ context.Context, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
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
			result, err := filterPGResourceTree(t.Context(), root, 9, func(_ context.Context, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
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
