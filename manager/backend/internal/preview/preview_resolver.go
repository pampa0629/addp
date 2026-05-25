package preview

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/catalogview"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/format"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/catalogutil"
	"github.com/addp/manager/internal/models"
)

var ErrPreviewRequiresScannedMeta = errors.New("preview requires scanned meta item or node")
var ErrEngineAccessDenied = errors.New("engine not accessible for current tenant")

// PreviewResolver 数据预览解析器
// 职责：
// 1. 解析 ResourceLocator URI
// 2. 选择合适的 PreviewProvider（通过 Registry）
// 3. 执行预览
// 4. 组装 PreviewResult
//
// 注意：重命名自 PreviewOrchestrator，避免与 Orchestrator 模块混淆
type PreviewResolver struct {
	registry     *PreviewRegistry
	systemClient *commonClient.SystemClient
	metaClient   *commonClient.MetaClient
}

// NewPreviewResolver 创建预览解析器
func NewPreviewResolver(
	registry *PreviewRegistry,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
) *PreviewResolver {
	if registry == nil {
		registry = NewPreviewRegistry()
	}

	return &PreviewResolver{
		registry:     registry,
		systemClient: systemClient,
		metaClient:   metaClient,
	}
}

// PreviewRequest 新的预览请求（基于 ResourceLocator）
type PreviewResolverRequest struct {
	Locator         *catalogview.ResourceLocator // 资源定位符
	Engine          *commonModels.Engine         // 引擎信息
	Metadata        *commonModels.MetaNode       // 可选：Meta 节点数据
	Pagination      *Pagination                  // 分页参数
	TenantID        *uint                        // 租户 ID
	ItemType        string                       // 数据项类型（如 "table"），来自 MetaItem
	PhysicalPath    string                       // 物理路径（来自 meta_item.attributes.storage.physical_path）
	ScopePath       string                       // 范围路径（来自 meta_item.attributes.storage.physical_path）
	ChildName       string                       // 容器内部 child 名称，例如 Excel sheet
	RefPath         string                       // multi child 内的单个ref 路径，指向容器内原始对象
	NestedChildPath string                       // 当前 child 是容器时，继续寻址其内部 child 的相对路径
}

// Pagination 分页参数
type Pagination struct {
	Page     int
	PageSize int
}

// PreviewResult 预览结果（新结构）
type PreviewResult struct {
	PreviewType string      `json:"preview_type"` // "table"/"object"/"node"/"unsupported"
	Data        interface{} `json:"data"`         // 预览数据

	// 预览上下文元数据
	Metadata *PreviewMetadata `json:"metadata"`
}

// PreviewMetadata 预览上下文元数据
type PreviewMetadata struct {
	Locator      string `json:"locator"`       // ResourceLocator URI
	EngineName   string `json:"engine_name"`   // 引擎名称
	ResourceType string `json:"resource_type"` // 引擎类型（postgresql/minio）
	MetaScanned  bool   `json:"meta_scanned"`  // 是否已被 Meta 扫描
	ItemCount    *int64 `json:"item_count"`    // 项目数（来自 Meta）
	SizeBytes    *int64 `json:"size_bytes"`    // 大小（来自 Meta）
}

// Preview 执行预览。预览必须基于已经由 Meta 扫描入库的节点或 item。
func (r *PreviewResolver) Preview(ctx context.Context, req *PreviewResolverRequest) (*PreviewResult, error) {
	// 1. 验证参数
	if req.Locator == nil {
		return nil, fmt.Errorf("locator is required")
	}
	if req.Engine == nil {
		return nil, fmt.Errorf("engine is required")
	}

	if req.Metadata == nil {
		return nil, ErrPreviewRequiresScannedMeta
	}

	// 3. 转换为旧的 PreviewRequest 格式（provider 接口过渡期复用）
	legacyReq := r.convertToLegacyRequest(req)

	// 4. 按 Meta 标准属性确定性选择预览插件
	provider, err := r.resolveProviderByMeta(req, legacyReq)
	if err != nil {
		logger.L().Warn("未找到合适的预览插件", "locator", req.Locator.ToURI(), "error", err)
		return &PreviewResult{
			PreviewType: "unsupported",
			Data:        nil,
			Metadata:    r.buildMetadata(req),
		}, nil
	}
	if refProvider := r.resolveRefPreviewProvider(req, legacyReq); refProvider != nil {
		provider = refProvider
	}

	logger.L().Info("选择预览插件", "provider", provider.Name(), "locator", req.Locator.ToURI(), "item_type", req.ItemType)

	// 5. 执行预览
	tablePreview, err := provider.Preview(ctx, legacyReq)
	if err != nil {
		return nil, fmt.Errorf("preview failed: %w", err)
	}

	// 6. 补充 item_meta（来自 meta）
	r.attachItemMeta(tablePreview, req)

	// 7. 组装新的 PreviewResult
	result := r.convertToNewResult(tablePreview, req)

	return result, nil
}

