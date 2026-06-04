package service

import (
	"context"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	commonUtils "github.com/addp/common/utils"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
)

// ExplorerService 数据探查服务
// 作为协调层，负责：
// 1. 调用 TreeBuilder 构建资源树（使用 Common 模块的通用能力）
// 2. 调用 PreviewResolver 提供数据预览
// 3. 管理节点刷新
type ExplorerService struct {
	systemClient    *commonClient.SystemClient
	metaClient      *commonClient.MetaClient
	treeBuilder     *resourcetree.TreeBuilder
	previewResolver *preview.PreviewResolver
}

type RefreshNodeResult struct {
	Run *commonExecution.TaskExecution `json:"run"`
}

// NewExplorerService 创建数据探查服务
func NewExplorerService(
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
	previewResolver *preview.PreviewResolver,
) *ExplorerService {
	treeBuilder := resourcetree.NewTreeBuilder()

	return &ExplorerService{
		systemClient:    systemClient,
		metaClient:      metaClient,
		treeBuilder:     treeBuilder,
		previewResolver: previewResolver,
	}
}

// GetTree 获取引擎的资源树
// 参数:
//   - ctx: 上下文
//   - tenantID: 租户 ID
//   - engineID: 引擎 ID
//   - expandDepth: 展开深度（-1 表示全部展开）
//
// 返回: 资源树根节点
func (s *ExplorerService) GetTree(ctx context.Context, tenantID *uint, engineID uint, expandDepth int) (*resourcetree.TreeNode, error) {
	// 1. 通过 SystemClient 获取引擎信息
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system: %w", err)
	}

	// 验证租户权限
	if tenantID != nil && engine.TenantID != nil && *engine.TenantID != *tenantID {
		return nil, ErrEngineAccessDenied
	}

	// 2. 尝试从 Meta 获取资源树（可选，失败不影响功能）
	metaDepth := metadataExpandDepth(nil, expandDepth)
	metaNodes, err := s.getMetaNodes(ctx, engineID, engine.EngineType, metaDepth, tenantID)
	if err != nil {
		logger.L().Warn("获取 Meta 节点失败，使用降级方案", "engine_id", engineID, "error", err)
		// Meta 不可用时的降级方案：返回引擎根节点
		return s.buildEngineRootNode(engine), nil
	}

	// 3. 使用 TreeBuilder 构建树
	tree, err := s.treeBuilder.BuildFromMeta(engine, metaNodes, metaDepth)
	if err != nil {
		return nil, fmt.Errorf("failed to build tree: %w", err)
	}

	logger.L().Info("成功构建资源树", "engine_id", engineID, "children_count", len(tree.Children))
	return tree, nil
}

// RefreshNode 刷新指定节点
// 参数:
//   - ctx: 上下文
//   - tenantID: 租户 ID
//   - locator: ResourceLocator URI
//
// 返回: 已提交的扫描运行
func (s *ExplorerService) RefreshNode(ctx context.Context, tenantID *uint, locatorURI string, authToken string) (*RefreshNodeResult, error) {
	// 1. 解析 Locator
	loc, err := resourcetree.ParseURI(locatorURI)
	if err != nil {
		return nil, fmt.Errorf("invalid locator: %w", err)
	}

	// 2. 通过 SystemClient 验证引擎权限
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	engine, err := s.systemClient.GetEngine(loc.EngineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system: %w", err)
	}

	if tenantID != nil && engine.TenantID != nil && *engine.TenantID != *tenantID {
		return nil, ErrEngineAccessDenied
	}

	// 3. 提交 Meta 后台深度扫描运行，并保留运行记录用于前端反馈。
	if s.metaClient == nil {
		return nil, fmt.Errorf("meta client not available")
	}
	authToken = strings.TrimSpace(authToken)
	if authToken == "" {
		return nil, fmt.Errorf("metadata scan requires authorization token")
	}
	opts := refreshScanOptions(loc)
	logger.L().Info("提交 Meta 后台深度扫描", "engine_id", loc.EngineID, "type", loc.Type, "node_id", opts.NodeID, "item_id", opts.ItemID, "targets", opts.Targets)

	scanRun, err := s.metaClient.WithAuthToken(authToken).CreateManualScanRun(opts)
	if err != nil {
		logger.L().Warn("提交 Meta 后台深度扫描失败", "error", err)
		return nil, fmt.Errorf("failed to submit metadata scan: %w", err)
	}
	logger.L().Info("Meta 后台深度扫描已提交", "engine_id", loc.EngineID, "execution_id", scanRun.ExecutionID)

	return &RefreshNodeResult{Run: scanRun}, nil
}

