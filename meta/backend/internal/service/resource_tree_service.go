package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	metaErrors "github.com/addp/meta/internal/errors"
	metaModels "github.com/addp/meta/internal/models"
)

type ResourceTreeService struct {
	engineService        *EngineService
	metadataQueryService *MetadataQueryService
	treeBuilder          *resourcetree.TreeBuilder
}

func NewResourceTreeService(engineService *EngineService, metadataQueryService *MetadataQueryService) *ResourceTreeService {
	return &ResourceTreeService{
		engineService:        engineService,
		metadataQueryService: metadataQueryService,
		treeBuilder:          resourcetree.NewTreeBuilder(),
	}
}

func (s *ResourceTreeService) GetTree(ctx context.Context, tenantID, engineID uint, expandDepth int) (*resourcetree.TreeNode, error) {
	_ = ctx
	engine, err := s.getEngine(engineID, tenantID)
	if err != nil {
		return nil, err
	}
	metaTree, err := s.metadataQueryService.GetMetadataTree(tenantID, engineID)
	if err != nil {
		return nil, err
	}
	nodes := s.metadataTreeNodes(engine.EngineType, metaTree, expandDepth)
	return s.treeBuilder.BuildFromMeta(engine, nodes, expandDepth)
}

func (s *ResourceTreeService) GetNode(ctx context.Context, tenantID, engineID uint, locatorURI string) (*resourcetree.TreeNode, error) {
	_ = ctx
	loc, err := resourcetree.ParseURI(locatorURI)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", metaErrors.ErrInvalidResourceLocator, err)
	}
	if loc.EngineID != engineID {
		return nil, fmt.Errorf("%w: locator engine_id %d does not match requested engine_id %d", metaErrors.ErrInvalidResourceLocator, loc.EngineID, engineID)
	}
	if loc.NodeID == nil || *loc.NodeID == 0 {
		return nil, fmt.Errorf("%w: resource tree node requires locator node_id", metaErrors.ErrInvalidResourceLocator)
	}

	engine, err := s.getEngine(engineID, tenantID)
	if err != nil {
		return nil, err
	}
	childNodes, err := s.metadataQueryService.GetNodeChildren(tenantID, *loc.NodeID)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to get meta node children: %v", metaErrors.ErrNodeNotFound, err)
	}
	items, err := s.metadataQueryService.GetNodeItems(tenantID, *loc.NodeID)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to get meta node items: %v", metaErrors.ErrNodeNotFound, err)
	}

	children := make([]*resourcetree.TreeNode, 0, len(childNodes)+len(items))
	presentationNodes := commonNodesFromLite(childNodes)
	itemNodes := s.treeBuilder.ConvertMetaItemsForEngine(engine.EngineType, commonItemsFromLite(items))
	presentationNodes = append(presentationNodes, itemNodes...)
	wholeItemNodes, err := s.wholeScopeItemsForChildContainers(tenantID, engine, childNodes)
	if err != nil {
		return nil, err
	}
	presentationNodes = append(presentationNodes, wholeItemNodes...)
	if presentationTree, err := s.treeBuilder.BuildFromMeta(engine, presentationNodes, -1); err == nil && presentationTree != nil {
		children = presentationTree.Children
	} else {
		children = s.treeBuilder.ConvertMetaNodes(engine, presentationNodes)
	}

	parent := s.treeBuilder.ConvertNodeToTree(loc, map[string]interface{}{
		"engine_id":   engine.ID,
		"engine_type": engine.EngineType,
		"full_name":   loc.FullName(),
		"node_id":     *loc.NodeID,
	})
	parent.Children = children
	parent.HasChildren = len(children) > 0
	return parent, nil
}

