package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func assertResourceTreeItemBoundary(t *testing.T, node *resourcetree.TreeNode) {
	t.Helper()
	loc, err := resourcetree.ParseURI(node.Locator)
	if err != nil {
		t.Fatal(err)
	}
	if loc.ItemID != nil {
		encoded, err := json.Marshal(node)
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			Metadata map[string]interface{} `json:"metadata"`
		}
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"item_count", "node_id", "is_meta_item"} {
			if _, exists := wire.Metadata[key]; exists {
				t.Fatalf("item leaked %s: %s", key, encoded)
			}
		}
		if node.Metadata["item_id"] != *loc.ItemID || node.HasChildren || len(node.Children) != 0 || node.Metadata["has_children"] != false {
			t.Fatalf("item identity or expansion differs: %s", encoded)
		}
	} else if _, exists := node.Metadata["item_count"]; !exists {
		t.Fatalf("node aggregate missing: %+v", node)
	}
	for _, child := range node.Children {
		assertResourceTreeItemBoundary(t, child)
	}
}

func TestResourceTreeItemContractAcrossQueryEntrypoints(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)
	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "S3", FullName: "", Depth: 0})
	bucket := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "bucket", Name: "data", FullName: "data", Depth: 1})
	item := createResourceTreeItem(t, db, models.MetaItem{TenantID: 7, EngineID: 9, NodeID: bucket.ID, ItemType: "object", Name: "report.pdf", FullName: "data/report.pdf", Fingerprint: "report"})
	tree, err := svc.GetTree(t.Context(), 7, 9, -1)
	if err != nil {
		t.Fatal(err)
	}
	assertResourceTreeItemBoundary(t, tree)
	if len(tree.Children) != 1 || len(tree.Children[0].Children) != 1 || tree.Metadata["item_count"] != 1 {
		t.Fatalf("tree lost item or aggregate: %+v", tree)
	}
	node, err := svc.GetNode(t.Context(), 7, 9, tree.Children[0].Locator)
	if err != nil {
		t.Fatal(err)
	}
	assertResourceTreeItemBoundary(t, node)
	ancestors, err := svc.GetAncestors(t.Context(), 7, 9, "addp://engine/9/path/data/report.pdf?type=object&item_id="+uintStringForTest(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, ancestor := range ancestors.Ancestors {
		assertResourceTreeItemBoundary(t, ancestor)
	}
	results, err := svc.Search(t.Context(), 7, 9, "report", nil, 10)
	if err != nil || len(results.Results) != 1 {
		t.Fatalf("search = %+v, %v", results, err)
	}
	assertResourceTreeItemBoundary(t, results.Results[0])
}

func TestResourceTreeDirectLeafCatalogUsesParentRelationshipForDepth(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)
	tenant := uint(7)
	svc.engineService.cacheEngine(tenant, &commonModels.Engine{ID: 9, TenantID: &tenant, EngineType: "kafka", LifecycleState: "active"})
	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", FullName: "", Depth: 1})
	item := createResourceTreeItem(t, db, models.MetaItem{TenantID: 7, EngineID: 9, NodeID: root.ID, ItemType: "topic", Name: "orders.v1", FullName: "orders.v1", Fingerprint: "orders"})
	tree, err := svc.GetTree(t.Context(), 7, 9, 1)
	if err != nil || len(tree.Children) != 1 || tree.Children[0].Label != "orders.v1" {
		t.Fatalf("direct leaf hidden by invented item depth: %+v, %v", tree, err)
	}
	assertResourceTreeItemBoundary(t, tree)
	if tree.Children[0].Locator != "addp://engine/9/path/orders.v1?type=topic&item_id="+uintStringForTest(item.ID) {
		t.Fatalf("native topic name changed: %s", tree.Children[0].Locator)
	}
}

func TestResourceTreeGetNodeRejectsInvalidLocatorAsBadRequest(t *testing.T) {
	svc := newResourceTreeTestService(t, 7, 9)

	_, err := svc.GetNode(t.Context(), 7, 9, "not-a-locator")
	if err == nil {
		t.Fatal("GetNode() error = nil, want invalid locator")
	}
	if !errors.Is(err, metaErrors.ErrInvalidResourceLocator) {
		t.Fatalf("error = %v, want ErrInvalidResourceLocator", err)
	}
}