func refreshScanOptions(loc *resourcetree.ResourceLocator) commonClient.MetaScanOptions {
	opts := commonClient.MetaScanOptions{
		EngineID:    loc.EngineID,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: "manual",
		Source:      "manager_refresh",
	}
	if loc.ItemID != nil && *loc.ItemID > 0 {
		opts.ItemID = *loc.ItemID
		return opts
	}
	if loc.NodeID != nil && *loc.NodeID > 0 {
		opts.NodeID = *loc.NodeID
		return opts
	}
	return opts
}

// ListEngines 获取引擎列表（用于前端显示）
func (s *ExplorerService) ListEngines(ctx context.Context, tenantID *uint) ([]*commonModels.Engine, error) {
	logger.L().Info("获取引擎列表")

	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	// 准备参数
	var tid uint
	if tenantID != nil {
		tid = *tenantID
	}

	// 通过 SystemClient 获取引擎列表（空字符串表示不过滤引擎类型）
	engines, err := s.systemClient.ListEngines("", tid)
	if err != nil {
		logger.L().Error("获取引擎列表失败", "error", err)
		return nil, fmt.Errorf("failed to list engines: %w", err)
	}

	// 转换为指针切片
	result := make([]*commonModels.Engine, len(engines))
	for i := range engines {
		result[i] = &engines[i]
	}

	logger.L().Info("获取引擎列表成功", "count", len(result))
	return result, nil
}

// GetEngineList 获取可用于探查的引擎列表
// Manager 模块只展示具有存储能力的引擎（capabilities 中包含 "storage"）
// 工作流引擎只有 "compute" 能力，不应显示在 Manager 数据探查界面
func (s *ExplorerService) GetEngineList(tenantID *uint) ([]*commonModels.Engine, error) {
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	// 准备参数
	var tid uint
	if tenantID != nil {
		tid = *tenantID
	}

	// 通过 SystemClient 获取引擎列表（空字符串表示不过滤引擎类型）
	engines, err := s.systemClient.ListEngines("", tid)
	if err != nil {
		return nil, fmt.Errorf("failed to list engines: %w", err)
	}

	// 过滤出具有存储能力的引擎（使用 common/utils 提供的统一工具）
	// 工作流引擎只有 "compute" 能力，不应显示在 Manager 数据探查界面
	var storageEngines []*commonModels.Engine
	for i := range engines {
		// 使用 common/utils 提供的 HasStorageCapability 函数检查存储能力
		if commonUtils.HasStorageCapability(&engines[i]) {
			storageEngines = append(storageEngines, &engines[i])
		}
	}

	return storageEngines, nil
}

// GetNodeChildren 获取节点的子节点（增量加载）
// 参数:
//   - ctx: 上下文
//   - tenantID: 租户 ID
//   - engineID: 引擎 ID
//   - locatorURI: 父节点的 ResourceLocator URI
//   - expandDepth: 保留 API 参数；直接子级加载路径固定只返回当前节点的一层 children/items
//
// 返回: 包含父节点 locator 和子节点列表的结构
func (s *ExplorerService) GetNodeChildren(ctx context.Context, tenantID *uint, engineID uint, locatorURI string, expandDepth int) (*resourcetree.TreeNode, error) {
	// 1. 解析 Locator
	loc, err := resourcetree.ParseURI(locatorURI)
	if err != nil {
		return nil, fmt.Errorf("invalid locator: %w", err)
	}
	if loc.EngineID != engineID {
		return nil, fmt.Errorf("locator engine_id %d does not match requested engine_id %d", loc.EngineID, engineID)
	}
	if loc.NodeID == nil || *loc.NodeID == 0 {
		return nil, fmt.Errorf("resource tree node expansion requires locator node_id")
	}

	// 2. 通过 SystemClient 验证引擎权限
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system: %w", err)
	}

	// 验证租户权限
	if tenantID != nil && engine.TenantID != nil && *engine.TenantID != *tenantID {
		return nil, ErrEngineAccessDenied
	}

	if s.metaClient == nil {
		return nil, fmt.Errorf("meta client not available")
	}
	s.metaClient.SetTenantID(tenantID)

	childNodes, err := s.metaClient.GetNodeChildren(*loc.NodeID)
	if err != nil {
		logger.L().Warn("获取 Meta 直接子节点失败", "locator", locatorURI, "node_id", *loc.NodeID, "error", err)
		return nil, fmt.Errorf("failed to get meta node children: %w", err)
	}

	items, err := s.metaClient.GetNodeItems(*loc.NodeID)
	if err != nil {
		logger.L().Warn("获取 Meta 节点 items 失败", "locator", locatorURI, "node_id", *loc.NodeID, "error", err)
		return nil, fmt.Errorf("failed to get meta node items: %w", err)
	}

	children := make([]*resourcetree.TreeNode, 0, len(childNodes)+len(items))
	childNodePtrs := make([]*commonModels.MetaNode, 0, len(childNodes))
	for i := range childNodes {
		childNodePtrs = append(childNodePtrs, &childNodes[i])
	}
	children = append(children, s.treeBuilder.ConvertMetaNodes(engine, childNodePtrs)...)

	itemNodes := s.treeBuilder.ConvertMetaItemsForEngine(engine.EngineType, items)
	children = append(children, s.treeBuilder.ConvertMetaNodes(engine, itemNodes)...)

	parent := s.treeBuilder.ConvertNodeToTree(loc, map[string]interface{}{
		"engine_id":   engine.ID,
		"engine_type": engine.EngineType,
		"full_name":   loc.FullName(),
		"node_id":     *loc.NodeID,
	})
	parent.Children = children
	parent.HasChildren = len(children) > 0

	logger.L().Info("获取节点子节点成功",
		"engine_id", engineID,
		"locator", locatorURI,
		"node_id", *loc.NodeID,
		"expand_depth", expandDepth,
		"children_count", len(parent.Children))

	return parent, nil
}

