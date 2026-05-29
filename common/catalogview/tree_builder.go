package catalogview

import (
	"encoding/json"
	"strings"
	"time"

	enginePlugin "github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/models"
)

// TreeNode 统一的树节点结构
// 用于在各模块间传递资源树数据（Manager、Meta、Service、Orchestrator）
type TreeNode struct {
	ID          string                 `json:"id"`          // 节点唯一标识（使用 locator 的值）
	Locator     string                 `json:"locator"`     // ResourceLocator URI (addp://engine/1/path/public/users?type=table)
	Label       string                 `json:"label"`       // 显示标签（节点名称）
	Type        string                 `json:"type"`        // 节点类型 (schema/table/bucket/directory/object)
	TypeLabel   string                 `json:"typeLabel"`   // 类型的 i18n key，如 "engine.term.schema"（前端查 i18n 字典展示）
	Icon        string                 `json:"icon"`        // 图标名称
	Metadata    map[string]interface{} `json:"metadata"`    // 元数据（node_id、item_id、item_count、scanned_at 等）
	Children    []*TreeNode            `json:"children"`    // 子节点
	HasChildren bool                   `json:"hasChildren"` // 是否有子节点（用于显示展开图标）
}

// TreeBuilder 资源树构建器
// 提供通用的树构建能力，可被多个模块复用（Manager、Meta、Service、Orchestrator）
type TreeBuilder struct {
	// 可选依赖
	metaClient MetaClient // 用于从 Meta 模块获取节点信息
}

// MetaClient Meta 模块客户端接口（可选依赖）
type MetaClient interface {
	// GetMetadataTree 获取引擎的完整元数据树
	GetMetadataTree(engineID uint) (*models.MetadataTree, error)

	// GetNodeByCatalogPath 按 catalog path 查询节点。
	GetNodeByCatalogPath(engineID uint, catalogPath string) (*models.MetaNode, error)

	// GetItemByCatalogPath 按 catalog path 查询数据项。
	GetItemByCatalogPath(engineID uint, catalogPath string) (*models.MetaItem, error)

	// GetNodeChildren 获取节点的子节点
	GetNodeChildren(nodeID uint) ([]models.MetaNode, error)

	// GetNodeItems 获取节点下的项目
	GetNodeItems(nodeID uint) ([]models.MetaItem, error)

	// GetMetaNode 获取单个节点详情
	GetMetaNode(nodeID uint) (*models.MetaNode, error)
}

// CustomTreeBuilder 自定义树构建器接口（用于降级方案）
type CustomTreeBuilder interface {
	// Build 自定义构建逻辑
	Build(engine *models.Engine) (*TreeNode, error)
}

// NewTreeBuilder 创建树构建器
func NewTreeBuilder(metaClient MetaClient) *TreeBuilder {
	return &TreeBuilder{
		metaClient: metaClient,
	}
}

// BuildFromMetadataTree 从 MetadataTree 构建树
// 适用场景：Service 模块需要将 Meta API 返回的 MetadataTree 转换为前端期待的树结构
//
// 参数:
//   - engine: 引擎信息
//   - metadataTree: Meta API 返回的元数据树
//
// 返回: 树的根节点
func (b *TreeBuilder) BuildFromMetadataTree(engine *models.Engine, metadataTree *models.MetadataTree) (*TreeNode, error) {
	// 合并 TopNodes 和 ChildNodes
	allNodes := make([]*models.MetaNode, 0, len(metadataTree.TopNodes)+len(metadataTree.ChildNodes))
	for i := range metadataTree.TopNodes {
		allNodes = append(allNodes, &metadataTree.TopNodes[i])
	}
	for i := range metadataTree.ChildNodes {
		allNodes = append(allNodes, &metadataTree.ChildNodes[i])
	}
	itemNodes := b.ConvertMetaItems(metadataTree.Items)
	allNodes = append(allNodes, itemNodes...)

	// 使用现有的 BuildFromMeta 方法
	return b.BuildFromMeta(engine, allNodes, -1)
}

