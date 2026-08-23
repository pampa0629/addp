package preview

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/instanceprovider"
	"github.com/addp/common/engine/plugin"
	supermapworkflow "github.com/addp/common/engine/plugins/supermap_workflow"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/federatedquery"
	"github.com/addp/common/format"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/engineaccess"
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
	registry                 *PreviewRegistry
	systemClient             *commonClient.SystemClient
	metaClient               *commonClient.MetaClient
	runtimeClient            instanceprovider.RuntimeDescriptorClient
	runtimeClientFactory     func(uint) instanceprovider.RuntimeDescriptorClient
	runtimeListClient        workflowRuntimeDescriptorListClient
	runtimeListClientFactory func(uint) workflowRuntimeDescriptorListClient
}

type workflowRuntimeDescriptorListClient interface {
	ListEngineRuntimeDescriptors(context.Context) ([]commonModels.EngineRuntimeDescriptor, error)
}

// NewPreviewResolver 创建预览解析器
func NewPreviewResolver(
	registry *PreviewRegistry,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
	runtimeClients ...instanceprovider.RuntimeDescriptorClient,
) *PreviewResolver {
	if registry == nil {
		registry = NewPreviewRegistry()
	}

	var runtimeClient instanceprovider.RuntimeDescriptorClient
	if len(runtimeClients) > 0 {
		runtimeClient = runtimeClients[0]
	}
	resolver := &PreviewResolver{
		registry:      registry,
		systemClient:  systemClient,
		metaClient:    metaClient,
		runtimeClient: runtimeClient,
	}
	resolver.runtimeListClient, _ = runtimeClient.(workflowRuntimeDescriptorListClient)
	if systemRuntime, ok := runtimeClient.(*commonClient.SystemServiceClient); ok {
		resolver.runtimeClientFactory = func(tenantID uint) instanceprovider.RuntimeDescriptorClient {
			return systemRuntime.WithTenantID(tenantID)
		}
		resolver.runtimeListClientFactory = func(tenantID uint) workflowRuntimeDescriptorListClient {
			return systemRuntime.WithTenantID(tenantID)
		}
	}
	return resolver
}

func (r *PreviewResolver) runtimeDescriptorListClientForTenant(tenantID *uint) workflowRuntimeDescriptorListClient {
	if r == nil {
		return nil
	}
	if r.runtimeListClient == nil || tenantID == nil || *tenantID == 0 || r.runtimeListClientFactory == nil {
		return r.runtimeListClient
	}
	return r.runtimeListClientFactory(*tenantID)
}

func (r *PreviewResolver) runtimeDescriptorClientForTenant(tenantID *uint) instanceprovider.RuntimeDescriptorClient {
	if r == nil {
		return nil
	}
	if r.runtimeClient == nil || tenantID == nil || *tenantID == 0 || r.runtimeClientFactory == nil {
		return r.runtimeClient
	}
	return r.runtimeClientFactory(*tenantID)
}

