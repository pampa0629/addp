package service

import (
	"context"
	"gorm.io/gorm"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
)

// FilterMetadataItems projects a complete candidate list before the API returns it.
// It uses owner-stored parent identities, never splits a display path into a target.
func (s *ResourceTreeService) FilterMetadataItems(ctx context.Context, tenantID uint, items []models.MetaItemLite) ([]models.MetaItemLite, error) {
	check, userQuery := metadataVisibility(ctx)
	if !userQuery {
		return items, nil
	}
	if s == nil || s.engineService == nil || s.metadataQueryService == nil || check == nil {
		return nil, commonapi.ErrForbidden
	}
	byEngine := map[uint][]models.MetaItemLite{}
	for _, item := range items {
		if item.TenantID != tenantID {
			return nil, commonapi.ErrForbidden
		}
		byEngine[item.EngineID] = append(byEngine[item.EngineID], item)
	}
	projected := map[uint]models.MetaItemLite{}
	for engineID, candidates := range byEngine {
		engine, err := s.getEngine(engineID, tenantID)
		if err != nil {
			return nil, err
		}
		if engine.EngineType != "postgresql" {
			for _, item := range candidates {
				projected[item.ID] = item
			}
			continue
		}
		ids := []uint{}
		for _, item := range candidates {
			ids = append(ids, item.NodeID)
		}
		var parents []models.MetaNode
		if err := s.metadataQueryService.db.WithContext(ctx).Where("tenant_id = ? AND engine_id = ? AND id IN ? AND deleted_at IS NULL", tenantID, engineID, ids).Find(&parents).Error; err != nil {
			return nil, err
		}
		parentByID := map[uint]models.MetaNode{}
		for _, parent := range parents {
			parentByID[parent.ID] = parent
		}
		targets := []plugin.EngineCatalogPath{}
		keys := map[uint]string{}
		for _, item := range candidates {
			if item.ItemType != "table" {
				continue
			}
			parent, ok := parentByID[item.NodeID]
			if !ok || parent.NodeType != plugin.EngineCatalogTermSchema || parent.Name == "" || parent.FullName != parent.Name || item.Name == "" || item.FullName != parent.Name+"."+item.Name {
				return nil, commonapi.ErrForbidden
			}
			target := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermSchema, parent.Name, item.Name)
			key, err := authorization.EncodeSharingTarget(target)
			if err != nil {
				return nil, commonapi.ErrForbidden
			}
			keys[item.ID] = string(key)
			targets = append(targets, target)
		}
		visibility, err := observeMetadataVisibility(ctx, targets, check)
		if err != nil {
			return nil, err
		}
		for _, item := range candidates {
			if key, ok := keys[item.ID]; ok && visibility[key] {
				projected[item.ID] = metaquery.MetadataOnlyItem(item)
			}
		}
	}
	result := make([]models.MetaItemLite, 0, len(items))
	for _, item := range items {
		if safe, ok := projected[item.ID]; ok {
			result = append(result, safe)
		}
	}
	return result, nil
}

// FilterMetadataNodes keeps only navigation nodes with visible descendants.
// Counts come from the same filtered tree, not from raw subtree aggregates.
func (s *ResourceTreeService) FilterMetadataNodes(ctx context.Context, tenantID uint, nodes []models.MetaNodeLite) ([]models.MetaNodeLite, error) {
	if _, userQuery := metadataVisibility(ctx); !userQuery {
		return nodes, nil
	}
	if s == nil || s.engineService == nil {
		return nil, commonapi.ErrForbidden
	}
	indexes := map[uint]map[uint]*resourcetree.TreeNode{}
	pg := map[uint]bool{}
	result := make([]models.MetaNodeLite, 0, len(nodes))
	for _, node := range nodes {
		if node.TenantID != tenantID {
			return nil, commonapi.ErrForbidden
		}
		if _, checked := pg[node.EngineID]; !checked {
			engine, err := s.getEngine(node.EngineID, tenantID)
			if err != nil {
				return nil, err
			}
			pg[node.EngineID] = engine.EngineType == "postgresql"
			if pg[node.EngineID] {
				tree, err := s.GetTree(ctx, tenantID, node.EngineID, -1)
				if err != nil {
					return nil, err
				}
				indexes[node.EngineID], _ = metadataTreeIdentityIndex(tree)
			}
		}
		if !pg[node.EngineID] {
			result = append(result, node)
			continue
		}
		if visible, ok := indexes[node.EngineID][node.ID]; ok {
			count, _ := visible.Metadata["item_count"].(int)
			result = append(result, metaquery.MetadataOnlyNode(node, count, visible.HasChildren))
		}
	}
	return result, nil
}

func (s *ResourceTreeService) FilterMetadataTree(ctx context.Context, tenantID, engineID uint, tree *models.MetadataTreeResponse) (*models.MetadataTreeResponse, error) {
	check, userQuery := metadataVisibility(ctx)
	if !userQuery {
		return tree, nil
	}
	if s == nil || s.engineService == nil || tree == nil {
		return nil, commonapi.ErrForbidden
	}
	engine, err := s.getEngine(engineID, tenantID)
	if err != nil {
		return nil, err
	}
	if engine.EngineType != "postgresql" {
		return tree, nil
	}
	built, err := s.treeBuilder.BuildFromMetadataTree(engine, commonMetadataTree(tree))
	if err != nil {
		return nil, err
	}
	visible, err := filterPGResourceTree(ctx, built, engineID, check)
	if err != nil {
		return nil, err
	}
	nodes, items := metadataTreeIdentityIndex(visible)
	safeNodes := func(input []models.MetaNodeLite) []models.MetaNodeLite {
		output := make([]models.MetaNodeLite, 0, len(input))
		for _, node := range input {
			if safe, ok := nodes[node.ID]; ok {
				count, _ := safe.Metadata["item_count"].(int)
				output = append(output, metaquery.MetadataOnlyNode(node, count, safe.HasChildren))
			}
		}
		return output
	}
	result := &models.MetadataTreeResponse{TopNodes: safeNodes(tree.TopNodes), ChildNodes: safeNodes(tree.ChildNodes), Items: []models.MetaItemLite{}}
	for _, item := range tree.Items {
		if items[item.ID] {
			result.Items = append(result.Items, metaquery.MetadataOnlyItem(item))
		}
	}
	return result, nil
}