// BuildFromMeta 从 Meta 模块的节点构建树
// 适用场景：Manager、Meta、Service 模块
//
// 参数:
//   - engine: 引擎信息
//   - metaNodes: Meta 模块的节点列表（已按层级组织）
//   - expandDepth: 展开深度（-1 表示全部展开）
//
// 返回: 树的根节点
func (b *TreeBuilder) BuildFromMeta(engine *models.Engine, metaNodes []*models.MetaNode, expandDepth int) (*TreeNode, error) {
	// 创建引擎根节点
	rootLocator := buildEngineRootLocator(engine.ID)

	// 引擎根节点的 Children 处理：
	// - 如果有 metaNodes，初始化为空数组（后续会填充）
	// - 如果没有 metaNodes，设置为空数组表示确实没有子节点
	hasChildren := len(metaNodes) > 0
	children := []*TreeNode{} // 引擎根节点在构建时会立即填充子节点，所以这里初始化为空数组

	root := &TreeNode{
		ID:        rootLocator,
		Locator:   rootLocator,
		Label:     engine.Name,
		Type:      "engine",
		TypeLabel: "engine.term.engine",
		Icon:      EngineIcon(engine),
		Metadata: map[string]interface{}{
			"engine_id":     engine.ID,
			"engine_type":   engine.EngineType,
			"engine_family": engineFamily(engine),
			"capabilities":  engine.Capabilities,
		},
		Children:    children,
		HasChildren: hasChildren,
	}

	// 构建节点 ID 到 TreeNode 的映射
	nodeMap := make(map[uint]*TreeNode)
	skipNodeIDs, parentOverrides := wholeScopePresentationOverrides(metaNodes)

	// 第一遍：创建所有节点
	for _, node := range metaNodes {
		if skipNodeIDs[node.ID] {
			continue
		}
		treeNode := b.convertMetaNode(engine, node)
		nodeMap[node.ID] = treeNode
	}

	// 第二遍：建立父子关系
	for _, node := range metaNodes {
		if skipNodeIDs[node.ID] {
			continue
		}
		treeNode := nodeMap[node.ID]

		// root 节点透明化：仅对 NFS/NAS 生效（挂载点不显示为独立层级）
		// 对象存储（minio/s3）的 root 节点不透明化，保留其层级
		isNFSRoot := node.NodeType == "root" && isNFSEngine(engine.EngineType)
		if isNFSRoot {
			continue
		}

		parentNodeID := node.ParentNodeID
		if override, ok := parentOverrides[node.ID]; ok {
			parentNodeID = override
		}

		if parentNodeID == nil || node.Depth == 1 {
			// 顶层节点，添加到引擎根节点
			root.Children = append(root.Children, treeNode)
		} else {
			// 子节点，添加到父节点的 children
			parentTreeNode, exists := nodeMap[*parentNodeID]
			if !exists {
				// 父节点不存在（可能是被透明化的 NFS root），直接挂到引擎根节点
				root.Children = append(root.Children, treeNode)
			} else if parentTreeNode.Type == "root" && isNFSEngine(engine.EngineType) {
				// 父节点是 NFS root（透明化），直接挂到引擎根节点
				root.Children = append(root.Children, treeNode)
			} else {
				parentTreeNode.Children = append(parentTreeNode.Children, treeNode)
			}
		}
	}

	return root, nil
}

func wholeScopePresentationOverrides(metaNodes []*models.MetaNode) (map[uint]bool, map[uint]*uint) {
	byID := make(map[uint]*models.MetaNode, len(metaNodes))
	for _, node := range metaNodes {
		if node != nil {
			byID[node.ID] = node
		}
	}
	skip := map[uint]bool{}
	parentOverrides := map[uint]*uint{}
	for _, node := range metaNodes {
		if node == nil || !isWholeScopeItemNode(node) || node.ParentNodeID == nil {
			continue
		}
		parent := byID[*node.ParentNodeID]
		if parent == nil || !sameResourceFullName(parent.FullName, node.FullName) || !isPathContainerNodeType(parent.NodeType) {
			continue
		}
		skip[parent.ID] = true
		parentOverrides[node.ID] = parent.ParentNodeID
	}
	return skip, parentOverrides
}

