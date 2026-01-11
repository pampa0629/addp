package service

import (
	"context"
	"fmt"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

// ExplorerService 数据探查服务
// 作为协调层，负责：
// 1. 调用 TreeBuilder 构建资源树（使用 Common 模块的通用能力）
// 2. 调用 PreviewResolver 提供数据预览
// 3. 管理节点刷新
type ExplorerService struct {
	engineRepo   *repository.EngineRepository
	metaClient   *commonClient.MetaClient
	treeBuilder  *resource.TreeBuilder
	previewResolver *PreviewResolver
}

// NewExplorerService 创建数据探查服务
func NewExplorerService(
	engineRepo *repository.EngineRepository,
	metaClient *commonClient.MetaClient,
	previewResolver *PreviewResolver,
) *ExplorerService {
	// 创建 TreeBuilder（使用 metaClient 作为可选依赖）
	treeBuilder := resource.NewTreeBuilder(&metaClientAdapter{client: metaClient})

	return &ExplorerService{
		engineRepo:      engineRepo,
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
func (s *ExplorerService) GetTree(ctx context.Context, tenantID *uint, engineID uint, expandDepth int) (*resource.TreeNode, error) {
	// 1. 获取引擎信息
	managerEngine, err := s.engineRepo.GetByID(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 验证租户权限
	if tenantID != nil && managerEngine.TenantID != nil && *managerEngine.TenantID != *tenantID {
		return nil, ErrEngineAccessDenied
	}

	// 转换为 commonModels.Engine
	engine := convertManagerEngineToCommon(managerEngine)

	// 2. 尝试从 Meta 获取资源树（可选，失败不影响功能）
	metaNodes, err := s.getMetaNodes(ctx, engineID, expandDepth, tenantID)
	if err != nil {
		logger.L().Warn("获取 Meta 节点失败，使用降级方案", "engine_id", engineID, "error", err)
		// Meta 不可用时的降级方案：返回引擎根节点
		return s.buildEngineRootNode(engine), nil
	}

	// 3. 使用 TreeBuilder 构建树
	tree, err := s.treeBuilder.BuildFromMeta(engine, metaNodes, expandDepth)
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
// 返回: 刷新后的节点信息
func (s *ExplorerService) RefreshNode(ctx context.Context, tenantID *uint, locatorURI string) (*resource.TreeNode, error) {
	// 1. 解析 Locator
	loc, err := resource.ParseURI(locatorURI)
	if err != nil {
		return nil, fmt.Errorf("invalid locator: %w", err)
	}

	// 2. 验证引擎权限
	engine, err := s.engineRepo.GetByID(loc.EngineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}
	if tenantID != nil && engine.TenantID != nil && *engine.TenantID != *tenantID {
		return nil, ErrEngineAccessDenied
	}

	// 3. 触发 Meta 扫描（异步）
	if s.metaClient != nil {
		// TODO: 调用 Meta 的扫描接口刷新节点
		logger.L().Info("触发 Meta 扫描", "locator", locatorURI)
	}

	// 4. 重新获取节点信息
	metaNodes, err := s.getMetaNodes(ctx, loc.EngineID, 1, tenantID) // 只获取当前节点的子节点
	if err != nil {
		logger.L().Warn("刷新节点失败", "locator", locatorURI, "error", err)
		return nil, fmt.Errorf("failed to refresh node: %w", err)
	}

	// 5. 查找对应的节点
	for _, node := range metaNodes {
		if node.ID == *loc.MetaID {
			// 构建树节点并返回
			commonEngine := convertManagerEngineToCommon(engine)
			tree, err := s.treeBuilder.BuildFromMeta(commonEngine, []*commonModels.MetaNode{node}, 1)
			if err != nil {
				return nil, fmt.Errorf("failed to build node tree: %w", err)
			}
			return tree, nil
		}
	}

	return nil, fmt.Errorf("node not found: %s", locatorURI)
}

// GetEngineList 获取可用于探查的引擎列表
func (s *ExplorerService) GetEngineList(tenantID *uint) ([]*commonModels.Engine, error) {
	// 使用 ListAllActive 获取引擎列表
	engines, err := s.engineRepo.ListAllActive(tenantID)
	if err != nil {
		return nil, err
	}

	// 转换为 commonModels.Engine 切片
	commonEngines := make([]*commonModels.Engine, len(engines))
	for i := range engines {
		commonEngines[i] = convertManagerEngineToCommon(&engines[i])
	}

	return commonEngines, nil
}

// 内部辅助方法

// getMetaNodes 从 Meta 模块获取节点列表
func (s *ExplorerService) getMetaNodes(ctx context.Context, engineID uint, expandDepth int, tenantID *uint) ([]*commonModels.MetaNode, error) {
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

	// 合并 TopNodes、ChildNodes 和 Items（转换为 MetaNode）
	allNodes := make([]*commonModels.MetaNode, 0, len(tree.TopNodes)+len(tree.ChildNodes)+len(tree.Items))

	// 添加顶层节点（数据库/Schema/Bucket）
	// 创建新实例以避免指针共享导致的循环引用
	for i := range tree.TopNodes {
		node := tree.TopNodes[i]
		allNodes = append(allNodes, &node)
	}

	// 添加子节点（中间层容器）
	// 创建新实例以避免指针共享导致的循环引用
	for i := range tree.ChildNodes {
		node := tree.ChildNodes[i]
		allNodes = append(allNodes, &node)
	}

	// 将 Items 转换为 MetaNode（叶子节点：表/集合/对象）
	for i := range tree.Items {
		item := &tree.Items[i]
		// 将 MetaItem 转换为 MetaNode
		node := &commonModels.MetaNode{
			ID:             item.ID,
			TenantID:       item.TenantID,
			EngineID:       item.EngineID,
			ParentNodeID:   &item.NodeID, // Item 的 NodeID 是其父节点
			NodeType:       item.ItemType,
			Name:           item.Name,
			FullName:       item.FullName,
			Depth:          2, // Items 通常是第二层（database → collection）
			Path:           item.FullName,
			ScanStatus:     "completed",
			ItemCount:      0, // Items 没有子项
			TotalSizeBytes: 0,
			Attributes:     item.Attributes,
		}
		if item.RowCount != nil {
			node.ItemCount = int(*item.RowCount)
		}
		if item.SizeBytes != nil {
			node.TotalSizeBytes = *item.SizeBytes
		}
		allNodes = append(allNodes, node)
	}

	logger.L().Info("从 Meta 获取节点成功",
		"engine_id", engineID,
		"top_nodes", len(tree.TopNodes),
		"child_nodes", len(tree.ChildNodes),
		"items", len(tree.Items),
		"total", len(allNodes))

	return allNodes, nil
}

// buildEngineRootNode 构建引擎根节点（降级方案）
func (s *ExplorerService) buildEngineRootNode(engine *commonModels.Engine) *resource.TreeNode {
	locator := fmt.Sprintf("addp://engine/%d/path/?type=database", engine.ID)
	return &resource.TreeNode{
		ID:      locator,
		Locator: locator,
		Label:   engine.Name,
		Type:    "engine",
		Icon:    getEngineIcon(engine.EngineType),
		Metadata: map[string]interface{}{
			"engine_id":      engine.ID,
			"engine_type":    engine.EngineType,
			"meta_available": false,
		},
		Children: []*resource.TreeNode{},
	}
}

func getEngineIcon(engineType string) string {
	// 复用 TreeBuilder 的逻辑（这里简化实现）
	icons := map[string]string{
		"postgresql": "Database",
		"mysql":      "Database",
		"mongodb":    "DocumentText",
		"minio":      "FolderOpen",
	}
	if icon, ok := icons[engineType]; ok {
		return icon
	}
	return "Database"
}

// metaClientAdapter 适配器：将 MetaClient 适配为 TreeBuilder 的 MetaClient 接口
type metaClientAdapter struct {
	client *commonClient.MetaClient
}

func (a *metaClientAdapter) GetNodeByID(nodeID uint) (*commonModels.MetaNode, error) {
	// TODO: 实现从 MetaClient 获取节点
	return nil, fmt.Errorf("not implemented")
}

func (a *metaClientAdapter) GetTree(engineID uint, expandDepth int) ([]*commonModels.MetaNode, error) {
	// TODO: 实现从 MetaClient 获取树
	return nil, fmt.Errorf("not implemented")
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
		EngineCategory:   "standard",           // 默认为标准引擎
		ScanConfig:       nil,                  // Manager 不维护扫描配置
		ConnectionStatus: "unknown",            // 默认未知状态
		LastCheckAt:      nil,
		CheckMessage:     "",
	}
}
