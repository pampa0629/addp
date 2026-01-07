package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// ResourceDiscoveryService 资源发现服务
// 提供实时查询资源信息的接口（非扫描元数据）
type ResourceDiscoveryService struct {
	db            *gorm.DB
	log           *slog.Logger
	engineService *EngineService
}

// NewResourceDiscoveryService 创建资源发现服务
func NewResourceDiscoveryService(db *gorm.DB, engineService *EngineService, log *slog.Logger) *ResourceDiscoveryService {
	return &ResourceDiscoveryService{
		db:            db,
		log:           log,
		engineService: engineService,
	}
}

// ============================================================================
// 资源发现接口
// ============================================================================

func (s *ResourceDiscoveryService) GetSchemasByResource(engineID, tenantID uint) ([]*models.SchemaWithStatus, error) {
	var nodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL", tenantID, engineID).
		Order("name").
		Find(&nodes).Error; err != nil {
		return nil, err
	}

	result := make([]*models.SchemaWithStatus, 0, len(nodes))
	for _, node := range nodes {
		item := &models.SchemaWithStatus{
			ID:             node.ID,
			SchemaName:     node.Name,
			ScanStatus:     node.ScanStatus,
			TableCount:     node.ItemCount,
			TotalSizeBytes: node.TotalSizeBytes,
			ErrorMessage:   node.ScanError,
		}
		if node.ScannedAt != nil {
			item.ScannedAt = node.ScannedAt.Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}

	return result, nil
}
func (s *ResourceDiscoveryService) ListAvailableSchemas(engineID, tenantID uint, token string) ([]*models.SchemaInfo, error) {
	// 1. 获取资源（从System读取，包含connection_status）
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}

	// 2. 获取插件
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		// 连接失败：触发System刷新状态（异步，不阻塞）
		s.engineService.TriggerConnectionCheck(engineID)
		if resource.ConnectionStatus == "offline" && resource.CheckMessage != "" {
			return nil, fmt.Errorf("资源离线: %s", resource.CheckMessage)
		}
		return nil, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	// 3. 尝试关系型数据库插件
	if relPlugin, ok := p.(plugin.RelationalDBPlugin); ok {
		return s.listRelationalSchemas(engineID, resource, relPlugin)
	}

	// 4. 尝试 NoSQL 数据库插件
	if nosqlPlugin, ok := p.(plugin.NoSQLPlugin); ok {
		return s.listNoSQLSchemas(engineID, resource, nosqlPlugin)
	}

	return nil, fmt.Errorf("engine %s does not implement RelationalDBPlugin or NoSQLPlugin", resource.EngineType)
}

// listRelationalSchemas 列出关系型数据库的 Schema
func (s *ResourceDiscoveryService) listRelationalSchemas(engineID uint, resource *commonModels.Engine, relPlugin plugin.RelationalDBPlugin) ([]*models.SchemaInfo, error) {
	db, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
		ID:             resource.ID,
		EngineType:     resource.EngineType,
		ConnectionInfo: plugin.ConnectionInfo(resource.ConnectionInfo),
	}, nil)
	if err != nil {
		// 连接失败：触发System刷新状态（异步，不阻塞）
		s.engineService.TriggerConnectionCheck(engineID)
		if resource.ConnectionStatus == "offline" && resource.CheckMessage != "" {
			return nil, fmt.Errorf("资源离线: %s", resource.CheckMessage)
		}
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// 获取Schema列表
	schemasInfo, err := relPlugin.ListSchemas(context.Background(), db)
	if err != nil {
		// 连接成功但查询失败，也触发刷新
		s.engineService.TriggerConnectionCheck(engineID)
		return nil, err
	}

	// 成功：如果之前是offline，触发刷新状态为online
	if resource.ConnectionStatus == "offline" {
		s.engineService.TriggerConnectionCheck(engineID)
	}

	// 转换并返回
	var result []*models.SchemaInfo
	for _, info := range schemasInfo {
		result = append(result, &models.SchemaInfo{
			Name: info.Name,
		})
	}
	return result, nil
}

// listNoSQLSchemas 列出 NoSQL 数据库的 Database（作为 Schema）
func (s *ResourceDiscoveryService) listNoSQLSchemas(engineID uint, resource *commonModels.Engine, nosqlPlugin plugin.NoSQLPlugin) ([]*models.SchemaInfo, error) {
	// 对于 NoSQL，Database 对应 Schema
	databases, err := nosqlPlugin.ListDatabases(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo))
	if err != nil {
		// 连接失败：触发System刷新状态（异步，不阻塞）
		s.engineService.TriggerConnectionCheck(engineID)
		if resource.ConnectionStatus == "offline" && resource.CheckMessage != "" {
			return nil, fmt.Errorf("资源离线: %s", resource.CheckMessage)
		}
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}

	// 成功：如果之前是offline，触发刷新状态为online
	if resource.ConnectionStatus == "offline" {
		s.engineService.TriggerConnectionCheck(engineID)
	}

	// 转换并返回
	var result []*models.SchemaInfo
	for _, db := range databases {
		result = append(result, &models.SchemaInfo{
			Name: db.Name,
		})
	}

	return result, nil
}
func (s *ResourceDiscoveryService) ListObjectStorageNodes(engineID, tenantID uint, path, token string) ([]*models.ObjectNode, error) {
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}

	if !isObjectStorageType(strings.ToLower(resource.EngineType)) {
		return nil, fmt.Errorf("resource %s is not object storage", resource.EngineType)
	}

	// ✅ 重构后：直接使用 ObjectStoragePlugin
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	objPlugin, ok := p.(plugin.ObjectStoragePlugin)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement ObjectStoragePlugin", resource.EngineType)
	}

	// 解析路径：bucket/prefix
	bucket, prefix := splitObjectPath(path)

	// 🔧 如果 path 为空，列出所有 buckets（根级别）
	if bucket == "" {
		buckets, err := objPlugin.ListBuckets(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo))
		if err != nil {
			return nil, fmt.Errorf("failed to list buckets: %w", err)
		}

		var result []*models.ObjectNode
		for _, bucketInfo := range buckets {
			result = append(result, &models.ObjectNode{
				Name: bucketInfo.Name,
				Path: bucketInfo.Name,
				Type: "bucket",
			})
		}
		return result, nil
	}

	// 非递归列出对象（用于目录浏览）
	objects, err := objPlugin.ListObjects(
		context.Background(),
		plugin.ConnectionInfo(resource.ConnectionInfo),
		bucket,
		prefix,
		false, // 非递归
	)
	if err != nil {
		return nil, err
	}

	// 转换为 ObjectNode 格式
	var result []*models.ObjectNode
	for _, obj := range objects {
		// 计算相对路径和节点名称
		relativePath := strings.TrimPrefix(obj.Key, prefix)
		if relativePath == "" {
			continue // 跳过空路径
		}

		// 判断是目录还是文件
		nodeType := "object"
		name := filepath.Base(obj.Key)
		if strings.HasSuffix(obj.Key, "/") {
			nodeType = "prefix"
			name = strings.TrimSuffix(name, "/")
		}

		item := &models.ObjectNode{
			Name:        name,
			Path:        obj.Key,
			Type:        nodeType,
			SizeBytes:   obj.Size,
			FileType:    filepath.Ext(obj.Key),
			ObjectCount: 1,
		}
		if !obj.LastModified.IsZero() {
			item.LastModified = obj.LastModified.Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}

	return result, nil
}