func TestResourceTreeGetAncestorsRejectsEngineMismatchAsBadRequest(t *testing.T) {
	svc := newResourceTreeTestService(t, 7, 9)

	_, err := svc.GetAncestors(t.Context(), 7, 9, "addp://engine/8/path/addp?type=bucket&node_id=23")
	if err == nil {
		t.Fatal("GetAncestors() error = nil, want engine mismatch")
	}
	if !errors.Is(err, metaErrors.ErrInvalidResourceLocator) {
		t.Fatalf("error = %v, want ErrInvalidResourceLocator", err)
	}
}

func TestResourceTreeGetAncestorsRequiresLocatorIdentity(t *testing.T) {
	svc := newResourceTreeTestService(t, 7, 9)

	_, err := svc.GetAncestors(t.Context(), 7, 9, "addp://engine/9/path/addp?type=bucket")
	if err == nil {
		t.Fatal("GetAncestors() error = nil, want missing identity")
	}
	if !errors.Is(err, metaErrors.ErrInvalidResourceLocator) {
		t.Fatalf("error = %v, want ErrInvalidResourceLocator", err)
	}
}

func TestResourceTreeGetTreeUsesExpandDepthRelativeToCatalogRoot(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)

	root := createResourceTreeNode(t, db, models.MetaNode{
		TenantID: 7, EngineID: 9,
		NodeType: "service", Name: "Business MinIO", FullName: "", Depth: 1,
	})
	bucket := createResourceTreeNode(t, db, models.MetaNode{
		TenantID: 7, EngineID: 9, ParentNodeID: &root.ID,
		NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 2,
	})
	createResourceTreeItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: bucket.ID,
		ItemType: "object", Name: "report.pdf", FullName: "addp/report.pdf",
		Fingerprint: "fp-report",
	})

	tree, err := svc.GetTree(t.Context(), 7, 9, 1)
	if err != nil {
		t.Fatalf("GetTree() error = %v", err)
	}
	if len(tree.Children) != 1 {
		t.Fatalf("root children = %d, want first catalog level", len(tree.Children))
	}
	if tree.Children[0].Label != "addp" || tree.Children[0].Type != "bucket" {
		t.Fatalf("first child = %#v, want bucket addp", tree.Children[0])
	}
	if len(tree.Children[0].Children) != 0 {
		t.Fatalf("bucket children at expandDepth=1 = %d, want lazy item loading", len(tree.Children[0].Children))
	}

	tree, err = svc.GetTree(t.Context(), 7, 9, 2)
	if err != nil {
		t.Fatalf("GetTree(expandDepth=2) error = %v", err)
	}
	if len(tree.Children) != 1 || len(tree.Children[0].Children) != 1 {
		t.Fatalf("expanded tree children = %d/%d, want bucket item", len(tree.Children), len(tree.Children[0].Children))
	}
	if tree.Children[0].Children[0].Label != "report.pdf" {
		t.Fatalf("item label = %q, want report.pdf", tree.Children[0].Children[0].Label)
	}
}

func TestResourceTreeGetTreeMarksNodeOnlyPrefixAsExpandableAtDepthLimit(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)

	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "MinIO", FullName: "", Depth: 0})
	bucket := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 1})
	mosaics := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &bucket.ID, NodeType: "prefix", Name: "mosaics", FullName: "addp/mosaics", Depth: 2})
	createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &mosaics.ID, NodeType: "prefix", Name: "srtm-test", FullName: "addp/mosaics/srtm-test", Depth: 3})

	tree, err := svc.GetTree(t.Context(), 7, 9, 2)
	if err != nil {
		t.Fatalf("GetTree() error = %v", err)
	}
	mosaicsNode := findResourceTreeNodeForTest(tree, "addp/mosaics")
	if mosaicsNode == nil {
		t.Fatal("mosaics node not found")
	}
	if len(mosaicsNode.Children) != 0 {
		t.Fatalf("mosaics children = %d, want lazy boundary with no loaded children", len(mosaicsNode.Children))
	}
	if !mosaicsNode.HasChildren {
		t.Fatalf("mosaics hasChildren = false, want true because direct child prefix exists")
	}
}