func (r *PreviewResolver) resolveRefPreviewProvider(req *PreviewResolverRequest, legacyReq *PreviewRequest) PreviewProvider {
	if r == nil || r.registry == nil || req == nil || legacyReq == nil {
		return nil
	}
	if strings.TrimSpace(req.ChildName) != "" || strings.TrimSpace(req.RefPath) == "" {
		return nil
	}
	if !isContentFileItemType(legacyReq.ItemType) {
		return nil
	}
	if provider, err := r.registry.GetByName("builtin:ref-file"); err == nil {
		return provider
	}
	return nil
}

// PreviewFromURI 从 URI 执行预览（便捷方法）
func (r *PreviewResolver) PreviewFromURI(ctx context.Context, locatorURI string, page, pageSize int, childName string, tenantID *uint) (*PreviewResult, error) {
	return r.PreviewFromURIWithSelection(ctx, locatorURI, page, pageSize, childName, "", "", tenantID)
}

func (r *PreviewResolver) PreviewFromURIWithSelection(ctx context.Context, locatorURI string, page, pageSize int, childName, refPath, nestedChildPath string, tenantID *uint) (*PreviewResult, error) {
	// 1. 解析 Locator
	loc, err := catalogview.ParseURI(locatorURI)
	if err != nil {
		return nil, fmt.Errorf("invalid locator: %w", err)
	}

	// 2. 通过 SystemClient 获取引擎信息
	if r.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	engine, err := r.systemClient.GetEngine(loc.EngineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 验证租户权限
	if tenantID != nil && engine.TenantID != nil && *engine.TenantID != *tenantID {
		return nil, ErrEngineAccessDenied
	}

	// 3. 尝试从 Meta 获取元数据。
	// meta_id 是最可靠的类型来源，尤其是图模型的 label/relationship 这类
	// 旧 locator type 可能被前端树兼容层误写成 table 的数据项。
	var metaNode *commonModels.MetaNode
	var metaItem *commonModels.MetaItem
	metaIDResolved := false
	if r.metaClient != nil {
		// 设置租户 ID，确保服务间调用时正确过滤
		r.metaClient.SetTenantID(tenantID)

		// 优先通过 meta_id 直接获取节点（更精确，避免路径歧义）
		// 注意：虚拟 ID（>= 100000）对应 MetaItem，需要解码为真实 item ID
		if loc.MetaID != nil {
			metaID := *loc.MetaID
			if metaID >= 100000 {
				// 虚拟 ID：MetaItem 的 ID + 100000
				realItemID := metaID - 100000
				item, err := r.metaClient.GetMetaItemByID(realItemID)
				if err == nil && item != nil {
					if item.ScannedDepth != "deep" {
						if ensureErr := r.metaClient.EnsureItemDeepScanned(realItemID); ensureErr != nil {
							logger.L().Warn("触发 Meta item deep 补齐失败", "item_id", realItemID, "error", ensureErr)
						} else if refreshed, refreshErr := r.metaClient.GetMetaItemByID(realItemID); refreshErr == nil && refreshed != nil {
							item = refreshed
						}
					}
					metaItem = item
					metaIDResolved = true
					logger.L().Debug("从 Meta 通过虚拟 ID 获取到 MetaItem",
						"virtual_meta_id", metaID,
						"real_item_id", realItemID,
						"item_type", item.ItemType)
				} else {
					logger.L().Debug("未从 Meta 通过虚拟 ID 获取到 MetaItem",
						"virtual_meta_id", metaID,
						"real_item_id", realItemID,
						"error", err)
				}
			} else {
				node, err := r.metaClient.GetMetaNode(metaID)
				if err == nil && node != nil {
					metaNode = node
					metaIDResolved = true
					logger.L().Debug("从 Meta 通过 ID 获取到节点元数据",
						"meta_id", metaID,
						"node_type", node.NodeType,
						"total_size_bytes", node.TotalSizeBytes)
				} else {
					logger.L().Debug("未从 Meta 通过 ID 获取到节点元数据",
						"meta_id", metaID,
						"error", err)
				}
			}
		}
	}

	if r.metaClient != nil {
		// 设置租户 ID，确保服务间调用时正确过滤
		r.metaClient.SetTenantID(tenantID)

		if !metaIDResolved && len(loc.Path) > 0 {
			catalogPath := strings.Join(loc.Path, "/")
			if isPreviewItemLocator(loc) {
				item, err := r.metaClient.GetItemByCatalogPath(loc.EngineID, catalogPath)
				if err == nil && item != nil {
					if item.ScannedDepth != "deep" {
						if ensureErr := r.metaClient.EnsureItemDeepScanned(item.ID); ensureErr != nil {
							logger.L().Warn("触发 Meta item deep 补齐失败", "item_id", item.ID, "error", ensureErr)
						} else if refreshed, refreshErr := r.metaClient.GetMetaItemByID(item.ID); refreshErr == nil && refreshed != nil {
							item = refreshed
						}
					}
					metaItem = item
					logger.L().Debug("从 Meta 获取到数据项元数据",
						"catalog_path", catalogPath,
						"size_bytes", item.SizeBytes)
				} else {
					logger.L().Debug("未从 Meta 获取到数据项元数据",
						"catalog_path", catalogPath,
						"error", err)
				}
			} else {
				// Bucket 或 Prefix 路径
				node, err := r.metaClient.GetNodeByCatalogPath(loc.EngineID, catalogPath)
				if err == nil && node != nil {
					metaNode = node
					logger.L().Debug("从 Meta 获取到节点元数据",
						"catalog_path", catalogPath,
						"total_size_bytes", node.TotalSizeBytes)
				}
			}
		}
	}

	if metaNode == nil && metaItem == nil {
		return nil, ErrPreviewRequiresScannedMeta
	}

	// 对文件系统类型，用 metaItem.FullName 修正 locator 路径（前端传来的路径可能缺少根目录）
	if metaItem != nil && isFileSystemType(engine.EngineType) && metaItem.FullName != "" {
		parts := strings.Split(strings.Trim(metaItem.FullName, "/"), "/")
		if len(parts) > 0 {
			loc.Path = parts
		}
	}

	// 4. 构建请求
	req := &PreviewResolverRequest{
		Locator: loc,
		Engine:  engine,
		Pagination: &Pagination{
			Page:     page,
			PageSize: pageSize,
		},
		TenantID:        tenantID,
		ChildName:       strings.TrimSpace(childName),
		RefPath:         strings.Trim(strings.TrimSpace(refPath), "/"),
		NestedChildPath: strings.Trim(strings.TrimSpace(nestedChildPath), "/"),
	}

	locatorType := strings.ToLower(strings.TrimSpace(string(loc.Type)))
	if isPreviewItemType(locatorType) {
		req.ItemType = locatorType
	}

	// 设置元数据（如果有）
	if metaNode != nil {
		req.Metadata = metaNode
		// 从 MetaNode 的 NodeType 推断 ItemType
		if metaNode.NodeType != "" && metaNode.NodeType != "directory" && metaNode.NodeType != "bucket" && metaNode.NodeType != "dir" && metaNode.NodeType != "root" {
			req.ItemType = metaNode.NodeType
		}
	} else if metaItem != nil {
		// 将 MetaItem 转换为 MetaNode 格式
		sizeBytes := int64(0)
		if metaItem.SizeBytes != nil {
			sizeBytes = *metaItem.SizeBytes
		}
		attrs := cloneMetaAttributes(metaItem.Attributes)
		if metaItem.RowCount != nil && *metaItem.RowCount > 0 {
			upsertMetaNested(attrs, "type_info", "table", map[string]interface{}{
				"row_count": *metaItem.RowCount,
			})
		}
		req.Metadata = &commonModels.MetaNode{
			ID:             metaItem.ID,
			EngineID:       metaItem.EngineID,
			Path:           metaItem.FullName,
			FullName:       metaItem.FullName,
			ItemCount:      1,
			TotalSizeBytes: sizeBytes,
			Attributes:     attrs,
		}
		req.ItemType = metaItem.ItemType
		req.PhysicalPath, req.ScopePath = previewResourcePaths(attrs)
	}

	// 5. 执行预览
	return r.Preview(ctx, req)
}

func cloneMetaAttributes(attrs map[string]interface{}) map[string]interface{} {
	if len(attrs) == 0 {
		return map[string]interface{}{}
	}
	cloned := make(map[string]interface{}, len(attrs))
	for k, v := range attrs {
		cloned[k] = v
	}
	return cloned
}

func upsertMetaNested(attrs map[string]interface{}, section, namespace string, values map[string]interface{}) {
	if attrs == nil {
		return
	}
	if attrs[section] == nil {
		attrs[section] = map[string]interface{}{}
	}
	sectionMap, ok := attrs[section].(map[string]interface{})
	if !ok {
		sectionMap = map[string]interface{}{}
		attrs[section] = sectionMap
	}
	if sectionMap[namespace] == nil {
		sectionMap[namespace] = map[string]interface{}{}
	}
	targetMap, ok := sectionMap[namespace].(map[string]interface{})
	if !ok {
		targetMap = map[string]interface{}{}
		sectionMap[namespace] = targetMap
	}
	for key, value := range values {
		targetMap[key] = value
	}
}

func previewResourcePaths(attrs map[string]interface{}) (physicalPath string, scopePath string) {
	physPath := catalogutil.StringAttribute(attrs, "physical_path")
	if physPath == "" {
		return "", ""
	}
	switch catalogutil.StringAttribute(attrs, "layout") {
	case "single":
		return physPath, ""
	case "whole":
		return "", physPath
	default:
		return "", ""
	}
}

func isPreviewItemLocator(loc *catalogview.ResourceLocator) bool {
	if loc == nil {
		return false
	}
	return isPreviewItemType(strings.ToLower(strings.TrimSpace(string(loc.Type))))
}

func isContentCatalogEngine(engineType string) bool {
	return IsContentCatalogEngine(engineType)
}

func isContentFileItemType(itemType string) bool {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "object", "file":
		return true
	default:
		return false
	}
}