func (s *ResourceTreeService) GetAncestors(ctx context.Context, tenantID, engineID uint, locatorURI string) (*metaModels.ResourceTreeAncestorsResponse, error) {
	_ = ctx
	loc, err := resourcetree.ParseURI(locatorURI)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", metaErrors.ErrInvalidResourceLocator, err)
	}
	if loc.EngineID != engineID {
		return nil, fmt.Errorf("%w: locator engine_id %d does not match requested engine_id %d", metaErrors.ErrInvalidResourceLocator, loc.EngineID, engineID)
	}
	if (loc.ItemID == nil || *loc.ItemID == 0) && (loc.NodeID == nil || *loc.NodeID == 0) {
		return nil, fmt.Errorf("%w: resource tree ancestors requires locator node_id or item_id", metaErrors.ErrInvalidResourceLocator)
	}
	engine, err := s.getEngine(engineID, tenantID)
	if err != nil {
		return nil, err
	}
	if loc.ItemID != nil && *loc.ItemID > 0 {
		return s.getItemAncestors(tenantID, engine, loc)
	}
	return s.getNodeAncestors(tenantID, engine, loc)
}

func (s *ResourceTreeService) Search(ctx context.Context, tenantID, engineID uint, keyword string, nodeTypes []string, limit int) (*metaModels.ResourceTreeSearchResponse, error) {
	keyword = strings.TrimSpace(keyword)
	if len([]rune(keyword)) < 2 {
		return nil, fmt.Errorf("%w: search keyword must be at least 2 characters", metaErrors.ErrInvalidResourceLocator)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	tree, err := s.GetTree(ctx, tenantID, engineID, -1)
	if err != nil {
		return nil, err
	}
	results := make([]*resourcetree.TreeNode, 0)
	searchResourceTree(tree, strings.ToLower(keyword), normalizeNodeTypeFilters(nodeTypes), &results)
	total := len(results)
	if limit < total {
		results = results[:limit]
	}
	return &metaModels.ResourceTreeSearchResponse{
		Keyword: keyword,
		Total:   total,
		Results: results,
	}, nil
}

func (s *ResourceTreeService) getEngine(engineID, tenantID uint) (*commonModels.Engine, error) {
	if s.engineService == nil {
		return nil, fmt.Errorf("engine service is not configured")
	}
	engine, err := s.engineService.GetEngine(engineID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", metaErrors.ErrEngineNotFound, err)
	}
	if engine == nil {
		return nil, metaErrors.ErrEngineNotFound
	}
	return engine, nil
}

func searchResourceTree(node *resourcetree.TreeNode, keyword string, nodeTypes map[string]struct{}, results *[]*resourcetree.TreeNode) {
	if node == nil {
		return
	}
	typeMatch := len(nodeTypes) == 0
	if !typeMatch {
		_, typeMatch = nodeTypes[strings.ToLower(strings.TrimSpace(node.Type))]
	}
	labelMatch := strings.Contains(strings.ToLower(node.Label), keyword)
	if typeMatch && labelMatch {
		*results = append(*results, node)
	}
	for _, child := range node.Children {
		searchResourceTree(child, keyword, nodeTypes, results)
	}
}

func normalizeNodeTypeFilters(nodeTypes []string) map[string]struct{} {
	if len(nodeTypes) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(nodeTypes))
	for _, nodeType := range nodeTypes {
		nodeType = strings.ToLower(strings.TrimSpace(nodeType))
		if nodeType != "" {
			out[nodeType] = struct{}{}
		}
	}
	return out
}