func metadataExpandDepth(loc *resourcetree.ResourceLocator, expandDepth int) int {
	if expandDepth < 0 {
		return -1
	}
	// Locator.Path 不包含显式 catalog root，MetaNode.Depth 包含。
	if loc == nil {
		return 1 + expandDepth
	}
	return len(loc.Path) + 1 + expandDepth
}

// SearchNodes 搜索资源树节点
// 参数:
//   - ctx: 上下文
//   - tenantID: 租户 ID
//   - engineID: 引擎 ID
//   - keyword: 搜索关键词
//   - nodeTypes: 节点类型过滤（可选）
//   - limit: 返回数量限制
//
// 返回: 搜索结果列表和总数
func (s *ExplorerService) SearchNodes(ctx context.Context, tenantID *uint, engineID uint, keyword string, nodeTypes []string, limit int) ([]*resourcetree.TreeNode, int, error) {
	// 1. 验证引擎权限
	if s.systemClient == nil {
		return nil, 0, fmt.Errorf("system client not available")
	}

	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get engine from system: %w", err)
	}

	if tenantID != nil && engine.TenantID != nil && *engine.TenantID != *tenantID {
		return nil, 0, ErrEngineAccessDenied
	}

	// 2. 从 Meta 获取所有节点（不限制深度）
	metaNodes, err := s.getMetaNodes(ctx, engineID, engine.EngineType, -1, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get meta nodes: %w", err)
	}

	// 3. 使用 TreeBuilder 构建完整树
	tree, err := s.treeBuilder.BuildFromMeta(engine, metaNodes, -1)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build tree: %w", err)
	}

	// 4. 在树中搜索节点（简单实现：遍历所有节点）
	// TODO: 使用专门的 TreeSearchService 实现更高效的搜索
	results := make([]*resourcetree.TreeNode, 0)
	s.searchInTree(tree, keyword, nodeTypes, &results)

	// 5. 限制返回数量
	total := len(results)
	if limit > 0 && limit < total {
		results = results[:limit]
	}

	logger.L().Info("搜索节点成功",
		"engine_id", engineID,
		"keyword", keyword,
		"total", total,
		"returned", len(results))

	return results, total, nil
}

// searchInTree 在树中递归搜索节点
func (s *ExplorerService) searchInTree(node *resourcetree.TreeNode, keyword string, nodeTypes []string, results *[]*resourcetree.TreeNode) {
	// 检查节点类型过滤
	typeMatch := len(nodeTypes) == 0
	if !typeMatch {
		for _, nt := range nodeTypes {
			if node.Type == nt {
				typeMatch = true
				break
			}
		}
	}

	// 检查关键词匹配（忽略大小写）
	keywordLower := strings.ToLower(keyword)
	labelMatch := strings.Contains(strings.ToLower(node.Label), keywordLower)

	if typeMatch && labelMatch {
		*results = append(*results, node)
	}

	// 递归搜索子节点
	for _, child := range node.Children {
		s.searchInTree(child, keyword, nodeTypes, results)
	}
}