func isPreviewItemType(itemType string) bool {
	switch itemType {
	case "table", "view", "materialized_view", "collection", "label", "relationship", "object", "file":
		return true
	default:
		return false
	}
}

// DetectPreviewType 检测预览类型
func (r *PreviewResolver) DetectPreviewType(loc *catalogview.ResourceLocator, engine *commonModels.Engine) string {
	req := &PreviewResolverRequest{
		Locator: loc,
		Engine:  engine,
	}
	if loc != nil {
		req.ItemType = strings.ToLower(strings.TrimSpace(string(loc.Type)))
	}
	legacyReq := r.convertToLegacyRequest(req)

	provider, err := r.resolveProviderByMeta(req, legacyReq)
	if err != nil {
		return "unsupported"
	}

	return provider.Name()
}

// 内部辅助方法

// convertToLegacyRequest 将新的请求转换为旧的 PreviewRequest 格式（兼容现有插件）
func (r *PreviewResolver) convertToLegacyRequest(req *PreviewResolverRequest) *PreviewRequest {
	schema := ""
	table := ""

	// 对于对象存储类型，schema 是根名称，table 是完整的子路径
	// 对于文件系统类型，根目录下文件需要映射为 schema="" + table="文件名"，避免被识别为目录
	// 对于关系型数据库，schema 是第一个组件，table 是第二个组件
	if isObjectStorageType(req.Engine.EngineType) {
		// 对象存储: path = [bucket, ...subPath]
		if len(req.Locator.Path) >= 1 {
			schema = req.Locator.Path[0]
			if len(req.Locator.Path) > 1 {
				table = strings.Join(req.Locator.Path[1:], "/")
			}
		}
	} else if isFileSystemType(req.Engine.EngineType) {
		// 文件系统:
		// 1) 目录节点: path=[rootName,...subPath] -> schema=rootName, table=subPath
		// 2) 根目录下文件: path=[fileName] -> schema="", table=fileName
		nodeType := strings.ToLower(string(req.Locator.Type))
		if len(req.Locator.Path) == 1 && (nodeType == "file" || nodeType == "object") {
			table = req.Locator.Path[0]
		} else if len(req.Locator.Path) >= 1 {
			schema = req.Locator.Path[0]
			if len(req.Locator.Path) > 1 {
				table = strings.Join(req.Locator.Path[1:], "/")
			}
		}
	} else {
		// 关系型数据库/NoSQL: path = [schema/database, table/collection]
		if len(req.Locator.Path) >= 2 {
			schema = req.Locator.Path[0]
			table = req.Locator.Path[1]
		} else if len(req.Locator.Path) == 1 {
			schema = req.Locator.Path[0]
		}
	}

	page := 1
	pageSize := 20
	if req.Pagination != nil {
		page = req.Pagination.Page
		pageSize = req.Pagination.PageSize
	}

	// 转换 commonModels.Engine 为 models.Engine
	managerEngine := &models.Engine{
		ID:             req.Engine.ID,
		Name:           req.Engine.Name,
		EngineType:     req.Engine.EngineType,
		ConnectionInfo: models.ConnectionInfo(req.Engine.ConnectionInfo),
		Description:    req.Engine.Description,
		IsActive:       req.Engine.IsActive,
		TenantID:       req.Engine.TenantID,
		CreatedBy:      req.Engine.CreatedBy,
		CreatedAt:      req.Engine.CreatedAt,
		UpdatedAt:      req.Engine.UpdatedAt,
	}

	return &PreviewRequest{
		Engine:          managerEngine,
		Schema:          schema,
		Table:           table,
		Page:            page,
		PageSize:        pageSize,
		TenantID:        req.TenantID,
		ItemType:        req.ItemType,
		NodeType:        string(req.Locator.Type),
		PhysicalPath:    req.PhysicalPath,
		ScopePath:       req.ScopePath,
		ChildName:       req.ChildName,
		RefPath:         req.RefPath,
		NestedChildPath: req.NestedChildPath,
		Attributes:      req.MetadataAttributes(),
	}
}