func isWholeScopeItemNode(node *models.MetaNode) bool {
	if node == nil {
		return false
	}
	layout := strings.ToLower(strings.TrimSpace(commonJSON.StringFromSections(node.Attributes, "layout", "item")))
	return layout == "whole"
}

func sameResourceFullName(left, right string) bool {
	return strings.Trim(strings.TrimSpace(left), "/") == strings.Trim(strings.TrimSpace(right), "/")
}

func isPathContainerNodeType(nodeType string) bool {
	switch strings.ToLower(strings.TrimSpace(nodeType)) {
	case "root", "dir", "directory", "prefix", "bucket":
		return true
	default:
		return false
	}
}

// ConvertMetaNodes 将 MetaNode 列表转换为 TreeNode 列表
// 适用场景：懒加载子节点时，将 Meta API 返回的节点转换为前端树节点
//
// 参数:
//   - engine: 引擎信息
//   - metaNodes: Meta 模块的节点列表
//
// 返回: 树节点列表
func (b *TreeBuilder) ConvertMetaNodes(engine *models.Engine, metaNodes []*models.MetaNode) []*TreeNode {
	treeNodes := make([]*TreeNode, 0, len(metaNodes))
	for _, node := range metaNodes {
		treeNode := b.convertMetaNode(engine, node)
		treeNodes = append(treeNodes, treeNode)
	}
	return treeNodes
}

// ConvertMetaItems 将 MetaItem 列表转换为 MetaNode 列表
// 适用场景：从 Meta API 获取叶子项目（表、对象等）后，转换为节点格式
//
// 参数:
//   - items: Meta 模块的 Item 列表
//
// 返回: MetaNode 列表。ID 仍保留树内虚拟 ID，locator 使用真实 item_id。
func (b *TreeBuilder) ConvertMetaItems(items []models.MetaItem) []*models.MetaNode {
	nodes := make([]*models.MetaNode, 0, len(items))

	for i := range items {
		item := &items[i]

		// 动态计算 Depth：根据 ItemType 和 FullName
		depth := calculateItemDepth(item.ItemType, item.FullName)

		// 使用虚拟 ID (item.ID + 100000) 避免与 meta_node 的 ID 冲突
		virtualID := item.ID + 100000

		node := &models.MetaNode{
			ID:             virtualID,
			TenantID:       item.TenantID,
			EngineID:       item.EngineID,
			ParentNodeID:   &item.NodeID,
			NodeType:       item.ItemType,
			Name:           item.Name,
			FullName:       item.FullName,
			Depth:          depth,
			Path:           item.FullName,
			ScanStatus:     "completed",
			ItemCount:      calculateItemCount(item.ItemType, item.Attributes),
			TotalSizeBytes: metaItemSizeBytes(item),
			Attributes:     item.Attributes,
		}
		node.Attributes = withMetaItemFacts(node.Attributes, item)
		if item.ScannedAt != nil {
			node.LastScanAt = item.ScannedAt
		}

		// 注意：
		// - 对于 table, collection, object 等叶子节点，ItemCount=0，不会显示展开箭头
		// - 对于 directory, prefix 等容器节点，ItemCount 从 attributes 中提取（可能为 0 或实际子对象数）
		// - hasChildren 字段由 shouldHaveChildren() 判断，容器类型即使 ItemCount=0 也会返回 true

		nodes = append(nodes, node)
	}

	return nodes
}

func withMetaItemFacts(attrs map[string]interface{}, item *models.MetaItem) map[string]interface{} {
	next := make(map[string]interface{}, len(attrs)+4)
	for key, value := range attrs {
		next[key] = value
	}
	next["item_id"] = item.ID
	next["is_meta_item"] = true
	if item.RowCount != nil {
		next["row_count"] = *item.RowCount
		if commonJSON.Int64(next, "type_info.table", "row_count") == 0 {
			upsertTreeMetadataSection(next, "type_info", "table", map[string]interface{}{"row_count": *item.RowCount})
		}
	}
	if item.SizeBytes != nil {
		next["size_bytes"] = *item.SizeBytes
	} else if item.ObjectSizeBytes != nil {
		next["object_size_bytes"] = *item.ObjectSizeBytes
	}
	return next
}

