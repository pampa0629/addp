package resourcetree

import (
	"encoding/json"
	"maps"
	"strings"
	"time"

	enginePlugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// TreeNode 统一的树节点结构
// 用于在各模块间传递资源树数据（Manager、Meta、Service、Orchestrator）
type TreeNode struct {
	ID          string                 `json:"id"`          // 节点唯一标识（使用 locator 的值）
	Locator     string                 `json:"locator"`     // ResourceLocator URI (addp://engine/1/path/public/users?type=table)
	Label       string                 `json:"label"`       // 显示标签（节点名称）
	Type        string                 `json:"type"`        // catalog 术语（如 server/schema/table/service/bucket/prefix/root/directory/file/object）
	TypeLabel   string                 `json:"typeLabel"`   // 类型的 i18n key，如 "engine.term.schema"（前端查 i18n 字典展示）
	Icon        string                 `json:"icon"`        // 图标名称
	Metadata    map[string]interface{} `json:"metadata"`    // 展示元数据：Node 使用 node_id/item_count，DataItem 仅使用 item_id；展开仅由显式子级事实决定
	Children    []*TreeNode            `json:"children"`    // 子节点
	HasChildren bool                   `json:"hasChildren"` // 是否有子节点（用于显示展开图标）
}

// TreeBuilder 资源树构建器
// 提供通用的树构建能力，可被多个模块复用（Manager、Meta、Service、Orchestrator）
type TreeBuilder struct{}

// CustomTreeBuilder 自定义树构建器接口（用于降级方案）
type CustomTreeBuilder interface {
	// Build 自定义构建逻辑
	Build(engine *models.Engine) (*TreeNode, error)
}

// NewTreeBuilder 创建树构建器
func NewTreeBuilder() *TreeBuilder {
	return &TreeBuilder{}
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
	return b.BuildFromMeta(engine, allNodes, metadataTree.Items)
}

// BuildFromMeta 从 Meta 模块的节点构建树
// 适用场景：Manager、Meta、Service 模块
//
// 参数:
//   - engine: 引擎信息
//   - metaNodes: Meta 模块的节点列表（已按层级组织）
//   - items: 真实数据项列表，直接投影为叶子，不转换为 MetaNode
//
// 返回: 树的根节点
func (b *TreeBuilder) BuildFromMeta(engine *models.Engine, metaNodes []*models.MetaNode, items []models.MetaItem) (*TreeNode, error) {
	// 构建节点 ID 到 TreeNode 的映射
	nodeMap := make(map[uint]*TreeNode)
	skipNodeIDs, itemParentOverrides := wholeScopePresentationOverrides(metaNodes, items)
	var catalogRoot *TreeNode

	// 第一遍：创建所有节点
	for _, node := range metaNodes {
		if skipNodeIDs[node.ID] {
			continue
		}
		treeNode := b.convertMetaNode(engine, node)
		nodeMap[node.ID] = treeNode
		if isCatalogRootMetaNode(node) {
			catalogRoot = treeNode
		}
	}

	if catalogRoot == nil {
		catalogRoot = buildFallbackCatalogRootTreeNode(engine, false)
	}

	// 第二遍：建立父子关系
	for _, node := range metaNodes {
		if skipNodeIDs[node.ID] {
			continue
		}
		treeNode := nodeMap[node.ID]

		if node.ParentNodeID == nil {
			if treeNode != catalogRoot {
				catalogRoot.Children = append(catalogRoot.Children, treeNode)
			}
		} else if parentTreeNode, exists := nodeMap[*node.ParentNodeID]; exists {
			parentTreeNode.Children = append(parentTreeNode.Children, treeNode)
		} else {
			catalogRoot.Children = append(catalogRoot.Children, treeNode)
		}
	}

	for i := range items {
		item := &items[i]
		parentID := &item.NodeID
		if override, ok := itemParentOverrides[item.ID]; ok {
			parentID = override
		} else if skipNodeIDs[item.NodeID] {
			continue
		}
		parent := catalogRoot
		if parentID != nil && nodeMap[*parentID] != nil {
			parent = nodeMap[*parentID]
		}
		parent.Children = append(parent.Children, b.convertMetaItem(engine, item))
	}
	for _, node := range nodeMap {
		node.HasChildren = node.HasChildren || len(node.Children) > 0
		node.Metadata["has_children"] = node.HasChildren
	}
	catalogRoot.HasChildren = catalogRoot.HasChildren || len(catalogRoot.Children) > 0
	catalogRoot.Metadata["has_children"] = catalogRoot.HasChildren
	return catalogRoot, nil
}

func isCatalogRootMetaNode(node *models.MetaNode) bool {
	if node == nil {
		return false
	}
	return node.ParentNodeID == nil && node.FullName == ""
}

func buildFallbackCatalogRootTreeNode(engine *models.Engine, hasChildren bool) *TreeNode {
	rootType := catalogRootResourceType(engine)
	rootLocator := buildEngineRootLocator(engine.ID, rootType)
	return &TreeNode{
		ID:          rootLocator,
		Locator:     rootLocator,
		Label:       engine.Name,
		Type:        string(rootType),
		TypeLabel:   enginePlugin.EngineCatalogTermI18nKey(string(rootType)),
		Icon:        getIconByType(string(rootType)),
		Metadata:    catalogRootMetadata(engine, hasChildren),
		Children:    []*TreeNode{},
		HasChildren: hasChildren,
	}
}

func wholeScopePresentationOverrides(metaNodes []*models.MetaNode, items []models.MetaItem) (map[uint]bool, map[uint]*uint) {
	byID := make(map[uint]*models.MetaNode, len(metaNodes))
	for _, node := range metaNodes {
		if node != nil {
			byID[node.ID] = node
		}
	}
	skip := map[uint]bool{}
	parentOverrides := map[uint]*uint{}
	for _, item := range items {
		if itemLayoutForTreeMetadata(item.Attributes) != "whole" {
			continue
		}
		parent := byID[item.NodeID]
		if parent == nil || !sameResourceFullName(parent.FullName, item.FullName) || !isPathContainerNodeType(parent.NodeType) {
			continue
		}
		skip[parent.ID] = true
		parentOverrides[item.ID] = parent.ParentNodeID
	}
	// whole 的同路径目录只用于持久归属，其内部目录不再投影为资源树分支。
	for _, node := range metaNodes {
		for parentID := node.ParentNodeID; parentID != nil && byID[*parentID] != nil; parentID = byID[*parentID].ParentNodeID {
			if skip[*parentID] {
				skip[node.ID] = true
				break
			}
		}
	}
	return skip, parentOverrides
}

func sameResourceFullName(left, right string) bool {
	return strings.Trim(left, "/") == strings.Trim(right, "/")
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

// ConvertMetaItems 将真实 MetaItem 直接转换为资源树叶子。
func (b *TreeBuilder) ConvertMetaItems(engine *models.Engine, items []models.MetaItem) []*TreeNode {
	treeNodes := make([]*TreeNode, 0, len(items))
	for i := range items {
		treeNodes = append(treeNodes, b.convertMetaItem(engine, &items[i]))
	}
	return treeNodes
}

func (b *TreeBuilder) convertMetaItem(engine *models.Engine, item *models.MetaItem) *TreeNode {
	loc := &ResourceLocator{
		EngineID: engine.ID,
		Path:     parsePathForEngine(engine.EngineType, item.FullName, item.ItemType),
		Type:     convertNodeType(item.ItemType),
		ItemID:   &item.ID,
	}
	metadata := engineTreeMetadata(engine, item.FullName, "completed", item.ScannedAt)
	metadata["item_id"] = item.ID
	metadata["scanned_depth"] = item.ScannedDepth
	metadata["size_bytes"] = metaItemSizeBytes(item)
	mergeTreeAttributes(metadata, item.Attributes)
	if item.RowCount != nil {
		metadata["row_count"] = *item.RowCount
		// 保留表摘要的记录数投影；只复制将要修改的 map，不修改持久 attributes。
		typeInfo, _ := metadata["type_info"].(map[string]interface{})
		typeInfo = maps.Clone(typeInfo)
		if typeInfo == nil {
			typeInfo = map[string]interface{}{}
		}
		table, _ := typeInfo["table"].(map[string]interface{})
		table = maps.Clone(table)
		if table == nil {
			table = map[string]interface{}{}
		}
		if _, exists := table["row_count"]; !exists {
			table["row_count"] = *item.RowCount
		}
		typeInfo["table"] = table
		metadata["type_info"] = typeInfo
	}
	if item.DataUpdatedAt != nil {
		metadata["last_modified_at"] = item.DataUpdatedAt.Format(time.RFC3339)
	}
	return &TreeNode{
		ID: loc.ToURI(), Locator: loc.ToURI(), Label: item.Name,
		Type: item.ItemType, TypeLabel: catalogTypeLabel(engine, item.ItemType),
		Icon: getIconByType(item.ItemType), Metadata: metadata,
		Children: []*TreeNode{}, HasChildren: false,
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

// convertMetaNode 将真实 MetaNode 转换为树分支。
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
	loc.NodeID = &node.ID
	metadata := engineTreeMetadata(engine, node.FullName, node.ScanStatus, node.LastScanAt)
	metadata["node_id"] = node.ID
	metadata["item_count"] = node.ItemCount
	metadata["scanned_depth"] = node.ScannedDepth
	metadata["has_children"] = node.HasChildren
	metadata["size_bytes"] = node.TotalSizeBytes
	mergeTreeAttributes(metadata, node.Attributes)

	locatorURI := loc.ToURI()

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
		HasChildren: node.HasChildren,
	}

	return treeNode
}

func engineTreeMetadata(engine *models.Engine, fullName string, scanStatus string, scannedAt *time.Time) map[string]interface{} {
	metadata := map[string]interface{}{
		"engine_id":     engine.ID,
		"engine_type":   engine.EngineType,
		"engine_family": engineFamily(engine),
		"capabilities":  engine.Capabilities,
		"full_name":     fullName,
		"has_children":  false,
		"scan_status":   scanStatus,
	}
	if scannedAt != nil {
		metadata["scanned_at"] = scannedAt.Format(time.RFC3339)
	}
	return metadata
}

func catalogRootMetadata(engine *models.Engine, hasChildren bool) map[string]interface{} {
	metadata := engineTreeMetadata(engine, "", "", nil)
	metadata["item_count"] = 0
	metadata["has_children"] = hasChildren
	return metadata
}

func mergeTreeAttributes(metadata, attrs map[string]interface{}) {
	// 身份、扫描事实和 Node 聚合不能由 attributes 覆盖或引入。
	protected := map[string]bool{
		"engine_id": true, "engine_type": true, "engine_family": true, "capabilities": true,
		"node_id": true, "item_id": true, "is_meta_item": true, "full_name": true,
		"item_count": true, "total_size_bytes": true, "size_bytes": true,
		"has_children": true, "scan_status": true, "scanned_depth": true, "scanned_at": true,
	}
	for key, value := range attrs {
		if !protected[key] {
			metadata[key] = value
		}
	}
	addMetaItemFacts(metadata, attrs)
}

func addMetaItemFacts(metadata map[string]interface{}, attrs map[string]interface{}) {
	if metadata == nil || len(attrs) == 0 {
		return
	}
	if dataType := dataTypeForTreeMetadata(attrs); dataType != "" {
		metadata["data_type"] = dataType
	}
	if formatName := formatNameForTreeMetadata(attrs); formatName != "" {
		metadata["format"] = formatName
	}
	if layout := itemLayoutForTreeMetadata(attrs); layout != "" {
		metadata["layout"] = layout
	}
	if physicalPath := physicalPathForTreeMetadata(attrs); physicalPath != "" {
		metadata["physical_path"] = physicalPath
	}
	if rowCount := tableRowCountForTreeMetadata(attrs); rowCount > 0 {
		metadata["row_count"] = rowCount
	}
	if fieldCount := tableFieldCountForTreeMetadata(attrs); fieldCount > 0 {
		metadata["field_count"] = fieldCount
	}
	if spatial := spatialSummaryForTreeMetadata(attrs); len(spatial) > 0 {
		metadata["spatial"] = spatial
	}
}

func catalogTypeLabel(engine *models.Engine, nodeType string) string {
	if engine == nil || engine.Capabilities == nil {
		return enginePlugin.EngineCatalogTermI18nKey(nodeType)
	}
	capabilities, err := enginePlugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil || capabilities == nil || capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return enginePlugin.EngineCatalogTermI18nKey(nodeType)
	}
	return enginePlugin.EngineCatalogLevelI18nKey(*capabilities.Storage.CatalogModel, nodeType)
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
	case "schema", "database", "index", "topic":
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
		"prefix":     TypePrefix,
		"directory":  TypeDirectory,
		"root":       TypeRoot,
		"server":     TypeServer,
		"service":    TypeService,
		"dir":        TypeDir,
		"table":      TypeTable,
		"collection": TypeCollection,
		"topic":      TypeTopic,
		"index":      TypeIndex,
		"graph":      TypeGraph,
		"keyspace":   TypeKeyspace,
		"object":     TypeObject,
		"file":       TypeFile,
	}
	if t, ok := mapping[metaNodeType]; ok {
		return t
	}
	return TypeUnknown
}

