package service

import (
	"context"
	"fmt"
	"strings"

	commonClient "github.com/addp/common/client"
	commonFormat "github.com/addp/common/format"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
)

// PreviewResolver 数据预览解析器
// 职责：
// 1. 解析 ResourceLocator URI
// 2. 选择合适的 PreviewProvider（通过 Registry）
// 3. 执行预览
// 4. 组装 PreviewResult（引用 Common 的 TableInfo/ObjectInfo）
//
// 注意：重命名自 PreviewOrchestrator，避免与 Orchestrator 模块混淆
type PreviewResolver struct {
	registry        *PreviewRegistry
	systemClient    *commonClient.SystemClient
	metaClient      *commonClient.MetaClient
	engineConnector *EngineConnector
}

// NewPreviewResolver 创建预览解析器
func NewPreviewResolver(
	registry *PreviewRegistry,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
	engineConnector *EngineConnector,
) *PreviewResolver {
	if registry == nil {
		registry = NewPreviewRegistry()
	}
	if engineConnector == nil {
		engineConnector = NewEngineConnector(systemClient)
	}

	return &PreviewResolver{
		registry:        registry,
		systemClient:    systemClient,
		metaClient:      metaClient,
		engineConnector: engineConnector,
	}
}

// PreviewRequest 新的预览请求（基于 ResourceLocator）
type PreviewResolverRequest struct {
	Locator      *resource.ResourceLocator // 资源定位符
	Engine       *commonModels.Engine       // 引擎信息
	Metadata     *commonModels.MetaNode     // 可选：Meta 节点数据
	Pagination   *Pagination                // 分页参数
	TenantID     *uint                      // 租户 ID
	ItemType     string                     // 数据项类型（如 "lake_table"），来自 MetaItem
	PhysicalPath string                     // 物理路径（来自 meta_item.attributes.physical_path）
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

	// 元数据部分（引用 Common 的 Info 结构）
	TableInfo  *commonFormat.TableInfo  `json:"table_info,omitempty"`  // 当 PreviewType="table"
	ObjectInfo *commonFormat.ObjectInfo `json:"object_info,omitempty"` // 当 PreviewType="object"

	// 预览上下文元数据
	Metadata *PreviewMetadata `json:"metadata"`
}

// PreviewMetadata 预览上下文元数据
type PreviewMetadata struct {
	Locator      string `json:"locator"`        // ResourceLocator URI
	EngineName   string `json:"engine_name"`    // 引擎名称
	ResourceType string `json:"resource_type"`  // 引擎类型（postgresql/minio）
	MetaScanned  bool   `json:"meta_scanned"`   // 是否已被 Meta 扫描
	ItemCount    *int64 `json:"item_count"`     // 项目数（来自 Meta）
	SizeBytes    *int64 `json:"size_bytes"`     // 大小（来自 Meta）
}

// Preview 执行预览
func (r *PreviewResolver) Preview(ctx context.Context, req *PreviewResolverRequest) (*PreviewResult, error) {
	// 1. 验证参数
	if req.Locator == nil {
		return nil, fmt.Errorf("locator is required")
	}
	if req.Engine == nil {
		return nil, fmt.Errorf("engine is required")
	}

	// 2. 尝试从 Meta 获取元数据（可选，失败不影响预览）
	if req.Metadata == nil && req.Locator.MetaID != nil && r.metaClient != nil {
		// TODO: 调用 MetaClient 获取节点元数据
		logger.L().Debug("尝试从 Meta 获取节点元数据", "meta_id", *req.Locator.MetaID)
	}

	// 3. 转换为旧的 PreviewRequest 格式（兼容现有插件）
	legacyReq := r.convertToLegacyRequest(req)

	// 4. 选择预览插件
	provider, err := r.registry.Resolve(legacyReq)
	if err != nil {
		logger.L().Warn("未找到合适的预览插件", "locator", req.Locator.ToURI(), "error", err)
		return &PreviewResult{
			PreviewType: "unsupported",
			Data:        nil,
			Metadata:    r.buildMetadata(req),
		}, nil
	}

	logger.L().Info("选择预览插件", "provider", provider.Name(), "locator", req.Locator.ToURI())

	// 5. 执行预览
	tablePreview, err := provider.Preview(ctx, legacyReq)
	if err != nil {
		return nil, fmt.Errorf("preview failed: %w", err)
	}

	// 6. 组装新的 PreviewResult
	result := r.convertToNewResult(tablePreview, req)

	return result, nil
}