func metaItemIDFromNode(node *models.MetaNode) uint {
	if node == nil || node.Attributes == nil {
		return 0
	}
	switch value := node.Attributes["item_id"].(type) {
	case uint:
		return value
	case int:
		if value > 0 {
			return uint(value)
		}
	case int64:
		if value > 0 {
			return uint(value)
		}
	case float64:
		if value > 0 {
			return uint(value)
		}
	}
	return 0
}

func upsertTreeMetadataSection(attrs map[string]interface{}, section, namespace string, values map[string]interface{}) {
	sectionMap, _ := attrs[section].(map[string]interface{})
	if sectionMap == nil {
		sectionMap = map[string]interface{}{}
		attrs[section] = sectionMap
	}
	namespaceMap, _ := sectionMap[namespace].(map[string]interface{})
	if namespaceMap == nil {
		namespaceMap = map[string]interface{}{}
		sectionMap[namespace] = namespaceMap
	}
	for key, value := range values {
		if _, exists := namespaceMap[key]; !exists {
			namespaceMap[key] = value
		}
	}
}

func metaItemSizeBytes(item *models.MetaItem) int64 {
	if item == nil {
		return 0
	}
	if item.SizeBytes != nil {
		return *item.SizeBytes
	}
	if item.ObjectSizeBytes != nil {
		return *item.ObjectSizeBytes
	}
	return 0
}

// BuildFromEngine 从引擎直接构建树（不依赖 Meta）
// 适用场景：Meta 不可用时的降级方案
//
// 参数:
//   - engine: 引擎信息
//   - builder: 自定义构建器（由调用方提供引擎特定的构建逻辑）
//
// 返回: 树的根节点
func (b *TreeBuilder) BuildFromEngine(engine *models.Engine, builder CustomTreeBuilder) (*TreeNode, error) {
	return builder.Build(engine)
}

// ConvertNodeToTree 将单个节点转换为树节点
// 用于动态加载子节点时创建节点
//
// 参数:
//   - loc: ResourceLocator
//   - metadata: 节点元数据
//
// 返回: 树节点
func (b *TreeBuilder) ConvertNodeToTree(loc *ResourceLocator, metadata map[string]interface{}) *TreeNode {
	locatorURI := loc.ToURI()

	// 从metadata中提取item_count判断是否有子节点
	hasChildren := false
	if itemCount, ok := metadata["item_count"].(int); ok && itemCount > 0 {
		hasChildren = true
	}

	return &TreeNode{
		ID:          locatorURI,
		Locator:     locatorURI,
		Label:       loc.LastSegment(),
		Type:        string(loc.Type),
		TypeLabel:   "engine.term." + string(loc.Type),
		Icon:        getIconByType(string(loc.Type)),
		Metadata:    metadata,
		Children:    []*TreeNode{},
		HasChildren: hasChildren,
	}
}

// 内部辅助方法

// shouldHaveChildren 判断节点是否应该有子节点
// 统一根据 ItemCount 判断：ItemCount > 0 则有子节点，否则无子节点
// 这样空的 schema/bucket/directory 不会显示展开箭头
func shouldHaveChildren(nodeType string, itemCount int) bool {
	return itemCount > 0
}

// calculateItemCount 计算 Item 的子项数量
// 对于对象存储的 directory/prefix，可能包含子对象，需要从 attributes 中提取
// 对于表、集合、对象等叶子节点，返回 0
func calculateItemCount(itemType string, attributes map[string]interface{}) int {
	// 对象存储的 directory/prefix 可能包含子对象
	if itemType == "directory" || itemType == "prefix" {
		// 从标准 storage 分区中提取 object_count。
		if objCount := commonJSON.Int64(attributes, "storage", "object_count"); objCount > 0 {
			return int(objCount)
		}
		// 如果没有 object_count，返回 0（后续懒加载时会尝试获取子节点）
		return 0
	}

	// table, collection, object 等叶子节点，ItemCount 固定为 0
	return 0
}