// PreviewRequest 新的预览请求（基于 ResourceLocator）
type PreviewResolverRequest struct {
	Locator          *resourcetree.ResourceLocator // 资源定位符
	Engine           *commonModels.Engine          // 引擎信息
	Metadata         *commonModels.MetaNode        // 可选：Meta 节点数据
	Pagination       *Pagination                   // 分页参数
	TenantID         *uint                         // 租户 ID
	MetaItemID       *uint                         // MetaItem ID，只有数据项预览存在
	ItemName         string                        // MetaItem name，只有数据项预览存在
	ItemFullName     string                        // MetaItem full_name，只有数据项预览存在
	ItemFingerprint  string                        // 标准数据项指纹，GenerateItemFingerprint(engine_id, full_name)
	ItemType         string                        // 数据项类型（如 "table"），来自 MetaItem
	ItemRowCount     *int64                        // 表/集合行数，来自 MetaItem.RowCount
	ItemScannedDepth string                        // Meta item 当前扫描深度
	PhysicalPath     string                        // 物理路径（来自 meta_item.attributes.storage.physical_path）
	ScopePath        string                        // 范围路径（来自 meta_item.attributes.storage.physical_path）
	ChildName        string                        // 容器内部 child 名称，例如 Excel sheet
	RefPath          string                        // multi child 内的单个ref 路径，指向容器内原始对象
	NestedChildPath  string                        // 当前 child 是容器时，继续寻址其内部 child 的相对路径
	GraphSample      plugin.GraphSampleFilter      // 图预览样本过滤条件
	DataScope        dataprofile.DataScope         // Manager 剖析内部使用的数据范围；公共预览不接受该参数
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
	Locator         string            `json:"locator"`                    // ResourceLocator URI
	EngineName      string            `json:"engine_name"`                // 引擎名称
	ResourceType    string            `json:"resource_type"`              // 引擎类型（postgresql/minio）
	MetaScanned     bool              `json:"meta_scanned"`               // 是否已被 Meta 扫描
	ScannedDepth    string            `json:"scanned_depth,omitempty"`    // Meta 扫描深度
	ItemID          *uint             `json:"item_id,omitempty"`          // MetaItem ID
	FullName        string            `json:"full_name,omitempty"`        // MetaItem full_name
	ItemFingerprint string            `json:"item_fingerprint,omitempty"` // 标准数据项指纹
	EngineType      string            `json:"engine_type,omitempty"`      // Source Engine 类型
	SchemaCoverage  string            `json:"schema_coverage,omitempty"`  // complete/sampled/unknown
	QueryNames      map[string]string `json:"query_names,omitempty"`      // 按查询语言声明的原生资源名称
	ItemCount       *int64            `json:"item_count"`                 // 项目数（来自 Meta）
	SizeBytes       *int64            `json:"size_bytes"`                 // 大小（来自 Meta）
}

// ResourceFacts 是供生成类 Tool 消费的受限资源事实，不包含原始数据行。
type ResourceFacts struct {
	Locator          string               `json:"locator"`
	EngineID         uint                 `json:"engine_id"`
	EngineName       string               `json:"engine_name"`
	SourceEngineType string               `json:"source_engine_type"`
	ItemID           uint                 `json:"item_id"`
	ItemType         string               `json:"item_type"`
	DataType         string               `json:"data_type"`
	FullName         string               `json:"full_name"`
	ItemFingerprint  string               `json:"item_fingerprint"`
	ScannedDepth     string               `json:"scanned_depth"`
	SchemaCoverage   string               `json:"schema_coverage"`
	QueryNames       map[string]string    `json:"query_names"`
	Fields           []datatype.FieldInfo `json:"fields,omitempty"`
	GeometryColumn   string               `json:"geometry_column,omitempty"`
	GeometryType     string               `json:"geometry_type,omitempty"`
	CRS              string               `json:"crs,omitempty"`
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

	// 3. 转换为Provider 执行请求
	providerReq, err := r.buildProviderRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	// 4. 按 Meta 标准属性确定性选择预览插件
	provider, err := r.resolveProviderByMeta(req, providerReq)
	if err != nil {
		logger.L().Warn("未找到合适的预览插件", "locator", req.Locator.ToURI(), "error", err)
		return &PreviewResult{
			PreviewType: "unsupported",
			Data:        nil,
			Metadata:    r.buildMetadata(req),
		}, nil
	}
	if refProvider := r.resolveRefPreviewProvider(req, providerReq); refProvider != nil {
		provider = refProvider
	}
	if provider.Name() == "builtin:scope-table" {
		if err := r.bindRuntimeScopeTableReader(ctx, req, providerReq); err != nil {
			return nil, err
		}
	}

	logger.L().Info("选择预览插件", "provider", provider.Name(), "locator", req.Locator.ToURI(), "item_type", req.ItemType)

	// 5. 执行预览
	tablePreview, err := provider.Preview(ctx, providerReq)
	if err != nil {
		return nil, fmt.Errorf("preview failed: %w", err)
	}

	// 6. 补充 item_meta（来自 meta）
	r.attachItemMeta(tablePreview, req)

	// 7. 组装新的 PreviewResult
	result := r.buildPreviewResult(tablePreview, req)

	return result, nil
}

