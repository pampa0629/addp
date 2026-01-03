package service

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/plugins"
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
		// 解析 ScanConfig
		var scanConfig models.ScanConfig
		if node.ScanConfig != nil {
			if autoEnabled, ok := node.ScanConfig["auto_enabled"].(bool); ok {
				scanConfig.AutoEnabled = autoEnabled
			}
			if cron, ok := node.ScanConfig["cron"].(string); ok {
				scanConfig.Cron = cron
			}
			if nextScanAt, ok := node.ScanConfig["next_scan_at"].(string); ok {
				scanConfig.NextScanAt = nextScanAt
			}
			if errMsg, ok := node.ScanConfig["error_message"].(string); ok {
				scanConfig.ErrorMessage = errMsg
			}
		}

		item := &models.SchemaWithStatus{
			ID:             node.ID,
			SchemaName:     node.Name,
			ScanStatus:     node.ScanStatus,
			TableCount:     node.ItemCount,
			TotalSizeBytes: node.TotalSizeBytes,
			ScanConfig:     scanConfig,
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

	// 2. 尝试实际连接（快速超时3秒）
	scan, err := plugins.NewScanner(resource)
	if err != nil {
		// 连接失败：触发System刷新状态（异步，不阻塞）
		s.engineService.TriggerConnectionCheck(engineID)

		// 返回失败，附带缓存的状态信息
		if resource.ConnectionStatus == "offline" && resource.CheckMessage != "" {
			return nil, fmt.Errorf("资源离线: %s", resource.CheckMessage)
		}
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	// 3. 获取Schema列表
	schemasInfo, err := scan.ListSchemas()
	if err != nil {
		// 连接成功但查询失败，也触发刷新
		s.engineService.TriggerConnectionCheck(engineID)
		return nil, err
	}

	// 4. 成功：如果之前是offline，触发刷新状态为online
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
func (s *ResourceDiscoveryService) ListObjectStorageNodes(engineID, tenantID uint, path, token string) ([]*models.ObjectNode, error) {
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}

	if !isObjectStorageType(strings.ToLower(resource.EngineType)) {
		return nil, fmt.Errorf("resource %s is not object storage", resource.EngineType)
	}

	// 重构后：直接传入Resource对象，由插件系统管理连接
	scan, err := plugins.NewScanner(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}
	defer scan.Close()

	objectScanner, ok := scan.(plugins.ObjectStorageScanner)
	if !ok {
		return nil, fmt.Errorf("resource %s is not object storage", resource.EngineType)
	}

	nodes, err := objectScanner.ListNodes(path)
	if err != nil {
		return nil, err
	}

	var result []*models.ObjectNode
	for _, node := range nodes {
		item := &models.ObjectNode{
			Name:        node.Name,
			Path:        node.Path,
			Type:        node.Type,
			SizeBytes:   node.SizeBytes,
			FileType:    node.FileType,
			ObjectCount: node.ObjectCount,
		}
		if node.LastModified != nil {
			item.LastModified = node.LastModified.Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}

	return result, nil
}