// convertMetaNode 递归转换 MetaNode 为 TreeNode
func (b *TreeBuilder) convertMetaNode(engine *models.Engine, node *models.MetaNode) *TreeNode {
	// 统一处理：按引擎语义从 FullName 解析 ResourceLocator.Path
	// - PostgreSQL: "public.users" → ["public", "users"]
	// - MinIO: "addp/image/file.jpg" → ["addp", "image", "file.jpg"]
	// Path 包含完整路径，可以唯一标识资源
	path := parsePathForEngine(engine.EngineType, node.FullName, node.NodeType)

	// 构建 ResourceLocator
	loc := &ResourceLocator{
		EngineID: engine.ID,
		Path:     path,
		Type:     convertNodeType(node.NodeType),
	}
	if itemID := metaItemIDFromNode(node); itemID > 0 {
		loc.ItemID = &itemID
	} else {
		loc.NodeID = &node.ID
	}

	// 构建元数据
	metadata := map[string]interface{}{
		"node_id":     node.ID,
		"full_name":   node.FullName,
		"item_count":  node.ItemCount,
		"size_bytes":  node.TotalSizeBytes,
		"scan_status": node.ScanStatus,
	}
	if node.LastScanAt != nil {
		metadata["scanned_at"] = node.LastScanAt.Format(time.RFC3339)
	}

	// 合并自定义属性（保留规范字段，避免被 attributes 覆盖）
	protectedKeys := map[string]bool{
		"node_id":      true,
		"item_id":      true,
		"is_meta_item": true,
		"full_name":    true,
		"item_count":   true,
		"size_bytes":   true,
		"scan_status":  true,
		"scanned_at":   true,
	}
	for k, v := range node.Attributes {
		if protectedKeys[k] {
			continue
		}
		metadata[k] = v
	}

	locatorURI := loc.ToURI()

	// 根据节点类型和ItemCount判断是否有子节点
	// 容器类型（schema, bucket, directory等）即使ItemCount=0也可能有子节点
	hasChildren := shouldHaveChildren(node.NodeType, node.ItemCount)

	// Children 字段始终初始化为空数组
	// Element Plus el-tree 在非 lazy 模式下需要 children 是数组才会显示展开箭头
	// 前端会根据 hasChildren=true && children.length=0 判断需要懒加载
	treeNode := &TreeNode{
		ID:          locatorURI,
		Locator:     locatorURI,
		Label:       node.Name,
		Type:        node.NodeType,
		TypeLabel:   catalogTypeLabel(engine, node.NodeType),
		Icon:        getIconByType(node.NodeType),
		Metadata:    metadata,
		Children:    []*TreeNode{},
		HasChildren: hasChildren,
	}

	// 递归处理子节点（注意：这里假设 MetaNode 有 Children 字段）
	// 如果 MetaNode 没有 Children，则跳过

	return treeNode
}

func catalogTypeLabel(engine *models.Engine, nodeType string) string {
	if engine == nil || engine.Capabilities == nil {
		return enginePlugin.CatalogTermI18nKey(nodeType)
	}
	capabilities, err := enginePlugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil || capabilities == nil || capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return enginePlugin.CatalogTermI18nKey(nodeType)
	}
	return enginePlugin.CatalogLevelI18nKey(*capabilities.Storage.CatalogModel, nodeType)
}

// calculateItemDepth 动态计算 Item 的深度
// 根据 ItemType 和 FullName 计算节点在树中的深度
func calculateItemDepth(itemType, fullName string) int {
	// 对象存储类型（object）：根据路径中的斜杠数量计算
	if itemType == "object" {
		segments := strings.Split(strings.Trim(fullName, "/"), "/")
		return len(segments)
	}

	// 数据库表类型（table）：根据点号数量计算
	// 例如: "public.users" → depth=2 (schema=1, table=2)
	if itemType == "table" {
		segments := strings.Split(fullName, ".")
		return len(segments)
	}

	// MongoDB 集合/图整体：通常是 database.collection 或 database.graph
	if itemType == "collection" || itemType == "graph" {
		segments := strings.Split(fullName, ".")
		return len(segments)
	}

	// 默认深度为 2（大多数情况下 Items 在第二层）
	return 2
}