func (r *PreviewResolver) resolveRefPreviewProvider(req *PreviewResolverRequest, providerReq *PreviewRequest) PreviewProvider {
	if r == nil || r.registry == nil || req == nil || providerReq == nil {
		return nil
	}
	if strings.TrimSpace(req.ChildName) != "" || strings.TrimSpace(req.RefPath) == "" {
		return nil
	}
	if !isContentFileItemType(providerReq.ItemType) {
		return nil
	}
	if provider, err := r.registry.GetByName("builtin:ref-file"); err == nil {
		return provider
	}
	return nil
}

// PreviewFromURI 从 URI 执行预览（便捷方法）
func (r *PreviewResolver) PreviewFromURI(ctx context.Context, locatorURI string, page, pageSize int, childName string, tenantID *uint) (*PreviewResult, error) {
	return r.PreviewFromURIWithSelection(ctx, locatorURI, page, pageSize, childName, "", "", plugin.GraphSampleFilter{}, tenantID)
}

// ResourceFactsFromURI 只解析 Engine 与 Meta 扫描事实，不调用 PreviewProvider 读取数据行。
func (r *PreviewResolver) ResourceFactsFromURI(ctx context.Context, locatorURI string, tenantID *uint) (*ResourceFacts, error) {
	req, err := r.ResolveRequestFromURIWithSelection(ctx, locatorURI, 1, 1, "", "", "", plugin.GraphSampleFilter{}, tenantID)
	if err != nil {
		return nil, err
	}
	if req.MetaItemID == nil || *req.MetaItemID == 0 {
		return nil, ErrPreviewRequiresScannedMeta
	}
	return r.buildResourceFacts(req)
}

func (r *PreviewResolver) buildResourceFacts(req *PreviewResolverRequest) (*ResourceFacts, error) {
	if req == nil || req.Locator == nil || req.Engine == nil || req.MetaItemID == nil || *req.MetaItemID == 0 {
		return nil, ErrPreviewRequiresScannedMeta
	}
	dataType := itemDataTypeFromMetaAttributes(req.MetadataAttributes())
	if dataType == "" {
		return nil, ErrPreviewRequiresScannedMeta
	}
	metadata := r.buildMetadata(req)
	queryNames := metadata.QueryNames
	if queryNames == nil {
		queryNames = map[string]string{}
	}
	facts := &ResourceFacts{
		Locator:          req.Locator.ToURI(),
		EngineID:         req.Engine.ID,
		EngineName:       req.Engine.Name,
		SourceEngineType: metadata.EngineType,
		ItemID:           *req.MetaItemID,
		ItemType:         req.ItemType,
		DataType:         dataType,
		FullName:         metadata.FullName,
		ItemFingerprint:  metadata.ItemFingerprint,
		ScannedDepth:     metadata.ScannedDepth,
		SchemaCoverage:   metadata.SchemaCoverage,
		QueryNames:       queryNames,
	}
	if tableInfo := tableInfoFromMetaAttributes(req.MetadataAttributes(), req.ItemName); tableInfo != nil {
		fields := tableInfo.Fields
		if len(fields) > 200 {
			fields = fields[:200]
		}
		facts.Fields = append([]datatype.FieldInfo(nil), fields...)
	}
	if spatialInfo := spatialInfoFromMetaAttributes(req.MetadataAttributes()); spatialInfo != nil {
		facts.GeometryColumn = spatialInfo.PrimaryGeometryColumn
		facts.CRS = spatialInfo.CRSRef
		for _, column := range spatialInfo.GeometryColumns {
			if column.Name == facts.GeometryColumn {
				facts.GeometryType = column.GeometryType
				if facts.CRS == "" {
					facts.CRS = column.CRSRef
				}
				break
			}
		}
	}
	return facts, nil
}

func (r *PreviewResolver) PreviewFromURIWithSelection(ctx context.Context, locatorURI string, page, pageSize int, childName, refPath, nestedChildPath string, graphSample plugin.GraphSampleFilter, tenantID *uint) (*PreviewResult, error) {
	req, err := r.ResolveRequestFromURIWithSelection(ctx, locatorURI, page, pageSize, childName, refPath, nestedChildPath, graphSample, tenantID)
	if err != nil {
		return nil, err
	}
	return r.Preview(ctx, req)
}