func (s *ResourceTreeService) metadataTreeNodes(engineType string, tree *metaModels.MetadataTreeResponse, expandDepth int) []*commonModels.MetaNode {
	if tree == nil {
		return nil
	}
	nodes := make([]*commonModels.MetaNode, 0, len(tree.TopNodes)+len(tree.ChildNodes)+len(tree.Items))
	topNodes := commonNodesFromLite(tree.TopNodes)
	nodes = append(nodes, topNodes...)
	includedNodeIDs := make(map[uint]struct{}, len(tree.TopNodes)+len(tree.ChildNodes))
	for _, node := range topNodes {
		if node != nil {
			includedNodeIDs[node.ID] = struct{}{}
		}
	}
	rootDepth := resourceTreeRootDepth(topNodes)
	for _, node := range commonNodesFromLite(tree.ChildNodes) {
		if expandDepth == -1 || node.Depth <= rootDepth+expandDepth {
			nodes = append(nodes, node)
			includedNodeIDs[node.ID] = struct{}{}
		}
	}
	if expandDepth == -1 || expandDepth >= 2 {
		for _, node := range s.treeBuilder.ConvertMetaItemsForEngine(engineType, commonItemsFromLite(tree.Items)) {
			if expandDepth != -1 && node.Depth > rootDepth+expandDepth {
				continue
			}
			if node.ParentNodeID != nil {
				if _, ok := includedNodeIDs[*node.ParentNodeID]; !ok {
					continue
				}
			}
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func resourceTreeRootDepth(nodes []*commonModels.MetaNode) int {
	if len(nodes) == 0 {
		return 0
	}
	rootDepth := nodes[0].Depth
	for _, node := range nodes[1:] {
		if node != nil && node.Depth < rootDepth {
			rootDepth = node.Depth
		}
	}
	return rootDepth
}

func (s *ResourceTreeService) getNodeAncestors(tenantID uint, engine *commonModels.Engine, loc *resourcetree.ResourceLocator) (*metaModels.ResourceTreeAncestorsResponse, error) {
	if loc == nil || loc.NodeID == nil || *loc.NodeID == 0 {
		return nil, fmt.Errorf("resource tree ancestors requires locator node_id")
	}
	currentNode, err := s.metadataQueryService.GetNodeByCatalogPath(tenantID, engine.ID, loc.FullName())
	if err != nil {
		return nil, fmt.Errorf("%w: failed to resolve meta node by locator path: %v", metaErrors.ErrNodeNotFound, err)
	}
	nodes, err := s.metadataQueryService.GetNodeAncestors(tenantID, currentNode.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to get meta node ancestors: %v", metaErrors.ErrNodeNotFound, err)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: meta node ancestors is empty", metaErrors.ErrNodeNotFound)
	}
	chain := s.treeBuilder.ConvertMetaNodes(engine, commonNodesFromLite(nodes))
	return &metaModels.ResourceTreeAncestorsResponse{
		EngineID:      engine.ID,
		TargetLocator: chain[len(chain)-1].Locator,
		TargetKind:    "node",
		Ancestors:     chain,
	}, nil
}

func (s *ResourceTreeService) getItemAncestors(tenantID uint, engine *commonModels.Engine, loc *resourcetree.ResourceLocator) (*metaModels.ResourceTreeAncestorsResponse, error) {
	if loc == nil || loc.ItemID == nil || *loc.ItemID == 0 {
		return nil, fmt.Errorf("resource tree ancestors requires locator item_id")
	}
	currentItem, err := s.metadataQueryService.GetItemByCatalogPath(tenantID, engine.ID, loc.FullName())
	if err != nil {
		return nil, fmt.Errorf("%w: failed to resolve meta item by locator path: %v", metaErrors.ErrItemNotFound, err)
	}
	result, err := s.metadataQueryService.GetItemAncestors(tenantID, currentItem.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to get meta item ancestors: %v", metaErrors.ErrItemNotFound, err)
	}
	if result == nil {
		return nil, fmt.Errorf("%w: meta item ancestors is empty", metaErrors.ErrItemNotFound)
	}
	chain := s.treeBuilder.ConvertMetaNodes(engine, commonNodesFromLite(result.Ancestors))
	itemNodes := s.treeBuilder.ConvertMetaItemsForEngine(engine.EngineType, commonItemsFromLite([]metaModels.MetaItemLite{result.Item}))
	itemTreeNodes := s.treeBuilder.ConvertMetaNodes(engine, itemNodes)
	if len(itemTreeNodes) != 1 {
		return nil, fmt.Errorf("%w: failed to convert meta item ancestor target", metaErrors.ErrItemNotFound)
	}
	if shouldFoldWholeScopeItemAncestor(result.Item, result.Ancestors) {
		chain = chain[:len(chain)-1]
	}
	chain = append(chain, itemTreeNodes[0])
	return &metaModels.ResourceTreeAncestorsResponse{
		EngineID:      engine.ID,
		TargetLocator: itemTreeNodes[0].Locator,
		TargetKind:    "item",
		Ancestors:     chain,
	}, nil
}

func commonNodesFromLite(nodes []metaModels.MetaNodeLite) []*commonModels.MetaNode {
	out := make([]*commonModels.MetaNode, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, &commonModels.MetaNode{
			ID:             node.ID,
			TenantID:       node.TenantID,
			EngineID:       node.EngineID,
			ParentNodeID:   node.ParentNodeID,
			NodeType:       node.NodeType,
			Name:           node.Name,
			FullName:       node.FullName,
			Depth:          node.Depth,
			Path:           node.Path,
			ScanStatus:     node.ScanStatus,
			ScannedDepth:   node.ScannedDepth,
			LastScanAt:     parseMetaTimePtr(node.ScannedAt),
			ItemCount:      node.ItemCount,
			HasChildren:    node.HasChildren,
			TotalSizeBytes: node.TotalSizeBytes,
			Attributes:     node.Attributes,
		})
	}
	return out
}

func commonItemsFromLite(items []metaModels.MetaItemLite) []commonModels.MetaItem {
	out := make([]commonModels.MetaItem, 0, len(items))
	for _, item := range items {
		out = append(out, commonModels.MetaItem{
			ID:            item.ID,
			TenantID:      item.TenantID,
			EngineID:      item.EngineID,
			NodeID:        item.NodeID,
			ItemType:      item.ItemType,
			Name:          item.Name,
			FullName:      item.FullName,
			RowCount:      item.RowCount,
			SizeBytes:     item.SizeBytes,
			DataUpdatedAt: parseMetaTimePtr(item.DataUpdatedAt),
			ScannedAt:     parseMetaTimePtr(item.ScannedAt),
			ScannedDepth:  item.ScannedDepth,
			Attributes:    item.Attributes,
		})
	}
	return out
}

func (s *ResourceTreeService) wholeScopeItemsForChildContainers(tenantID uint, engine *commonModels.Engine, childNodes []metaModels.MetaNodeLite) ([]*commonModels.MetaNode, error) {
	childByID := make(map[uint]metaModels.MetaNodeLite, len(childNodes))
	childNodeIDs := make([]uint, 0, len(childNodes))
	for _, node := range childNodes {
		if !isPathContainerNodeTypeForResourceTree(node.NodeType) {
			continue
		}
		childByID[node.ID] = node
		childNodeIDs = append(childNodeIDs, node.ID)
	}
	if len(childNodeIDs) == 0 {
		return nil, nil
	}

	items, err := s.metadataQueryService.GetItemsForNodes(tenantID, childNodeIDs)
	if err != nil {
		return nil, err
	}
	wholeItems := make([]metaModels.MetaItemLite, 0)
	for _, item := range items {
		parent, ok := childByID[item.NodeID]
		if !ok || !sameResourceTreeFullName(parent.FullName, item.FullName) || !isWholeScopeItemAttributes(item.Attributes) {
			continue
		}
		wholeItems = append(wholeItems, item)
	}
	return s.treeBuilder.ConvertMetaItemsForEngine(engine.EngineType, commonItemsFromLite(wholeItems)), nil
}

func shouldFoldWholeScopeItemAncestor(item metaModels.MetaItemLite, ancestors []metaModels.MetaNodeLite) bool {
	if len(ancestors) == 0 || !isWholeScopeItemAttributes(item.Attributes) {
		return false
	}
	parent := ancestors[len(ancestors)-1]
	return item.NodeID == parent.ID &&
		sameResourceTreeFullName(parent.FullName, item.FullName) &&
		isPathContainerNodeTypeForResourceTree(parent.NodeType)
}

func isWholeScopeItemAttributes(attrs map[string]interface{}) bool {
	return strings.EqualFold(strings.TrimSpace(commonJSON.StringFromSections(attrs, "layout", "item")), "whole")
}

func sameResourceTreeFullName(left, right string) bool {
	return strings.Trim(strings.TrimSpace(left), "/") == strings.Trim(strings.TrimSpace(right), "/")
}

func isPathContainerNodeTypeForResourceTree(nodeType string) bool {
	switch strings.ToLower(strings.TrimSpace(nodeType)) {
	case "root", "dir", "directory", "prefix", "bucket":
		return true
	default:
		return false
	}
}

func parseMetaTimePtr(value *string) *time.Time {
	if value == nil || *value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil
	}
	return &parsed
}
