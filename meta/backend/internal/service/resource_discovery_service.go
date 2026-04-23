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

	// 5. 尝试文件系统插件（NFS/NAS 等）
	if fsPlugin, ok := p.(plugin.FileSystemPlugin); ok {
		return s.listFileSystemRoots(engineID, resource, fsPlugin)
	}

	return nil, fmt.Errorf("engine %s does not implement RelationalDBPlugin, NoSQLPlugin or FileSystemPlugin", resource.EngineType)
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

// listFileSystemRoots 列出文件系统根节点（NFS/NAS 挂载点）作为 Schema
func (s *ResourceDiscoveryService) listFileSystemRoots(engineID uint, resource *commonModels.Engine, fsPlugin plugin.FileSystemPlugin) ([]*models.SchemaInfo, error) {
	roots, err := fsPlugin.ListRoots(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo))
	if err != nil {
		s.engineService.TriggerConnectionCheck(engineID)
		if resource.ConnectionStatus == "offline" && resource.CheckMessage != "" {
			return nil, fmt.Errorf("资源离线: %s", resource.CheckMessage)
		}
		return nil, fmt.Errorf("failed to list NFS roots: %w", err)
	}

	if resource.ConnectionStatus == "offline" {
		s.engineService.TriggerConnectionCheck(engineID)
	}

	var result []*models.SchemaInfo
	for _, root := range roots {
		result = append(result, &models.SchemaInfo{
			Name: root.Name,
		})
	}
	return result, nil
}

func (s *ResourceDiscoveryService) ListObjectStorageNodes(engineID, tenantID uint, path, token string) ([]*models.ObjectNode, error) {
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}

	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	// ObjectStoragePlugin（MinIO、S3）：用 ListBuckets + ListObjects
	if objPlugin, ok := p.(plugin.ObjectStoragePlugin); ok {
		return s.listObjectStorageNodes(resource, objPlugin, path)
	}

	// FileSystemPlugin（NFS 等）：用 ListRoots + ListDirectory
	if fsPlugin, ok := p.(plugin.FileSystemPlugin); ok {
		return s.listFileSystemNodes(resource, fsPlugin, path)
	}

	return nil, fmt.Errorf("engine %s does not support file/object browsing", resource.EngineType)
}

func (s *ResourceDiscoveryService) listObjectStorageNodes(resource *commonModels.Engine, objPlugin plugin.ObjectStoragePlugin, path string) ([]*models.ObjectNode, error) {
	bucket, prefix := splitObjectPath(path)

	if bucket == "" {
		buckets, err := objPlugin.ListBuckets(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo))
		if err != nil {
			return nil, fmt.Errorf("failed to list buckets: %w", err)
		}
		var result []*models.ObjectNode
		for _, b := range buckets {
			result = append(result, &models.ObjectNode{
				Name: b.Name,
				Path: b.Name,
				Type: "bucket",
			})
		}
		return result, nil
	}

	objects, err := objPlugin.ListObjects(
		context.Background(),
		plugin.ConnectionInfo(resource.ConnectionInfo),
		bucket,
		prefix,
		false,
	)
	if err != nil {
		return nil, err
	}

	var result []*models.ObjectNode
	for _, obj := range objects {
		relativePath := strings.TrimPrefix(obj.Key, prefix)
		if relativePath == "" {
			continue
		}
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

func (s *ResourceDiscoveryService) listFileSystemNodes(resource *commonModels.Engine, fsPlugin plugin.FileSystemPlugin, path string) ([]*models.ObjectNode, error) {
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	// 路径为空：列出根节点
	if path == "" {
		roots, err := fsPlugin.ListRoots(context.Background(), connInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to list roots: %w", err)
		}
		var result []*models.ObjectNode
		for _, r := range roots {
			result = append(result, &models.ObjectNode{
				Name: r.Name,
				Path: r.Path,
				Type: "root",
			})
		}
		return result, nil
	}

	// 列出指定目录内容
	files, dirs, err := fsPlugin.ListDirectory(context.Background(), connInfo, path)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory %s: %w", path, err)
	}

	var result []*models.ObjectNode
	for _, d := range dirs {
		result = append(result, &models.ObjectNode{
			Name: d.Name,
			Path: d.Path,
			Type: "dir",
		})
	}
	for _, f := range files {
		result = append(result, &models.ObjectNode{
			Name:      f.Name,
			Path:      f.Path,
			Type:      "file",
			SizeBytes: f.Size,
			FileType:  filepath.Ext(f.Name),
		})
	}
	return result, nil
}