func (r *PreviewResolver) ResolveRequestFromURIWithSelection(ctx context.Context, locatorURI string, page, pageSize int, childName, refPath, nestedChildPath string, graphSample plugin.GraphSampleFilter, tenantID *uint) (*PreviewResolverRequest, error) {
	// 1. 解析 Locator
	loc, err := resourcetree.ParseURI(locatorURI)
	if err != nil {
		return nil, fmt.Errorf("invalid locator: %w", err)
	}

	// 2. 通过 SystemClient 获取引擎信息
	if r.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	if tenantID == nil || *tenantID == 0 {
		return nil, ErrEngineAccessDenied
	}
	engine, err := r.systemClient.GetEngineForTenant(ctx, *tenantID, loc.EngineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 验证租户权限
	if tenantID != nil && engine.TenantID != nil && *engine.TenantID != *tenantID {
		return nil, ErrEngineAccessDenied
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return nil, err
	}

	// 3. 尝试从 Meta 获取元数据。
	// ResourceLocator 中的真实 node_id / item_id 是最可靠的身份来源；路径型 locator 的 type 只作为路由提示。
	var metaNode *commonModels.MetaNode
	var metaItem *commonModels.MetaItem
	identityResolved := false
	metaClient := r.metaClient
	if metaClient != nil && tenantID != nil {
		metaClient = metaClient.WithTenantID(*tenantID)
	}
	if metaClient != nil {

		if loc.ItemID != nil && *loc.ItemID > 0 {
			itemID := *loc.ItemID
			item, err := metaClient.GetItemByID(itemID)
			if err == nil && item != nil {
				metaItem = item
				identityResolved = true
				logger.L().Debug("从 Meta 通过 item_id 获取到 MetaItem",
					"item_id", itemID,
					"item_type", item.ItemType)
			} else {
				logger.L().Debug("未从 Meta 通过 item_id 获取到 MetaItem",
					"item_id", itemID,
					"error", err)
				return nil, ErrPreviewRequiresScannedMeta
			}
		} else if loc.NodeID != nil && *loc.NodeID > 0 {
			nodeID := *loc.NodeID
			node, err := metaClient.GetNodeByID(nodeID)
			if err == nil && node != nil {
				metaNode = node
				identityResolved = true
				logger.L().Debug("从 Meta 通过 node_id 获取到节点元数据",
					"node_id", nodeID,
					"node_type", node.NodeType,
					"total_size_bytes", node.TotalSizeBytes)
			} else {
				logger.L().Debug("未从 Meta 通过 node_id 获取到节点元数据",
					"node_id", nodeID,
					"error", err)
				return nil, ErrPreviewRequiresScannedMeta
			}
		}
	}

	if metaClient != nil {
		if !identityResolved && len(loc.Path) > 0 {
			catalogPath := strings.Join(loc.Path, "/")
			if isPreviewItemLocator(loc) {
				item, err := metaClient.GetItemByCatalogPath(loc.EngineID, catalogPath)
				if err == nil && item != nil {
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
				node, err := metaClient.GetNodeByCatalogPath(loc.EngineID, catalogPath)
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
	if metaItem != nil {
		itemID := metaItem.ID
		loc.ItemID = &itemID
		if strings.TrimSpace(metaItem.ItemType) != "" {
			loc.Type = resourcetree.ResourceType(metaItem.ItemType)
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
		GraphSample:     graphSample.Clone(),
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
		itemID := metaItem.ID
		// 将 MetaItem 转换为 MetaNode 格式
		sizeBytes := int64(0)
		if metaItem.SizeBytes != nil {
			sizeBytes = *metaItem.SizeBytes
		}
		attrs := cloneMetaAttributes(metaItem.Attributes)
		req.MetaItemID = &itemID
		req.ItemName = metaItem.Name
		req.ItemFullName = metaItem.FullName
		req.ItemFingerprint = commonModels.GenerateItemFingerprint(metaItem.EngineID, metaItem.FullName)
		req.ItemRowCount = metaItem.RowCount
		req.ItemScannedDepth = metaItem.ScannedDepth
		req.Metadata = &commonModels.MetaNode{
			ID:             metaItem.ID,
			EngineID:       metaItem.EngineID,
			Path:           metaItem.FullName,
			FullName:       metaItem.FullName,
			ScannedDepth:   metaItem.ScannedDepth,
			ItemCount:      1,
			TotalSizeBytes: sizeBytes,
			LastScanAt:     metaItem.ScannedAt,
			Attributes:     attrs,
		}
		req.ItemType = metaItem.ItemType
		req.PhysicalPath, req.ScopePath = previewResourcePaths(attrs)
	}

	return req, nil
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

func previewResourcePaths(attrs map[string]interface{}) (physicalPath string, scopePath string) {
	physPath := physicalPathFromMetaAttributes(attrs)
	if physPath == "" {
		return "", ""
	}
	switch itemLayoutFromMetaAttributes(attrs) {
	case "single":
		return physPath, ""
	case "whole":
		return "", physPath
	default:
		return "", ""
	}
}

func isPreviewItemLocator(loc *resourcetree.ResourceLocator) bool {
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
	case "table", "view", "materialized_view", "collection", "graph", "topic", "object", "file":
		return true
	default:
		return false
	}
}

// DetectPreviewType 检测预览类型
func (r *PreviewResolver) DetectPreviewType(loc *resourcetree.ResourceLocator, engine *commonModels.Engine) string {
	req := &PreviewResolverRequest{
		Locator: loc,
		Engine:  engine,
	}
	if loc != nil {
		req.ItemType = strings.ToLower(strings.TrimSpace(string(loc.Type)))
	}
	providerReq, err := r.buildProviderRequest(context.Background(), req)
	if err != nil {
		return "unsupported"
	}

	provider, err := r.resolveProviderByMeta(req, providerReq)
	if err != nil {
		return "unsupported"
	}

	return provider.Name()
}

// 内部辅助方法

// buildProviderRequest 根据 Resolver 请求构造 PreviewProvider 执行请求
func (r *PreviewResolver) buildProviderRequest(ctx context.Context, req *PreviewResolverRequest) (*PreviewRequest, error) {
	schema := ""
	table := ""
	providerPath := plugin.CatalogPath{}

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

	if ctx == nil {
		ctx = context.Background()
	}
	plug, err := instanceprovider.Resolve(ctx, r.runtimeDescriptorClientForTenant(req.TenantID), req.Engine, supermapworkflow.RequiredTableReadOperators()...)
	if err != nil {
		return nil, err
	}
	modelProvider, ok := plug.(plugin.CatalogModelProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement CatalogModelProvider", req.Engine.EngineType)
	}
	providerPath, err = resourcetree.ProviderCatalogPathFromLocator(modelProvider.CatalogModel(), req.Locator)
	if err != nil {
		return nil, fmt.Errorf("failed to build provider catalog path: %w", err)
	}

	// 转换 commonModels.Engine 为 models.Engine
	managerEngine := &models.Engine{
		ID:             req.Engine.ID,
		Name:           req.Engine.Name,
		EngineType:     req.Engine.EngineType,
		ConnectionInfo: models.ConnectionInfo(req.Engine.ConnectionInfo),
		Description:    req.Engine.Description,
		LifecycleState: req.Engine.LifecycleState,
		TenantID:       req.Engine.TenantID,
		CreatedBy:      req.Engine.CreatedBy,
		CreatedAt:      req.Engine.CreatedAt,
		UpdatedAt:      req.Engine.UpdatedAt,
	}

	return &PreviewRequest{
		Locator:         req.Locator.ToURI(),
		Engine:          managerEngine,
		EnginePlugin:    plug,
		Schema:          schema,
		Table:           table,
		Page:            page,
		PageSize:        pageSize,
		TenantID:        req.TenantID,
		ItemFingerprint: req.ItemFingerprint,
		ItemType:        req.ItemType,
		ItemRowCount:    req.ItemRowCount,
		ScannedDepth:    req.scannedDepth(),
		NodeType:        string(req.Locator.Type),
		ProviderPath:    providerPath,
		PhysicalPath:    req.PhysicalPath,
		ScopePath:       req.ScopePath,
		ChildName:       req.ChildName,
		RefPath:         req.RefPath,
		NestedChildPath: req.NestedChildPath,
		GraphSample:     req.GraphSample.Clone(),
		DataScope:       req.DataScope,
		Attributes:      req.MetadataAttributes(),
	}, nil
}

func (req *PreviewResolverRequest) MetadataAttributes() map[string]interface{} {
	if req == nil || req.Metadata == nil || len(req.Metadata.Attributes) == 0 {
		return nil
	}
	return req.Metadata.Attributes
}

func (r *PreviewResolver) resolveProviderByMeta(req *PreviewResolverRequest, providerReq *PreviewRequest) (PreviewProvider, error) {
	if r == nil || r.registry == nil {
		return nil, ErrNoPreviewProvider
	}
	for _, name := range providerNamesForMeta(req, providerReq) {
		provider, err := r.registry.GetByName(name)
		if err != nil {
			continue
		}
		return provider, nil
	}
	return nil, ErrNoPreviewProvider
}

func providerNamesForMeta(req *PreviewResolverRequest, providerReq *PreviewRequest) []string {
	attrs := req.MetadataAttributes()
	itemType := strings.ToLower(strings.TrimSpace(req.ItemType))
	if itemType == "" && req != nil && req.Metadata != nil {
		itemType = strings.ToLower(strings.TrimSpace(req.Metadata.NodeType))
	}
	dataType := itemDataTypeFromMetaAttributes(attrs)
	formatType := formatTypeFromMetaAttributes(attrs)
	layout := itemLayoutFromMetaAttributes(attrs)

	switch itemType {
	case "collection":
		return []string{"builtin:dynamic-schema-collection"}
	case "graph":
		return []string{"builtin:graph"}
	case "topic":
		return []string{"builtin:event-stream-topic"}
	}

	if isNodePreview(req, providerReq) {
		if providerReq != nil {
			if isFileSystemType(providerReq.Engine.EngineType) {
				return []string{"builtin:file-catalog"}
			}
			if isObjectStorageType(providerReq.Engine.EngineType) {
				return []string{"builtin:object-catalog"}
			}
		}
		return []string{"builtin:schema-node"}
	}

	switch dataType {
	case "table":
		if layout == "whole" && hasScopeTablePreviewProvider(formatType) {
			return []string{"builtin:scope-table"}
		}
		if providerReq != nil && isFileTableFormat(formatType, attrs) && isContentFileItemType(itemType) {
			return []string{"builtin:file-table"}
		}
		if providerReq != nil && itemType == "file" {
			return []string{"builtin:file-catalog"}
		}
		return []string{"builtin:database-table"}
	case "graph":
		return []string{"builtin:graph"}
	case "container":
		if providerReq != nil && strings.TrimSpace(providerReq.ChildName) != "" && isContentFileItemType(itemType) {
			// Some physical single-file containers (for example PGeo .mdb)
			// expose table children through the runtime scope-table capability.
			// Layout describes the physical resource, not whether a selected
			// child can be read as a scope table, so it must not gate this route.
			if hasScopeTablePreviewProvider(formatType) {
				return []string{"builtin:scope-table"}
			}
			return []string{"builtin:container-child"}
		}
		if providerReq != nil && itemType == "file" {
			return []string{"builtin:file-catalog"}
		}
		return []string{"builtin:object-catalog"}
	case "media", "document":
		if providerReq != nil && itemType == "file" {
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

func isNodePreview(req *PreviewResolverRequest, providerReq *PreviewRequest) bool {
	if providerReq != nil && providerReq.Table == "" {
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

func isFileTableFormat(formatType format.FormatType, attrs map[string]interface{}) bool {
	if formatType == "" || formatType == format.FormatUnknown {
		return false
	}
	if _, err := format.GetTableSampleReader(formatType); err == nil {
		if _, infoErr := format.GetTableInfoProvider(formatType); infoErr == nil {
			return true
		}
		return tableInfoFromMetaAttributes(attrs, "table") != nil
	}
	if _, err := format.GetMultiTableSampleReader(formatType); err == nil {
		_, infoErr := format.GetMultiTableInfoProvider(formatType)
		return infoErr == nil
	}
	return false
}

func hasScopeTablePreviewProvider(formatType format.FormatType) bool {
	if formatType == "" || formatType == format.FormatUnknown {
		return false
	}
	if _, err := format.GetScopeTableSampleReader(formatType); err == nil {
		return true
	}
	_, err := format.GetRuntimeScopeTableReaderFactory(formatType)
	return err == nil
}

func (r *PreviewResolver) bindRuntimeScopeTableReader(ctx context.Context, req *PreviewResolverRequest, providerReq *PreviewRequest) error {
	if req == nil || providerReq == nil || providerReq.ScopeTableReaderProvider != nil {
		return nil
	}
	formatType := resolveScopeTableFormat(providerReq)
	factory, err := format.GetRuntimeScopeTableReaderFactory(formatType)
	if err != nil {
		return nil
	}
	if req.TenantID == nil || *req.TenantID == 0 {
		return ErrEngineAccessDenied
	}
	client := r.runtimeDescriptorListClientForTenant(req.TenantID)
	if client == nil {
		return fmt.Errorf("System runtime descriptor client is required to preview %s", formatType)
	}
	descriptors, err := client.ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return fmt.Errorf("list workflow runtime descriptors for %s preview: %w", formatType, err)
	}
	source, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
		Engine: req.Engine, Locator: req.Locator, Kind: runtimeScopeTableSourceKind(providerReq.Attributes), Format: string(formatType),
	})
	if err != nil {
		return fmt.Errorf("resolve %s preview source access: %w", formatType, err)
	}
	plan, err := workflowaccess.NewSourcePlan(source)
	if err != nil {
		return err
	}
	sort.SliceStable(descriptors, func(left, right int) bool {
		if descriptors[left].IsBuiltin != descriptors[right].IsBuiltin {
			return !descriptors[left].IsBuiltin
		}
		return descriptors[left].ID < descriptors[right].ID
	})
	failures := make([]string, 0)
	for index := range descriptors {
		runtimeEngine := descriptors[index].AsEngine()
		if runtimeEngine == nil || runtimeEngine.LifecycleState != commonModels.EngineLifecycleActive {
			continue
		}
		if err := dbbridge.RequireDirectWorkflowOperators(ctx, runtimeEngine, factory.RequiredScopeTableReadOperators()...); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", runtimeEngine.Name, err))
			continue
		}
		runtimeProvider, err := dbbridge.WorkflowRuntimeProviderForEngine(runtimeEngine)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", runtimeEngine.Name, err))
			continue
		}
		bound, err := factory.BindScopeTableReader(runtimeProvider, plugin.ConnectionInfo(runtimeEngine.ConnectionInfo), plan)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", runtimeEngine.Name, err))
			continue
		}
		providerReq.ScopeTableReaderProvider = bound
		return nil
	}
	message := fmt.Sprintf("no active workflow runtime provides %s preview operators %s", formatType, strings.Join(factory.RequiredScopeTableReadOperators(), ", "))
	if len(failures) > 0 {
		message += "; discovery failures: " + strings.Join(failures, "; ")
	}
	return fmt.Errorf("%s", message)
}

func runtimeScopeTableSourceKind(attrs map[string]interface{}) string {
	if itemLayoutFromMetaAttributes(attrs) == format.LayoutWhole {
		return workflowaccess.KindDirectory
	}
	return workflowaccess.KindFile
}

// buildPreviewResult 将 provider 返回的预览内容包装为统一 PreviewResult。
func (r *PreviewResolver) buildPreviewResult(tablePreview *models.TablePreview, req *PreviewResolverRequest) *PreviewResult {
	// 直接使用完整的 TablePreview 作为 Data
	// 前端插件需要访问 object.content.kind、object.path 等字段来选择合适的预览组件
	result := &PreviewResult{
		PreviewType: tablePreview.Mode,
		Data:        tablePreview,
		Metadata:    r.buildMetadata(req),
	}
	appendPreviewAdvisory(tablePreview, refreshItemAdvisoryForRequest(req))

	return result
}

// buildMetadata 构建预览元数据
func (r *PreviewResolver) buildMetadata(req *PreviewResolverRequest) *PreviewMetadata {
	metadata := &PreviewMetadata{
		Locator:         req.Locator.ToURI(),
		EngineName:      req.Engine.Name,
		ResourceType:    req.Engine.EngineType,
		MetaScanned:     req.Metadata != nil,
		ScannedDepth:    req.scannedDepth(),
		ItemID:          req.MetaItemID,
		FullName:        strings.TrimSpace(req.ItemFullName),
		ItemFingerprint: strings.TrimSpace(req.ItemFingerprint),
		EngineType:      strings.ToLower(strings.TrimSpace(req.Engine.EngineType)),
		SchemaCoverage:  schemaCoverage(req),
		QueryNames:      queryNames(req),
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

func schemaCoverage(req *PreviewResolverRequest) string {
	if req == nil || req.MetaItemID == nil {
		return "unknown"
	}
	if strings.EqualFold(strings.TrimSpace(req.Engine.EngineType), "mongodb") {
		return "sampled"
	}
	if strings.TrimSpace(req.scannedDepth()) == "" || strings.EqualFold(req.scannedDepth(), "none") {
		return "unknown"
	}
	return "complete"
}

func queryNames(req *PreviewResolverRequest) map[string]string {
	if req == nil || req.MetaItemID == nil || strings.TrimSpace(req.ItemFullName) == "" {
		return nil
	}
	fullName := strings.TrimSpace(req.ItemFullName)
	switch strings.ToLower(strings.TrimSpace(req.Engine.EngineType)) {
	case "mongodb":
		if name := strings.TrimSpace(req.ItemName); name != "" {
			return map[string]string{"mql": name}
		}
		return nil
	case "neo4j":
		return map[string]string{"cypher": fullName}
	case "postgresql", "postgres", "postgis", "mysql", "oracle", "doris", "clickhouse", "duckdb":
		return map[string]string{
			"sql":           fullName,
			"federated_sql": federatedquery.SanitizeIdentifier(req.Engine.Name) + "." + fullName,
		}
	case "minio", "s3":
		if name := strings.TrimSpace(req.ItemName); name != "" {
			return map[string]string{
				"federated_sql": federatedquery.SanitizeIdentifier(req.Engine.Name) + "." + federatedquery.SanitizeIdentifier(name),
			}
		}
		return nil
	default:
		return nil
	}
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

	meta := &models.CatalogFacts{
		ItemType:        itemType,
		ItemTypeI18nKey: "engine.term." + itemType,
		FullName:        req.Metadata.FullName,
		RowCount:        req.ItemRowCount,
		Attributes:      mapToMetaAttributes(req.Metadata.Attributes),
		ScannedAt:       req.Metadata.LastScanAt,
		ScannedDepth:    req.scannedDepth(),
	}

	// FullName 兜底
	if meta.FullName == "" {
		meta.FullName = req.Metadata.Path
	}

	preview.ItemMeta = meta
}

func (req *PreviewResolverRequest) scannedDepth() string {
	if req == nil {
		return ""
	}
	if strings.TrimSpace(req.ItemScannedDepth) != "" {
		return strings.TrimSpace(req.ItemScannedDepth)
	}
	if req.Metadata != nil {
		return strings.TrimSpace(req.Metadata.ScannedDepth)
	}
	return ""
}

func refreshItemAdvisoryForRequest(req *PreviewResolverRequest) *models.PreviewAdvisory {
	if req == nil || req.Metadata == nil {
		return nil
	}
	if !isPreviewItemType(strings.ToLower(strings.TrimSpace(req.ItemType))) {
		return nil
	}
	if strings.EqualFold(req.scannedDepth(), "deep") {
		return nil
	}
	return &models.PreviewAdvisory{
		Code:     "item_refresh_recommended",
		Severity: "info",
		Action:   "item_refresh",
	}
}

func appendPreviewAdvisory(preview *models.TablePreview, advisory *models.PreviewAdvisory) {
	if preview == nil || advisory == nil || strings.TrimSpace(advisory.Code) == "" {
		return
	}
	for _, existing := range preview.Advisories {
		if existing.Code == advisory.Code && existing.Action == advisory.Action {
			return
		}
	}
	preview.Advisories = append(preview.Advisories, *advisory)
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
