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

func (s *ResourceDiscoveryService) ListObjectStorageNodes(engineID, tenantID uint, path, token string) ([]*models.ObjectNode, error) {
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}

	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	// CatalogProvider：统一浏览真实 catalog 节点
	if catalogProvider, ok := p.(plugin.CatalogProvider); ok {
		return s.listCatalogObjectNodes(resource, catalogProvider, path)
	}

	return nil, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
}

func (s *ResourceDiscoveryService) listCatalogObjectNodes(resource *commonModels.Engine, catalogProvider plugin.CatalogProvider, path string) ([]*models.ObjectNode, error) {
	parent := catalogPathFromBrowserPath(resource.ID, path, storageFamily(catalogProvider))
	nodes, err := catalogProvider.ListChildren(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo), parent, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]*models.ObjectNode, 0, len(nodes))
	for _, node := range nodes {
		nodePath := node.Path.StringPath()
		if raw, ok := node.Attributes["path"].(string); ok && raw != "" {
			nodePath = raw
		}
		item := &models.ObjectNode{
			Name: node.Name,
			Path: nodePath,
			Type: catalogNodeBrowserType(node),
		}
		if size, ok := int64Stat(node.Stats, "size_bytes"); ok {
			item.SizeBytes = size
		}
		if item.Type == "file" || item.Type == "object" {
			item.FileType = filepath.Ext(node.Name)
		}
		result = append(result, item)
	}
	return result, nil
}

func catalogPathFromBrowserPath(engineID uint, path, family string) plugin.CatalogPath {
	if family == "file" {
		return catalogPathFromFileBrowserPath(engineID, path)
	}
	return catalogPathFromObjectBrowserPath(engineID, path)
}

func catalogPathFromFileBrowserPath(engineID uint, path string) plugin.CatalogPath {
	catalogPath := plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
	}
	trimmed := strings.Trim(path, "/")
	if path == "" {
		return catalogPath
	}
	catalogPath.Segments = append(catalogPath.Segments, plugin.CatalogSegment{
		Term: plugin.CatalogTermRoot,
		Kind: plugin.CatalogKindRoot,
		Name: "/",
	})
	if trimmed == "" || trimmed == "." {
		return catalogPath
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" {
			continue
		}
		catalogPath.Segments = append(catalogPath.Segments, plugin.CatalogSegment{
			Term: plugin.CatalogTermPath,
			Kind: plugin.CatalogKindPrefix,
			Name: part,
		})
	}
	return catalogPath
}

func catalogPathFromObjectBrowserPath(engineID uint, path string) plugin.CatalogPath {
	catalogPath := plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
	}
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return catalogPath
	}

	parts := strings.Split(trimmed, "/")
	for i, part := range parts {
		term := plugin.CatalogTermPrefix
		kind := plugin.CatalogKindPrefix
		if i == 0 {
			term = plugin.CatalogTermBucket
			kind = plugin.CatalogKindBucket
		}
		catalogPath.Segments = append(catalogPath.Segments, plugin.CatalogSegment{
			Term: term,
			Kind: kind,
			Name: part,
		})
	}
	return catalogPath
}

func catalogNodeBrowserType(node plugin.CatalogNode) string {
	switch node.Kind {
	case plugin.CatalogKindBucket:
		return "bucket"
	case plugin.CatalogKindRoot:
		return "root"
	case plugin.CatalogKindPrefix:
		return "prefix"
	case plugin.CatalogKindObject:
		return "object"
	case plugin.CatalogKindFile:
		return "file"
	default:
		if node.IsContainer {
			return "prefix"
		}
		return "object"
	}
}

func int64Stat(stats map[string]interface{}, key string) (int64, bool) {
	if stats == nil {
		return 0, false
	}
	switch v := stats[key].(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}