func (req *PreviewResolverRequest) MetadataAttributes() map[string]interface{} {
	if req == nil || req.Metadata == nil || len(req.Metadata.Attributes) == 0 {
		return nil
	}
	return req.Metadata.Attributes
}

func (r *PreviewResolver) resolveProviderByMeta(req *PreviewResolverRequest, legacyReq *PreviewRequest) (PreviewProvider, error) {
	if r == nil || r.registry == nil {
		return nil, ErrNoPreviewProvider
	}
	for _, name := range providerNamesForMeta(req, legacyReq) {
		provider, err := r.registry.GetByName(name)
		if err != nil {
			continue
		}
		return provider, nil
	}
	return nil, ErrNoPreviewProvider
}

func providerNamesForMeta(req *PreviewResolverRequest, legacyReq *PreviewRequest) []string {
	attrs := req.MetadataAttributes()
	itemType := strings.ToLower(strings.TrimSpace(req.ItemType))
	if itemType == "" && req != nil && req.Metadata != nil {
		itemType = strings.ToLower(strings.TrimSpace(req.Metadata.NodeType))
	}
	dataType := strings.ToLower(strings.TrimSpace(catalogutil.StringAttribute(attrs, "data_type")))
	formatName := strings.ToLower(strings.TrimSpace(catalogutil.StringAttribute(attrs, "format")))
	layout := strings.ToLower(strings.TrimSpace(catalogutil.StringAttribute(attrs, "layout")))

	switch itemType {
	case "collection":
		return []string{"builtin:doc-collection"}
	case "label":
		return []string{"builtin:graph-label"}
	case "relationship":
		return []string{"builtin:graph-relationship"}
	}

	if isNodePreview(req, legacyReq) {
		if legacyReq != nil {
			if isFileSystemType(legacyReq.Engine.EngineType) {
				return []string{"builtin:file-catalog"}
			}
			if isObjectStorageType(legacyReq.Engine.EngineType) {
				return []string{"builtin:object-catalog"}
			}
		}
		return []string{"builtin:schema-node"}
	}

	switch dataType {
	case "table":
		if layout == "whole" && hasScopeTableProvider(formatName) {
			return []string{"builtin:scope-table"}
		}
		if legacyReq != nil && isFileTableFormat(formatName) && isContentFileItemType(itemType) {
			return []string{"builtin:file-table"}
		}
		if legacyReq != nil && itemType == "file" {
			return []string{"builtin:file-catalog"}
		}
		return []string{"builtin:database-table"}
	case "graph":
		if itemType == "relationship" {
			return []string{"builtin:graph-relationship"}
		}
		return []string{"builtin:graph-label"}
	case "container":
		if legacyReq != nil && strings.TrimSpace(legacyReq.ChildName) != "" && isContentFileItemType(itemType) {
			return []string{"builtin:container-child"}
		}
		if legacyReq != nil && itemType == "file" {
			return []string{"builtin:file-catalog"}
		}
		return []string{"builtin:object-catalog"}
	case "media", "document":
		if legacyReq != nil && itemType == "file" {
			return []string{"builtin:file-catalog"}
		}
		return []string{"builtin:object-catalog"}
	}

	switch itemType {
	case "table", "view", "materialized_view":
		return []string{"builtin:database-table"}
	case "object":
		return []string{"builtin:object-catalog"}
	case "file":
		return []string{"builtin:file-catalog"}
	}

	return nil
}