func TestResourceTreeGetTreeDoesNotAttachOrphanWholeItemToRoot(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)

	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "root", Name: "NFS", FullName: "", Depth: 1})
	dir3d := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "dir", Name: "3d", FullName: "3d", Depth: 2})
	scene := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &dir3d.ID, NodeType: "dir", Name: "白塔3dtiles", FullName: "3d/白塔3dtiles", Depth: 3})
	container := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &scene.ID, NodeType: "dir", Name: "baita", FullName: "3d/白塔3dtiles/baita", Depth: 4})
	createResourceTreeItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: container.ID,
		ItemType: "file", Name: "baita", FullName: "3d/白塔3dtiles/baita",
		Fingerprint: "fp-baita-tiles",
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout": "whole",
				"format": "3dtiles",
			},
		},
	})

	tree, err := svc.GetTree(t.Context(), 7, 9, 2)
	if err != nil {
		t.Fatalf("GetTree() error = %v", err)
	}
	for _, child := range tree.Children {
		if child.Label == "baita" {
			t.Fatalf("root child contains orphan whole item: %#v", child)
		}
	}
	sceneNode := findResourceTreeNodeForTest(tree, "3d/白塔3dtiles")
	if sceneNode == nil {
		t.Fatal("scene node not found at expand depth boundary")
	}
	if len(sceneNode.Children) != 0 {
		t.Fatalf("scene children = %d, want lazy boundary without orphan item", len(sceneNode.Children))
	}
}

func TestResourceTreeGetAncestorsRewritesItemLocatorFromCurrentMetaFacts(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)

	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "MinIO", FullName: "", Depth: 0})
	bucket := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 1})
	item := createResourceTreeItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: bucket.ID,
		ItemType: "object", Name: "report.pdf", FullName: "addp/report.pdf",
		Fingerprint: "fp-report",
	})

	result, err := svc.GetAncestors(t.Context(), 7, 9, "addp://engine/9/path/addp/report.pdf?type=object&item_id=404")
	if err != nil {
		t.Fatalf("GetAncestors() error = %v", err)
	}
	wantLocator := "addp://engine/9/path/addp/report.pdf?type=object&item_id=" + uintStringForTest(item.ID)
	if result.TargetLocator != wantLocator {
		t.Fatalf("TargetLocator = %q, want %q", result.TargetLocator, wantLocator)
	}
	if result.TargetKind != "item" {
		t.Fatalf("TargetKind = %q, want item", result.TargetKind)
	}
	if len(result.Ancestors) != 3 {
		t.Fatalf("ancestor len = %d, want 3", len(result.Ancestors))
	}
	if !strings.HasSuffix(result.Ancestors[len(result.Ancestors)-1].Locator, "item_id="+uintStringForTest(item.ID)) {
		t.Fatalf("last ancestor locator = %q, want current item id", result.Ancestors[len(result.Ancestors)-1].Locator)
	}
}

func TestResourceTreeGetNodePresentsWholeScopeItemAtParent(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)

	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "NFS", FullName: "", Depth: 0})
	dir3d := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "dir", Name: "3d", FullName: "3d", Depth: 1})
	scene := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &dir3d.ID, NodeType: "dir", Name: "白塔3dtiles", FullName: "3d/白塔3dtiles", Depth: 2})
	container := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &scene.ID, NodeType: "dir", Name: "baita", FullName: "3d/白塔3dtiles/baita", Depth: 3})
	item := createResourceTreeItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: container.ID,
		ItemType: "file", Name: "baita", FullName: "3d/白塔3dtiles/baita",
		Fingerprint: "fp-baita-tiles",
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout":    "whole",
				"data_type": "model3d",
				"format":    "3dtiles",
			},
		},
	})

	result, err := svc.GetNode(t.Context(), 7, 9, "addp://engine/9/path/3d/%E7%99%BD%E5%A1%943dtiles?type=dir&node_id="+uintStringForTest(scene.ID))
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if len(result.Children) != 1 {
		t.Fatalf("children = %d, want collapsed whole item: %#v", len(result.Children), result.Children)
	}
	child := result.Children[0]
	if child.Label != "baita" || child.Type != "file" {
		t.Fatalf("child = %s/%s, want baita/file", child.Label, child.Type)
	}
	if !strings.Contains(child.Locator, "item_id="+uintStringForTest(item.ID)) || strings.Contains(child.Locator, "node_id="+uintStringForTest(container.ID)) {
		t.Fatalf("child locator = %q, want item locator", child.Locator)
	}
	if got := child.Metadata["layout"]; got != "whole" {
		t.Fatalf("child layout = %#v, want whole", got)
	}
	if got := child.Metadata["format"]; got != "3dtiles" {
		t.Fatalf("child format = %#v, want 3dtiles", got)
	}
}

