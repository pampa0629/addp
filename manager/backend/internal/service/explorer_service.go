package service

import (
	"context"
	"fmt"
	"strings"

	commonClient "github.com/addp/common/client"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resource"
	commonUtils "github.com/addp/common/utils"
	"github.com/addp/manager/internal/models"
)

// ExplorerService 数据探查服务
// 作为协调层，负责：
// 1. 调用 TreeBuilder 构建资源树（使用 Common 模块的通用能力）
// 2. 调用 PreviewResolver 提供数据预览
// 3. 管理节点刷新
type ExplorerService struct {
	systemClient    *commonClient.SystemClient
	metaClient      *commonClient.MetaClient
	treeBuilder     *resource.TreeBuilder
	previewResolver *PreviewResolver
}

// NewExplorerService 创建数据探查服务
func NewExplorerService(
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
	previewResolver *PreviewResolver,
) *ExplorerService {
	// 创建 TreeBuilder（使用 metaClient 作为可选依赖）
	treeBuilder := resource.NewTreeBuilder(&metaClientAdapter{client: metaClient})

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
func (s *ExplorerService) GetTree(ctx context.Context, tenantID *uint, engineID uint, expandDepth int) (*resource.TreeNode, error) {
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
	metaNodes, err := s.getMetaNodes(ctx, engineID, engine.EngineType, expandDepth, tenantID)
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

	// 3. 触发 Meta 扫描（异步）
	if s.metaClient != nil {
		// 设置租户 ID（用于服务间调用时的租户隔离）
		s.metaClient.SetTenantID(tenantID)

		// 根据节点类型决定扫描范围
		var namespaces []string

		switch loc.Type {
		case resource.TypeTable:
			// 表节点：扫描该表所在的命名空间
			if len(loc.Path) > 0 {
				namespaces = []string{loc.Path[0]}
				logger.L().Info("触发命名空间级扫描（表节点）", "engine_id", loc.EngineID, "namespace", loc.Path[0])
			}
		case resource.TypeSchema:
			// Schema/namespace 节点：扫描该命名空间
			if len(loc.Path) > 0 {
				namespaces = []string{loc.Path[0]}
				logger.L().Info("触发命名空间级扫描（Schema 节点）", "engine_id", loc.EngineID, "namespace", loc.Path[0])
			}
		default:
			// 其他类型（数据库、引擎根节点、对象存储等）：扫描整个引擎
			logger.L().Info("触发引擎级扫描", "engine_id", loc.EngineID, "type", loc.Type)
		}

		// 调用 Meta 扫描 API
		if err := s.metaClient.TriggerScanEngine(loc.EngineID, namespaces); err != nil {
			// 扫描失败不中断流程，只记录警告
			logger.L().Warn("触发 Meta 扫描失败", "error", err)
		} else {
			logger.L().Info("Meta 扫描已触发", "engine_id", loc.EngineID, "namespaces", namespaces)
		}
	}

	// 4. 重新获取节点信息
	// 使用 -1 获取所有节点，确保能找到目标节点
	metaNodes, err := s.getMetaNodes(ctx, loc.EngineID, engine.EngineType, -1, tenantID)
	if err != nil {
		logger.L().Warn("刷新节点失败", "locator", locatorURI, "error", err)
		return nil, fmt.Errorf("failed to refresh node: %w", err)
	}

	// 5. 查找对应的节点
	// 如果是 engine 根节点（没有 meta_id），返回整个树
	if loc.MetaID == nil {
		logger.L().Info("刷新引擎根节点", "engine_id", loc.EngineID)
		// 构建完整的树结构
		tree, err := s.treeBuilder.BuildFromMeta(engine, metaNodes, 2)
		if err != nil {
			return nil, fmt.Errorf("failed to build engine tree: %w", err)
		}
		return tree, nil
	}

	// 查找具体的节点（有 meta_id）
	for _, node := range metaNodes {
		if node.ID == *loc.MetaID {
			// 构建树节点并返回（只构建该节点及其直接子节点）
			tree, err := s.treeBuilder.BuildFromMeta(engine, []*commonModels.MetaNode{node}, 1)
			if err != nil {
				return nil, fmt.Errorf("failed to build node tree: %w", err)
			}
			return tree, nil
		}
	}

	return nil, fmt.Errorf("node not found: %s", locatorURI)
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
//   - expandDepth: 展开深度（1=直接子节点，-1=全部展开）
//
// 返回: 包含父节点 locator 和子节点列表的结构
func (s *ExplorerService) GetNodeChildren(ctx context.Context, tenantID *uint, engineID uint, locatorURI string, expandDepth int) (*resource.TreeNode, error) {
	// 1. 解析 Locator
	loc, err := resource.ParseURI(locatorURI)
	if err != nil {
		return nil, fmt.Errorf("invalid locator: %w", err)
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

	// 3. 从 Meta 获取元数据节点
	// 这里我们获取所有节点，然后在内存中过滤出目标节点的子节点
	// TODO: 优化为直接从 Meta 获取指定节点的子节点
	nodeDepth := len(loc.Path) // 节点深度 = 路径段数
	metaNodes, err := s.getMetaNodes(ctx, engineID, engine.EngineType, expandDepth+nodeDepth, tenantID)
	if err != nil {
		logger.L().Warn("获取 Meta 节点失败", "locator", locatorURI, "error", err)
		return nil, fmt.Errorf("failed to get meta nodes: %w", err)
	}

	// 4. 使用 TreeBuilder 构建完整树
	tree, err := s.treeBuilder.BuildFromMeta(engine, metaNodes, expandDepth+nodeDepth)
	if err != nil {
		return nil, fmt.Errorf("failed to build tree: %w", err)
	}

	// 5. 在树中查找目标节点
	targetNode := s.findNodeByLocator(tree, locatorURI)
	if targetNode == nil {
		return nil, fmt.Errorf("node not found: %s", locatorURI)
	}

	logger.L().Info("获取节点子节点成功",
		"engine_id", engineID,
		"locator", locatorURI,
		"children_count", len(targetNode.Children))

	return targetNode, nil
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
func (s *ExplorerService) SearchNodes(ctx context.Context, tenantID *uint, engineID uint, keyword string, nodeTypes []string, limit int) ([]*resource.TreeNode, int, error) {
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
	results := make([]*resource.TreeNode, 0)
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

// findNodeByLocator 在树中查找指定 locator 的节点（递归）
func (s *ExplorerService) findNodeByLocator(node *resource.TreeNode, locator string) *resource.TreeNode {
	if node.Locator == locator {
		return node
	}

	for _, child := range node.Children {
		if found := s.findNodeByLocator(child, locator); found != nil {
			return found
		}
	}

	return nil
}

// searchInTree 在树中递归搜索节点
func (s *ExplorerService) searchInTree(node *resource.TreeNode, keyword string, nodeTypes []string, results *[]*resource.TreeNode) {
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
		for i := range tree.Items {
			item := &tree.Items[i]

			// 动态计算 Depth：根据 full_name 中的分隔符数量
			// - 对象存储: bucket/dir/file → depth=3
			// - 数据库表: schema.table → depth=2
			depth := calculateItemDepthByEngineType(engineType, item.ItemType, item.FullName, item.Attributes)

			// 再次检查深度
			if expandDepth != -1 && depth > expandDepth {
				continue
			}

			// 将 MetaItem 转换为 MetaNode
			// 为了避免 ID 冲突，使用虚拟 ID: 使用 node_id 作为实际的"虚拟"标识
			// 这样所有 Items 的虚拟 ID 都会是其 parent_node_id (1-100 之间的某个值)
			// 但为了保证唯一性,我们使用一个特殊的转换方法:
			// 对于 Items,使用 meta_item.ID + 100000 作为虚拟 ID
			virtualID := item.ID + 100000

			node := &commonModels.MetaNode{
				ID:             virtualID, // 使用虚拟 ID 避免与 meta_node 冲突
				TenantID:       item.TenantID,
				EngineID:       item.EngineID,
				ParentNodeID:   &item.NodeID, // Item 的 NodeID 是其父节点
				NodeType:       item.ItemType,
				Name:           item.Name,
				FullName:       item.FullName,
				Depth:          depth, // 动态计算深度，不再硬编码
				Path:           item.FullName,
				ScanStatus:     "completed",
				ItemCount:      0, // 叶子节点没有子树节点（RowCount 是数据行数，不是子节点数）
				TotalSizeBytes: 0,
				Attributes:     item.Attributes,
			}
			if item.SizeBytes != nil {
				node.TotalSizeBytes = *item.SizeBytes
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

// GraphSchemaResponse 图数据库 Schema 响应
type GraphSchemaResponse struct {
	NodeLabels    []NodeLabelSchema    `json:"nodes"`
	Relationships []RelationshipSchema `json:"relationships"`
}

// NodeLabelSchema 节点标签描述
type NodeLabelSchema struct {
	Label      string   `json:"label"`
	Count      int64    `json:"count"`
	Properties []string `json:"properties"`
}

// RelationshipSchema 关系类型描述
type RelationshipSchema struct {
	Type       string   `json:"type"`
	Count      int64    `json:"count"`
	FromLabels []string `json:"from_labels"`
	ToLabels   []string `json:"to_labels"`
}

// GetGraphSchema 获取图数据库的 Schema 结构（节点标签 + 关系类型）
// 数据来自 Meta 模块的扫描结果，不重新连接数据库
func (s *ExplorerService) GetGraphSchema(ctx context.Context, tenantID *uint, engineID uint, database string) (*GraphSchemaResponse, error) {
	if s.metaClient == nil {
		return nil, fmt.Errorf("meta client not available")
	}

	// 权限校验
	if s.systemClient != nil {
		engine, err := s.systemClient.GetEngine(engineID)
		if err != nil {
			return nil, fmt.Errorf("failed to get engine: %w", err)
		}
		if tenantID != nil && engine.TenantID != nil && *engine.TenantID != *tenantID {
			return nil, ErrEngineAccessDenied
		}
	}

	s.metaClient.SetTenantID(tenantID)

	// 获取元数据树（包含节点和 items）
	tree, err := s.metaClient.GetMetadataTree(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata tree: %w", err)
	}

	// 找到目标数据库节点
	var dbNodeID uint
	for _, node := range tree.TopNodes {
		if strings.EqualFold(node.Name, database) || database == "" {
			dbNodeID = node.ID
			break
		}
	}
	// 如果 TopNodes 没找到，再从 ChildNodes 里找
	if dbNodeID == 0 {
		for _, node := range tree.ChildNodes {
			if strings.EqualFold(node.Name, database) {
				dbNodeID = node.ID
				break
			}
		}
	}
	// 如果仍未找到（但 database 为空），使用第一个节点
	if dbNodeID == 0 && database == "" && len(tree.TopNodes) > 0 {
		dbNodeID = tree.TopNodes[0].ID
	}
	if dbNodeID == 0 {
		return nil, fmt.Errorf("database node not found: %s", database)
	}

	// 从 Items 中过滤出对应数据库的 label 和 relationship
	resp := &GraphSchemaResponse{
		NodeLabels:    []NodeLabelSchema{},
		Relationships: []RelationshipSchema{},
	}

	for _, item := range tree.Items {
		if item.NodeID != dbNodeID {
			continue
		}
		switch item.ItemType {
		case "label":
			label := NodeLabelSchema{
				Label:      item.Name,
				Properties: extractStringSliceFromAttrs(item.Attributes, "fields"),
			}
			if item.RowCount != nil {
				label.Count = *item.RowCount
			} else {
				label.Count = int64AttributeFromAttrs(item.Attributes, "document_count")
			}
			resp.NodeLabels = append(resp.NodeLabels, label)
		case "relationship":
			rel := RelationshipSchema{
				Type:       item.Name,
				FromLabels: extractStringSliceFromAttrs(item.Attributes, "from_labels"),
				ToLabels:   extractStringSliceFromAttrs(item.Attributes, "to_labels"),
			}
			if item.RowCount != nil {
				rel.Count = *item.RowCount
			} else {
				rel.Count = int64AttributeFromAttrs(item.Attributes, "count")
			}
			resp.Relationships = append(resp.Relationships, rel)
		}
	}

	return resp, nil
}

// extractStringSliceFromAttrs 从 attributes map 中提取字符串切片
func extractStringSliceFromAttrs(attrs map[string]interface{}, key string) []string {
	if attrs == nil {
		return []string{}
	}
	if sectionAttrs := explorerAttributeSection(attrs, "type_info.table"); sectionAttrs != nil {
		if values := interfaceToExplorerStringSlice(sectionAttrs[key]); len(values) > 0 {
			return values
		}
	}
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return []string{}
	}
	return interfaceToExplorerStringSlice(raw)
}

func interfaceToExplorerStringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return []string{}
}

func int64AttributeFromAttrs(attrs map[string]interface{}, key string) int64 {
	if attrs == nil {
		return 0
	}
	if sectionAttrs := explorerAttributeSection(attrs, "type_info.table"); sectionAttrs != nil {
		if value := toInt64(sectionAttrs[key]); value > 0 {
			return value
		}
	}
	return toInt64(attrs[key])
}

func explorerAttributeSection(attrs map[string]interface{}, section string) map[string]interface{} {
	current := attrs
	for _, part := range strings.Split(section, ".") {
		raw, ok := current[part]
		if !ok {
			return nil
		}
		next, ok := raw.(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

// toInt64 将 interface{} 转为 int64
func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int32:
		return int64(n)
	}
	return 0
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

// calculateItemDepth 动态计算 Item 的深度
// 根据 ItemType 和 FullName 计算节点在树中的深度
func calculateItemDepth(itemType, fullName string, attributes commonModels.JSONMap) int {
	return calculateItemDepthByEngineType("", itemType, fullName, attributes)
}

func calculateItemDepthByEngineType(engineType, itemType, fullName string, attributes commonModels.JSONMap) int {
	// 对象存储类型（object）：根据路径中的斜杠数量计算
	// 例如: "addp/image/file.jpg" → depth=3 (bucket=1, directory=2, object=3)
	if itemType == "object" || itemType == "lake_table" || isPathSemanticMetaItem(itemType, fullName, attributes, engineType) {
		segments := strings.Split(strings.Trim(fullName, "/"), "/")
		return len(segments)
	}

	// 数据库表类型（table）：根据点号数量计算
	// 例如: "public.users" → depth=2 (schema=1, table=2)
	if itemType == "table" {
		segments := strings.Split(fullName, ".")
		return len(segments)
	}

	// MongoDB 集合类型（collection）：通常是 database.collection
	if itemType == "collection" || itemType == "label" || itemType == "relationship" {
		segments := strings.Split(fullName, ".")
		return len(segments)
	}

	// 默认深度为 2（大多数情况下 Items 在第二层）
	return 2
}

func isPathSemanticMetaItem(itemType, fullName string, attributes commonModels.JSONMap, engineType string) bool {
	if itemType != "table" {
		return false
	}
	if strings.Contains(fullName, "/") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "minio", "s3", "nfs", "nas":
		return true
	}
	formatName := strings.ToLower(strings.TrimSpace(commonJSON.StringFromSections(attributes, "format", "item")))
	organization := strings.ToLower(strings.TrimSpace(commonJSON.StringFromSections(attributes, "organization", "item")))
	return formatName != "" && organization != ""
}

// metaClientAdapter 适配器：将 MetaClient 适配为 TreeBuilder 的 MetaClient 接口
type metaClientAdapter struct {
	client *commonClient.MetaClient
}

func (a *metaClientAdapter) GetMetadataTree(engineID uint) (*commonModels.MetadataTree, error) {
	if a.client == nil {
		return nil, fmt.Errorf("meta client not initialized")
	}
	return a.client.GetMetadataTree(engineID)
}

func (a *metaClientAdapter) GetNodeByPath(engineID uint, nodePath string) (*commonModels.MetaNode, error) {
	if a.client == nil {
		return nil, fmt.Errorf("meta client not initialized")
	}
	return a.client.GetNodeByPath(engineID, nodePath)
}

func (a *metaClientAdapter) GetNodeChildren(nodeID uint) ([]commonModels.MetaNode, error) {
	if a.client == nil {
		return nil, fmt.Errorf("meta client not initialized")
	}
	return a.client.GetNodeChildren(nodeID)
}

func (a *metaClientAdapter) GetNodeItems(nodeID uint) ([]commonModels.MetaItem, error) {
	if a.client == nil {
		return nil, fmt.Errorf("meta client not initialized")
	}
	return a.client.GetNodeItems(nodeID)
}

func (a *metaClientAdapter) GetMetaNode(nodeID uint) (*commonModels.MetaNode, error) {
	if a.client == nil {
		return nil, fmt.Errorf("meta client not initialized")
	}
	return a.client.GetMetaNode(nodeID)
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
		EngineCategory:   "standard", // 默认为标准引擎
		ScanConfig:       nil,        // Manager 不维护扫描配置
		ConnectionStatus: "unknown",  // 默认未知状态
		LastCheckAt:      nil,
		CheckMessage:     "",
	}
}