// PreviewFromURI 从 URI 执行预览（便捷方法）
func (r *PreviewResolver) PreviewFromURI(ctx context.Context, locatorURI string, page, pageSize int, tenantID *uint) (*PreviewResult, error) {
	// 1. 解析 Locator
	loc, err := resource.ParseURI(locatorURI)
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

	// 3. 尝试从 Meta 获取元数据（对于对象存储和文件系统类型）
	var metaNode *commonModels.MetaNode
	var metaItem *commonModels.MetaItem
	if r.metaClient != nil && (isObjectStorageType(engine.EngineType) || isFileSystemType(engine.EngineType)) {
		// 设置租户 ID，确保服务间调用时正确过滤
		r.metaClient.SetTenantID(tenantID)

		// 优先通过 meta_id 直接获取节点（更精确，避免路径歧义）
		// 注意：虚拟 ID（>= 100000）对应 MetaItem，需要解码为真实 item ID
		metaIDResolved := false
		if loc.MetaID != nil {
			metaID := *loc.MetaID
			if metaID >= 100000 {
				// 虚拟 ID：MetaItem 的 ID + 100000
				realItemID := metaID - 100000
				item, err := r.metaClient.GetMetaItemByID(realItemID)
				if err == nil && item != nil {
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
		if !metaIDResolved && len(loc.Path) > 0 {
			// 回退到路径查找
			bucketName := loc.Path[0]
			if len(loc.Path) > 1 {
				// 对象路径：bucket/path/to/file.ext
				objectPath := strings.Join(loc.Path[1:], "/")
				item, err := r.metaClient.GetItemByPath(loc.EngineID, bucketName, objectPath)
				if err == nil && item != nil {
					metaItem = item
					logger.L().Debug("从 Meta 获取到对象元数据",
						"bucket", bucketName,
						"path", objectPath,
						"size_bytes", item.SizeBytes)
				} else {
					logger.L().Debug("未从 Meta 获取到对象元数据",
						"bucket", bucketName,
						"path", objectPath,
						"error", err)
				}
			} else {
				// Bucket 或 Prefix 路径
				node, err := r.metaClient.GetNodeByPath(loc.EngineID, bucketName)
				if err == nil && node != nil {
					metaNode = node
					logger.L().Debug("从 Meta 获取到节点元数据",
						"path", bucketName,
						"total_size_bytes", node.TotalSizeBytes)
				}
			}
		}
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
		TenantID: tenantID,
	}

	// 设置元数据（如果有）
	if metaNode != nil {
		req.Metadata = metaNode
		// 从 MetaNode 的 NodeType 推断 ItemType（如 lake_table）
		if metaNode.NodeType != "" && metaNode.NodeType != "directory" && metaNode.NodeType != "bucket" && metaNode.NodeType != "dir" && metaNode.NodeType != "root" {
			req.ItemType = metaNode.NodeType
		}
	} else if metaItem != nil {
		// 将 MetaItem 转换为 MetaNode 格式
		sizeBytes := int64(0)
		if metaItem.SizeBytes != nil {
			sizeBytes = *metaItem.SizeBytes
		}
		req.Metadata = &commonModels.MetaNode{
			ID:             metaItem.ID,
			EngineID:       metaItem.EngineID,
			Path:           metaItem.FullName,
			ItemCount:      1,
			TotalSizeBytes: sizeBytes,
		}
		req.ItemType = metaItem.ItemType
		// 提取 physical_path（仅单文件湖表，mode="file"）
		// 目录型湖表的 physical_path 是目录路径，不能当文件读
		if physPath, ok := metaItem.Attributes["physical_path"].(string); ok && physPath != "" {
			mode, _ := metaItem.Attributes["mode"].(string)
			if mode == "file" {
				req.PhysicalPath = physPath
			}
		}
	}

	// 5. 执行预览
	return r.Preview(ctx, req)
}

// DetectPreviewType 检测预览类型
func (r *PreviewResolver) DetectPreviewType(loc *resource.ResourceLocator, engine *commonModels.Engine) string {
	legacyReq := r.convertToLegacyRequest(&PreviewResolverRequest{
		Locator: loc,
		Engine:  engine,
	})

	provider, err := r.registry.Resolve(legacyReq)
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

	// 对于对象存储类型和文件系统类型，schema 是根名称，table 是完整的子路径
	// 对于关系型数据库，schema 是第一个组件，table 是第二个组件
	if isObjectStorageType(req.Engine.EngineType) || isFileSystemType(req.Engine.EngineType) {
		// 对象存储/文件系统: path = [rootName, ...subPath]
		// schema = rootName, table = subPath (joined)
		if len(req.Locator.Path) >= 1 {
			schema = req.Locator.Path[0] // bucket / 根目录名
			if len(req.Locator.Path) > 1 {
				// 将剩余路径组件合并为完整子路径
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
		Engine:       managerEngine,
		Schema:       schema,
		Table:        table,
		Page:         page,
		PageSize:     pageSize,
		TenantID:     req.TenantID,
		ItemType:     req.ItemType,
		NodeType:     string(req.Locator.Type),
		PhysicalPath: req.PhysicalPath,
	}
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