func TestResourceTreeGetAncestorsPresentsWholeScopeItemWithoutContainer(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)

	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "NFS", FullName: "", Depth: 0})
	dir3d := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "dir", Name: "3d", FullName: "3d", Depth: 1})
	scene := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &dir3d.ID, NodeType: "dir", Name: "白塔3dtiles", FullName: "3d/白塔3dtiles", Depth: 2})
	container := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &scene.ID, NodeType: "dir", Name: "baita", FullName: "3d/白塔3dtiles/baita", Depth: 3})
	item := createResourceTreeItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: container.ID,
		ItemType: "file", Name: "baita", FullName: "3d/白塔3dtiles/baita",
		Fingerprint: "fp-baita-tiles",
		Attributes: models.JSONMap{
			"item": map[string]interface{}{
				"layout": "whole",
				"format": "3dtiles",
			},
		},
	})

	result, err := svc.GetAncestors(t.Context(), 7, 9, "addp://engine/9/path/3d/%E7%99%BD%E5%A1%943dtiles/baita?type=file&item_id="+uintStringForTest(item.ID))
	if err != nil {
		t.Fatalf("GetAncestors() error = %v", err)
	}
	if len(result.Ancestors) != 4 {
		t.Fatalf("ancestor len = %d, want root/3d/scene/item", len(result.Ancestors))
	}
	for _, node := range result.Ancestors {
		if strings.Contains(node.Locator, "node_id="+uintStringForTest(container.ID)) {
			t.Fatalf("ancestor chain contains folded container locator: %q", node.Locator)
		}
	}
	last := result.Ancestors[len(result.Ancestors)-1]
	if last.Label != "baita" || !strings.Contains(last.Locator, "item_id="+uintStringForTest(item.ID)) {
		t.Fatalf("last ancestor = %s %q, want whole item", last.Label, last.Locator)
	}
}

func TestResourceTreeGetAncestorsMissingTargetIsNotFound(t *testing.T) {
	svc := newResourceTreeTestService(t, 7, 9)

	_, err := svc.GetAncestors(t.Context(), 7, 9, "addp://engine/9/path/missing?type=bucket&node_id=23")
	if err == nil {
		t.Fatal("GetAncestors() error = nil, want missing node")
	}
	if !errors.Is(err, metaErrors.ErrNodeNotFound) {
		t.Fatalf("error = %v, want ErrNodeNotFound", err)
	}
}

func TestResourceTreePreservesLiteTimeFactsInTreeMetadata(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)

	scannedAt := time.Date(2026, 6, 17, 10, 11, 12, 0, time.UTC)
	dataUpdatedAt := time.Date(2026, 6, 16, 9, 8, 7, 0, time.UTC)
	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "MinIO", FullName: "", Depth: 0})
	bucket := createResourceTreeNode(t, db, models.MetaNode{
		TenantID: 7, EngineID: 9, ParentNodeID: &root.ID,
		NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 1,
		ScannedAt: &scannedAt, ScannedDepth: models.ScannedDepthBasic, ScanStatus: "failed",
	})
	item := createResourceTreeItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: bucket.ID,
		ItemType: "object", Name: "report.pdf", FullName: "addp/report.pdf",
		Fingerprint:   "fp-report",
		DataUpdatedAt: &dataUpdatedAt,
		ScannedAt:     &scannedAt,
	})

	nodeResult, err := svc.GetNode(t.Context(), 7, 9, "addp://engine/9/path/addp?type=bucket&node_id="+uintStringForTest(bucket.ID))
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got := nodeResult.Metadata["scanned_at"]; got != scannedAt.Format(time.RFC3339) {
		t.Fatalf("parent scanned_at metadata = %#v, want %s", got, scannedAt.Format(time.RFC3339))
	}
	if nodeResult.Metadata["scanned_depth"] != models.ScannedDepthBasic || nodeResult.Metadata["scan_status"] != "failed" {
		t.Fatalf("scope state and completed depth must remain independent: %#v", nodeResult.Metadata)
	}
	if got := nodeResult.Metadata["item_count"]; got != 1 {
		t.Fatalf("parent item_count metadata = %#v, want 1", got)
	}
	var itemNodeFound bool
	for _, child := range nodeResult.Children {
		if strings.Contains(child.Locator, "item_id="+uintStringForTest(item.ID)) {
			itemNodeFound = true
			if got := child.Metadata["scanned_at"]; got != scannedAt.Format(time.RFC3339) {
				t.Fatalf("item scanned_at metadata = %#v, want %s", got, scannedAt.Format(time.RFC3339))
			}
			if got := child.Metadata["last_modified_at"]; got != dataUpdatedAt.Format(time.RFC3339) {
				t.Fatalf("item last_modified_at metadata = %#v, want %s", got, dataUpdatedAt.Format(time.RFC3339))
			}
		}
	}
	if !itemNodeFound {
		t.Fatalf("item node with id %d not found in children", item.ID)
	}

	ancestorResult, err := svc.GetAncestors(t.Context(), 7, 9, "addp://engine/9/path/addp?type=bucket&node_id="+uintStringForTest(bucket.ID))
	if err != nil {
		t.Fatalf("GetAncestors() error = %v", err)
	}
	last := ancestorResult.Ancestors[len(ancestorResult.Ancestors)-1]
	if got := last.Metadata["scanned_at"]; got != scannedAt.Format(time.RFC3339) {
		t.Fatalf("node scanned_at metadata = %#v, want %s", got, scannedAt.Format(time.RFC3339))
	}
}