func isNodePreview(req *PreviewResolverRequest, legacyReq *PreviewRequest) bool {
	if legacyReq != nil && legacyReq.Table == "" {
		return true
	}
	if req == nil || req.Metadata == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(req.Metadata.NodeType)) {
	case "root", "bucket", "prefix", "schema", "database", "dir", "directory":
		return true
	default:
		return false
	}
}

func isFileTableFormat(formatName string) bool {
	formatType := normalizeFileTableFormat(formatName)
	if formatType == "" || formatType == format.FormatUnknown {
		return false
	}
	if _, err := format.GetTableSampleReader(formatType); err == nil {
		return true
	}
	if _, err := format.GetMultiTableSampleReader(formatType); err == nil {
		return true
	}
	return false
}

func hasScopeTableProvider(formatName string) bool {
	formatType := normalizeFileTableFormat(formatName)
	if formatType == "" || formatType == format.FormatUnknown {
		return false
	}
	_, err := format.GetScopeTableSampleReader(formatType)
	return err == nil
}

// convertToNewResult 将旧的 TablePreview 转换为新的 PreviewResult
func (r *PreviewResolver) convertToNewResult(tablePreview *models.TablePreview, req *PreviewResolverRequest) *PreviewResult {
	// 直接使用完整的 TablePreview 作为 Data
	// 前端插件需要访问 object.content.kind、object.path 等字段来选择合适的预览组件
	result := &PreviewResult{
		PreviewType: tablePreview.Mode,
		Data:        tablePreview,
		Metadata:    r.buildMetadata(req),
	}

	return result
}