// 内部辅助方法

// getMetaNodes 从 Meta 模块获取节点列表
func (s *ExplorerService) getMetaNodes(ctx context.Context, engineID uint, engineType string, expandDepth int, tenantID *uint) ([]*commonModels.MetaNode, error) {
	if s.metaClient == nil {
		return nil, fmt.Errorf("meta client not available")
	}

	// 设置 tenant_id（用于服务间调用时的租户隔离）
	s.metaClient.SetTenantID(tenantID)

	// 调用 MetaClient 获取树
	tree, err := s.metaClient.GetMetadataTree(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata tree: %w", err)
	}

	// 合并 TopNodes、ChildNodes
	// 注意：根据 expandDepth 决定是否包含 Items
	allNodes := make([]*commonModels.MetaNode, 0, len(tree.TopNodes)+len(tree.ChildNodes)+len(tree.Items))

	// 添加顶层节点（数据库/Schema/Bucket）
	for i := range tree.TopNodes {
		node := tree.TopNodes[i]
		allNodes = append(allNodes, &node)
	}

	// 添加子节点（中间层容器），根据 expandDepth 过滤
	for i := range tree.ChildNodes {
		node := tree.ChildNodes[i]
		// 如果设置了 expandDepth，只包含深度 <= expandDepth 的节点
		// -1 表示不限制深度
		if expandDepth == -1 || node.Depth <= expandDepth {
			allNodes = append(allNodes, &node)
		}
	}

	// 将 Items 转换为 MetaNode（叶子节点：表/集合/对象）
	// 只有当 expandDepth == -1 或者足够大时才包含 Items
	// 修正阈值：数据库表在 depth=2，对象存储对象在 depth=3
	includeItems := expandDepth == -1 || expandDepth >= 2
	if includeItems {
		itemNodes := s.treeBuilder.ConvertMetaItemsForEngine(engineType, tree.Items)
		for _, node := range itemNodes {
			if expandDepth != -1 && node.Depth > expandDepth {
				continue
			}
			allNodes = append(allNodes, node)
		}
	}

	logger.L().Info("从 Meta 获取节点成功",
		"engine_id", engineID,
		"expand_depth", expandDepth,
		"top_nodes", len(tree.TopNodes),
		"child_nodes", len(tree.ChildNodes),
		"items", len(tree.Items),
		"filtered_nodes", len(allNodes))

	return allNodes, nil
}

// buildEngineRootNode 构建引擎根节点（降级方案）
func (s *ExplorerService) buildEngineRootNode(engine *commonModels.Engine) *resourcetree.TreeNode {
	rootType := resourcetree.CatalogRootResourceType(engine)
	locator := resourcetree.EngineRootLocatorForType(engine.ID, rootType)
	return &resourcetree.TreeNode{
		ID:      locator,
		Locator: locator,
		Label:   engine.Name,
		Type:    string(rootType),
		Icon:    resourcetree.EngineIcon(engine),
		Metadata: map[string]interface{}{
			"engine_id":      engine.ID,
			"engine_type":    engine.EngineType,
			"capabilities":   engine.Capabilities,
			"meta_available": false,
		},
		Children: []*resourcetree.TreeNode{},
	}
}

// convertManagerEngineToCommon 将 Manager 的 Engine 转换为 Common 的 Engine
func convertManagerEngineToCommon(managerEngine *models.Engine) *commonModels.Engine {
	if managerEngine == nil {
		return nil
	}

	// 将 manager.ConnectionInfo 转换为 commonModels.ConnectionInfo
	commonConnInfo := make(commonModels.ConnectionInfo)
	for k, v := range managerEngine.ConnectionInfo {
		commonConnInfo[k] = v
	}

	return &commonModels.Engine{
		ID:             managerEngine.ID,
		TenantID:       managerEngine.TenantID,
		Name:           managerEngine.Name,
		EngineType:     managerEngine.EngineType,
		ConnectionInfo: commonConnInfo,
		Description:    managerEngine.Description,
		IsActive:       managerEngine.IsActive,
		CreatedBy:      managerEngine.CreatedBy,
		CreatedAt:      managerEngine.CreatedAt,
		UpdatedAt:      managerEngine.UpdatedAt,
		// Common 的 Engine 有额外字段，使用默认值
		EngineOrigin:     "general",
		ScanConfig:       nil,       // Manager 不维护扫描配置
		ConnectionStatus: "unknown", // 默认未知状态
		LastCheckAt:      nil,
		CheckMessage:     "",
	}
}