func TestResourceTreeSearchFiltersByKeywordTypeAndLimit(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	svc := newResourceTreeTestServiceWithDB(t, db, 7, 9)

	root := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, NodeType: "service", Name: "MinIO", FullName: "", Depth: 0})
	bucket := createResourceTreeNode(t, db, models.MetaNode{TenantID: 7, EngineID: 9, ParentNodeID: &root.ID, NodeType: "bucket", Name: "addp", FullName: "addp", Depth: 1})
	createResourceTreeItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: bucket.ID,
		ItemType: "object", Name: "roads.csv", FullName: "addp/roads.csv",
		Fingerprint: "fp-roads-csv",
	})
	createResourceTreeItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: bucket.ID,
		ItemType: "table", Name: "roads", FullName: "addp/roads",
		Fingerprint: "fp-roads-table",
	})
	createResourceTreeItem(t, db, models.MetaItem{
		TenantID: 7, EngineID: 9, NodeID: bucket.ID,
		ItemType: "object", Name: "rivers.csv", FullName: "addp/rivers.csv",
		Fingerprint: "fp-rivers-csv",
	})

	result, err := svc.Search(t.Context(), 7, 9, "roads", []string{"object"}, 1)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.Keyword != "roads" {
		t.Fatalf("Keyword = %q, want roads", result.Keyword)
	}
	if result.Total != 1 || len(result.Results) != 1 {
		t.Fatalf("search total/results = %d/%d, want 1/1", result.Total, len(result.Results))
	}
	if result.Results[0].Type != "object" || result.Results[0].Label != "roads.csv" {
		t.Fatalf("search result = %#v, want object roads.csv", result.Results[0])
	}
}

func TestResourceTreeSearchRejectsShortKeyword(t *testing.T) {
	svc := newResourceTreeTestService(t, 7, 9)

	_, err := svc.Search(t.Context(), 7, 9, "r", nil, 50)
	if err == nil {
		t.Fatal("Search() error = nil, want short keyword error")
	}
	if !errors.Is(err, metaErrors.ErrInvalidResourceLocator) {
		t.Fatalf("error = %v, want ErrInvalidResourceLocator", err)
	}
}

func newResourceTreeTestService(t *testing.T, tenantID, engineID uint) *ResourceTreeService {
	t.Helper()
	return newResourceTreeTestServiceWithDB(t, metatest.OpenMetadataDB(t), tenantID, engineID)
}

func newResourceTreeTestServiceWithDB(t *testing.T, db *gorm.DB, tenantID, engineID uint) *ResourceTreeService {
	t.Helper()
	engineSvc := NewEngineService(db, nil)
	engineSvc.cacheEngine(tenantID, &commonModels.Engine{
		ID:             engineID,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "active",
	})
	return NewResourceTreeService(engineSvc, NewMetadataQueryService(db))
}

func createResourceTreeNode(t *testing.T, db *gorm.DB, node models.MetaNode) models.MetaNode {
	t.Helper()
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	return node
}

func createResourceTreeItem(t *testing.T, db *gorm.DB, item models.MetaItem) models.MetaItem {
	t.Helper()
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}
	return item
}

func uintStringForTest(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}

func findResourceTreeNodeForTest(node *resourcetree.TreeNode, fullName string) *resourcetree.TreeNode {
	if node == nil {
		return nil
	}
	if metadataFullName, ok := node.Metadata["full_name"].(string); ok && metadataFullName == fullName {
		return node
	}
	for _, child := range node.Children {
		if found := findResourceTreeNodeForTest(child, fullName); found != nil {
			return found
		}
	}
	return nil
}