// buildMetadata 构建预览元数据
func (r *PreviewResolver) buildMetadata(req *PreviewResolverRequest) *PreviewMetadata {
	metadata := &PreviewMetadata{
		Locator:      req.Locator.ToURI(),
		EngineName:   req.Engine.Name,
		ResourceType: req.Engine.EngineType,
		MetaScanned:  req.Metadata != nil,
	}

	if req.Metadata != nil {
		if req.Metadata.ItemCount > 0 {
			count := int64(req.Metadata.ItemCount)
			metadata.ItemCount = &count
		}
		if req.Metadata.TotalSizeBytes > 0 {
			metadata.SizeBytes = &req.Metadata.TotalSizeBytes
		}
	}

	return metadata
}

// attachItemMeta 将 Meta 元数据附加到预览响应
func (r *PreviewResolver) attachItemMeta(preview *models.TablePreview, req *PreviewResolverRequest) {
	if preview == nil || req == nil || req.Metadata == nil {
		return
	}

	itemType := req.ItemType
	if itemType == "" {
		itemType = req.Metadata.NodeType
	}

	meta := &models.ItemMetadata{
		ItemType:        itemType,
		ItemTypeI18nKey: "engine.term." + itemType,
		FullName:        req.Metadata.FullName,
		Attributes:      mapToMetaAttributes(req.Metadata.Attributes),
		ScannedAt:       req.Metadata.LastScanAt,
	}

	// FullName 兜底
	if meta.FullName == "" {
		meta.FullName = req.Metadata.Path
	}

	preview.ItemMeta = meta
}

func mapToMetaAttributes(attrs map[string]interface{}) []models.MetaAttribute {
	if len(attrs) == 0 {
		return []models.MetaAttribute{}
	}

	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]models.MetaAttribute, 0, len(attrs))
	for _, k := range keys {
		result = append(result, models.MetaAttribute{Key: k, Value: attrs[k]})
	}
	return result
}