// parsePath 从 FullName 解析路径
// 不同类型的资源有不同的路径格式
func parsePath(fullName string, nodeType string) []string {
	return parsePathForEngine("", fullName, nodeType)
}

func parsePathForEngine(engineType string, fullName string, nodeType string) []string {
	if fullName == "" {
		return []string{}
	}

	switch nodeType {
	case "schema", "database":
		// PostgreSQL: schema 只有一级路径
		// MongoDB: database 只有一级路径
		return []string{fullName}
	case "table":
		return ParseFullNamePath(engineType, nodeType, fullName)
	case "collection", "graph":
		// MongoDB 集合/图整体: database.name
		// 使用点号分隔
		return ParseFullNamePath(engineType, nodeType, fullName)
	case "bucket", "prefix", "directory", "object", "root", "dir", "file":
		// MinIO/S3: bucket/prefix/object 使用斜杠分隔
		// 文件系统: root/dir/file 使用斜杠分隔
		// 注意：文件系统 full_name 可能以 "/" 开头，需去掉首尾斜杠，避免产生空路径段
		return ParseFullNamePath(engineType, nodeType, fullName)
	default:
		// 默认：尝试斜杠分隔
		return splitSlashPath(fullName)
	}
}

func splitSlashPath(fullName string) []string {
	trimmed := strings.Trim(fullName, "/")
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "/")
}

// convertNodeType 将 MetaNode 的 NodeType 转换为 ResourceType
func convertNodeType(metaNodeType string) ResourceType {
	mapping := map[string]ResourceType{
		"database":   TypeDatabase,
		"schema":     TypeSchema,
		"bucket":     TypeBucket,
		"prefix":     TypeDirectory,
		"directory":  TypeDirectory,
		"root":       TypeRoot,
		"dir":        TypeDir,
		"table":      TypeTable,
		"collection": TypeCollection,
		"graph":      TypeGraph,
		"object":     TypeObject,
		"file":       TypeFile,
	}
	if t, ok := mapping[metaNodeType]; ok {
		return t
	}
	return TypeUnknown
}

// buildEngineRootLocator 构建引擎根节点的 Locator
func buildEngineRootLocator(engineID uint) string {
	return EngineRootLocator(engineID)
}

func EngineIcon(engine *models.Engine) string {
	switch engineFamily(engine) {
	case "object", "file":
		return "FolderOpen"
	case "document":
		return "DocumentText"
	case "graph":
		return "Share"
	case "workflow":
		return "Grid"
	case "script":
		return "CodeBracket"
	case "tabular":
		return "Database"
	}

	if engine == nil {
		return "Database"
	}
	switch strings.ToLower(strings.TrimSpace(engine.EngineType)) {
	case "minio", "s3", "nfs", "nas":
		return "FolderOpen"
	case "mongodb":
		return "DocumentText"
	case "neo4j":
		return "Share"
	case "python_workflow", "spark_workflow", "math_workflow", "spark":
		return "Grid"
	case "jupyter":
		return "CodeBracket"
	default:
		return "Database"
	}
}

func engineFamily(engine *models.Engine) string {
	if engine == nil || engine.Capabilities == nil {
		return ""
	}
	var caps struct {
		EngineFamily string `json:"engine_family"`
		Compute      struct {
			Workflow *struct {
				Supported bool `json:"supported"`
			} `json:"workflow"`
			Script *struct {
				Supported bool `json:"supported"`
			} `json:"script"`
		} `json:"compute"`
	}
	if err := json.Unmarshal([]byte(*engine.Capabilities), &caps); err != nil {
		return ""
	}
	if caps.EngineFamily != "" {
		return caps.EngineFamily
	}
	if caps.Compute.Workflow != nil && caps.Compute.Workflow.Supported {
		return "workflow"
	}
	if caps.Compute.Script != nil && caps.Compute.Script.Supported {
		return "script"
	}
	return ""
}