// buildEngineRootLocator 构建引擎根节点的 Locator
func buildEngineRootLocator(engineID uint, rootType ResourceType) string {
	return EngineRootLocatorForType(engineID, rootType)
}

func catalogRootResourceType(engine *models.Engine) ResourceType {
	return EngineCatalogRootResourceType(engine)
}

// EngineCatalogRootResourceType 返回引擎结构根在 ResourceLocator 中应使用的 root term。
func EngineCatalogRootResourceType(engine *models.Engine) ResourceType {
	if engine == nil {
		return TypeRoot
	}
	if rootType := catalogRootResourceTypeFromCapabilities(engine); rootType != TypeUnknown {
		return rootType
	}
	switch engineFamily(engine) {
	case "object", "event_stream":
		return TypeService
	case "file":
		return TypeRoot
	case "tabular", "dynamic_schema", "document", "graph":
		return TypeServer
	}
	return TypeRoot
}

func catalogRootResourceTypeFromCapabilities(engine *models.Engine) ResourceType {
	if engine == nil || engine.Capabilities == nil {
		return TypeUnknown
	}
	capabilities, err := enginePlugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil || capabilities == nil || capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return TypeUnknown
	}
	switch strings.TrimSpace(capabilities.Storage.CatalogModel.RootTerm) {
	case enginePlugin.EngineCatalogTermServer:
		return TypeServer
	case enginePlugin.EngineCatalogTermService:
		return TypeService
	case enginePlugin.EngineCatalogTermRoot:
		return TypeRoot
	default:
		return TypeUnknown
	}
}