func metadataTreeIdentityIndex(root *resourcetree.TreeNode) (map[uint]*resourcetree.TreeNode, map[uint]bool) {
	nodes := map[uint]*resourcetree.TreeNode{}
	items := map[uint]bool{}
	var walk func(*resourcetree.TreeNode)
	walk = func(node *resourcetree.TreeNode) {
		if node == nil {
			return
		}
		loc, err := resourcetree.ParseURI(node.Locator)
		if err != nil {
			return
		}
		if loc.ItemID != nil {
			items[*loc.ItemID] = true
		} else if loc.NodeID != nil {
			nodes[*loc.NodeID] = node
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(root)
	return nodes, items
}

// CountVisibleMetadataItems counts the same authorized identity set as item queries.
// Source row counts, samples and attributes are not needed for this count.
func (s *ResourceTreeService) CountVisibleMetadataItems(ctx context.Context, tenantID uint) (int64, error) {
	if s == nil || s.metadataQueryService == nil {
		return 0, commonapi.ErrForbidden
	}
	if _, userQuery := metadataVisibility(ctx); !userQuery {
		return s.metadataQueryService.CountItems(tenantID)
	}
	var items []models.MetaItem
	err := s.metadataQueryService.db.WithContext(ctx).Select("id", "tenant_id", "engine_id", "node_id", "item_type", "name", "full_name", "fingerprint").Where("tenant_id = ? AND deleted_at IS NULL", tenantID).Find(&items).Error
	if err != nil {
		return 0, err
	}
	candidates := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		candidates[i] = metaquery.ToMetaItemLite(item)
	}
	visible, err := s.FilterMetadataItems(ctx, tenantID, candidates)
	if err != nil {
		return 0, err
	}
	return int64(len(visible)), nil
}

// FilterMetadataEngineStats preserves engine selection facts while recomputing PG
// scan counts from visible branches only. It never changes stored scan statistics.
func (s *ResourceTreeService) FilterMetadataEngineStats(ctx context.Context, tenantID uint, engines []*models.ResourceWithStats) ([]*models.ResourceWithStats, error) {
	if _, userQuery := metadataVisibility(ctx); !userQuery {
		return engines, nil
	}
	if s == nil || s.metadataQueryService == nil {
		return nil, commonapi.ErrForbidden
	}
	result := make([]*models.ResourceWithStats, 0, len(engines))
	for _, engine := range engines {
		if engine == nil {
			continue
		}
		if engine.ResourceType != "postgresql" {
			result = append(result, engine)
			continue
		}
		tree, err := s.metadataQueryService.GetMetadataTree(tenantID, engine.EngineID)
		if err != nil {
			return nil, err
		}
		safeTree, err := s.FilterMetadataTree(ctx, tenantID, engine.EngineID, tree)
		if err != nil {
			return nil, err
		}
		visibleIDs := map[uint]bool{}
		roots := map[uint]bool{}
		for _, node := range safeTree.TopNodes {
			roots[node.ID] = true
		}
		for _, node := range safeTree.ChildNodes {
			visibleIDs[node.ID] = true
		}
		safe := *engine
		safe.TotalCatalogNodes, safe.ScannedCatalogNodes, safe.UnscannedCatalogNodes = 0, 0, 0
		safe.ScannedAt, safe.CheckMessage = "", ""
		for _, node := range tree.ChildNodes {
			if !visibleIDs[node.ID] || node.ParentNodeID == nil || !roots[*node.ParentNodeID] {
				continue
			}
			safe.TotalCatalogNodes++
			if node.ScanStatus == "completed" {
				safe.ScannedCatalogNodes++
			}
			if node.ScannedAt != nil && *node.ScannedAt > safe.ScannedAt {
				safe.ScannedAt = *node.ScannedAt
			}
		}
		safe.UnscannedCatalogNodes = safe.TotalCatalogNodes - safe.ScannedCatalogNodes
		result = append(result, &safe)
	}
	return result, nil
}

// CheckMetadataBranch applies the same absent/hidden result to scoped list roots.
func (s *ResourceTreeService) CheckMetadataBranch(ctx context.Context, tenantID, engineID uint, branch string) error {
	if _, userQuery := metadataVisibility(ctx); !userQuery {
		return nil
	}
	if s == nil || s.engineService == nil || s.metadataQueryService == nil {
		return commonapi.ErrForbidden
	}
	engine, err := s.getEngine(engineID, tenantID)
	if err != nil {
		return err
	}
	if engine.EngineType != "postgresql" {
		return nil
	}
	node, err := s.metadataQueryService.GetNodeByCatalogPath(tenantID, engineID, branch)
	if err != nil {
		return err
	}
	visible, err := s.FilterMetadataNodes(ctx, tenantID, []models.MetaNodeLite{*node})
	if err != nil {
		return err
	}
	if len(visible) != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