// getIconByType 根据节点类型返回图标
func getIconByType(nodeType string) string {
	icons := map[string]string{
		"database":   "Database",
		"schema":     "Folder",
		"bucket":     "FolderOpen",
		"directory":  "Folder",
		"prefix":     "Folder",
		"root":       "FolderOpen",
		"dir":        "Folder",
		"table":      "Table",
		"collection": "DocumentText",
		"graph":      "Share",
		"object":     "Document",
		"file":       "Document",
	}
	if icon, ok := icons[nodeType]; ok {
		return icon
	}
	return "Document"
}

// MergeMetadata 合并 Meta 数据到现有树
// 用于增量更新树节点的元数据
//
// 参数:
//   - tree: 现有的树
//   - metaNodes: Meta 模块的节点列表
//
// 返回: 更新后的树
func (b *TreeBuilder) MergeMetadata(tree *TreeNode, metaNodes []*models.MetaNode) *TreeNode {
	// 构建 node_id 到 MetaNode 的映射
	metaMap := make(map[uint]*models.MetaNode)
	for _, node := range metaNodes {
		metaMap[node.ID] = node
	}

	// 递归更新树节点
	b.mergeNodeMetadata(tree, metaMap)
	return tree
}

// mergeNodeMetadata 递归合并节点元数据
func (b *TreeBuilder) mergeNodeMetadata(node *TreeNode, metaMap map[uint]*models.MetaNode) {
	// 更新当前节点
	if nodeIDVal, ok := node.Metadata["node_id"]; ok {
		if nodeID, ok := nodeIDVal.(uint); ok {
			if metaNode, exists := metaMap[nodeID]; exists {
				node.Metadata["item_count"] = metaNode.ItemCount
				node.Metadata["size_bytes"] = metaNode.TotalSizeBytes
				node.Metadata["scan_status"] = metaNode.ScanStatus
				if metaNode.LastScanAt != nil {
					node.Metadata["scanned_at"] = metaNode.LastScanAt.Format(time.RFC3339)
				}
			}
		}
	}

	// 递归更新子节点
	for _, child := range node.Children {
		b.mergeNodeMetadata(child, metaMap)
	}
}

// GetNodeByLocator 从树中根据 Locator 查找节点
// 用于快速定位特定节点
//
// 参数:
//   - tree: 树的根节点
//   - locator: ResourceLocator URI
//
// 返回: 找到的节点（未找到返回 nil）
func GetNodeByLocator(tree *TreeNode, locator string) *TreeNode {
	if tree.Locator == locator {
		return tree
	}

	for _, child := range tree.Children {
		if found := GetNodeByLocator(child, locator); found != nil {
			return found
		}
	}

	return nil
}

// FilterTreeByType 过滤树中特定类型的节点
// 用于只显示特定类型的资源（例如只显示表，不显示 schema）
//
// 参数:
//   - tree: 树的根节点
//   - types: 要保留的节点类型列表
//
// 返回: 过滤后的树
func FilterTreeByType(tree *TreeNode, types []string) *TreeNode {
	typeSet := make(map[string]bool)
	for _, t := range types {
		typeSet[t] = true
	}

	return filterNode(tree, typeSet)
}

func filterNode(node *TreeNode, typeSet map[string]bool) *TreeNode {
	// 如果当前节点类型不在集合中，返回 nil
	if !typeSet[node.Type] && node.Type != "engine" {
		return nil
	}

	// 过滤子节点
	filteredChildren := []*TreeNode{}
	for _, child := range node.Children {
		if filtered := filterNode(child, typeSet); filtered != nil {
			filteredChildren = append(filteredChildren, filtered)
		}
	}

	// 创建新节点（避免修改原树）
	return &TreeNode{
		Locator:  node.Locator,
		Label:    node.Label,
		Type:     node.Type,
		Icon:     node.Icon,
		Metadata: node.Metadata,
		Children: filteredChildren,
	}
}

func isNFSEngine(engineType string) bool {
	switch strings.ToLower(engineType) {
	case "nfs", "nas":
		return true
	}
	return false
}