func engineFamily(engine *models.Engine) string {
	if engine == nil {
		return ""
	}
	if engine.Capabilities == nil {
		if registered, err := enginePlugin.Get(engine.EngineType); err == nil {
			return registered.Capabilities().EngineFamily
		}
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
		if registered, pluginErr := enginePlugin.Get(engine.EngineType); pluginErr == nil {
			return registered.Capabilities().EngineFamily
		}
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
		"server":     "FolderOpen",
		"service":    "FolderOpen",
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
				node.Metadata["scanned_depth"] = metaNode.ScannedDepth
				if metaNode.LastScanAt != nil {
					node.Metadata["scanned_at"] = metaNode.LastScanAt.Format(time.RFC3339)
				} else {
					delete(node.Metadata, "scanned_at")
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
// 用于只显示特定类型的资源，同时保留通往匹配资源的结构祖先。
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
	if node == nil {
		return nil
	}

	// 过滤子节点
	filteredChildren := []*TreeNode{}
	for _, child := range node.Children {
		if filtered := filterNode(child, typeSet); filtered != nil {
			filteredChildren = append(filteredChildren, filtered)
		}
	}
	if !typeSet[node.Type] && len(filteredChildren) == 0 {
		return nil
	}

	// 创建新节点（避免修改原树），并保留完整节点契约。
	filtered := *node
	filtered.Children = filteredChildren
	filtered.HasChildren = len(filteredChildren) > 0
	return &filtered
}
