package service

import (
	"errors"
	"fmt"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

// ============================================================================
// TileServiceService 瓦片服务业务逻辑层
// ============================================================================

type TileServiceService struct {
	repo    *repository.TileServiceRepository
	baseURL string // 服务的基础URL（用于构建端点）
}

func NewTileServiceService(repo *repository.TileServiceRepository, baseURL string) *TileServiceService {
	return &TileServiceService{
		repo:    repo,
		baseURL: baseURL,
	}
}

// ============================================================================
// 瓦片服务 CRUD
// ============================================================================

// CreateService 创建瓦片服务（包括第一个图层）
func (s *TileServiceService) CreateService(req *models.CreateTileServiceRequest, tenantID uint, createdBy uint) (*models.TileServiceDTO, error) {
	// 1. 验证服务名称唯一性
	unique, err := s.repo.CheckServiceNameUnique(req.ServiceName, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("check service name failed: %w", err)
	}
	if !unique {
		return nil, errors.New("service name already exists")
	}

	// 2. 验证第一个图层配置
	if req.FirstLayer == nil {
		return nil, errors.New("first_layer is required")
	}
	if err := s.validateLayerConfig(req.FirstLayer.LayerType, req.FirstLayer.LayerConfig); err != nil {
		return nil, fmt.Errorf("invalid first layer config: %w", err)
	}

	// 3. 设置默认值
	defaultSRID := req.DefaultSRID
	if defaultSRID == 0 {
		defaultSRID = 3857 // Web Mercator
	}

	// 4. 构建协议配置
	protocols := s.buildProtocolsConfig(req.Protocols)

	// 5. 创建服务模型
	service := &models.TileService{
		TenantID:     tenantID,
		ServiceName:  req.ServiceName,
		Title:        req.Title,
		Description:  req.Description,
		Keywords:     models.StringArray(req.Keywords),
		DefaultSRID:  defaultSRID,
		Protocols:    protocols,
		PublicAccess: req.PublicAccess,
		Status:       "active",
		CreatedBy:    createdBy,
	}

	// 6. 创建第一个图层模型
	firstLayer := &models.TileServiceLayer{
		LayerName:    req.FirstLayer.LayerName,
		Title:        req.FirstLayer.Title,
		Description:  req.FirstLayer.Description,
		LayerType:    req.FirstLayer.LayerType,
		LayerConfig:  req.FirstLayer.LayerConfig,
		DisplayOrder: 0,
		Enabled:      true,
	}

	// 7. 使用事务创建服务和第一个图层
	// 注意：GORM 会自动通过 ForeignKey 设置 ServiceID
	service.Layers = []models.TileServiceLayer{*firstLayer}

	if err := s.repo.CreateService(service); err != nil {
		return nil, fmt.Errorf("create service failed: %w", err)
	}

	// 8. 重新加载服务（获取完整数据，包括自动生成的 ID）
	service, err = s.repo.GetServiceByID(service.ID)
	if err != nil {
		return nil, fmt.Errorf("get created service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// GetService 获取瓦片服务详情
func (s *TileServiceService) GetService(id uint) (*models.TileServiceDTO, error) {
	service, err := s.repo.GetServiceByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}
	return s.convertToDTO(service), nil
}

// GetServiceByName 根据名称获取服务（不过滤租户，用于公开服务）
func (s *TileServiceService) GetServiceByName(serviceName string) (*models.TileServiceDTO, error) {
	service, err := s.repo.GetServiceByName(serviceName)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}
	return s.convertToDTO(service), nil
}

// GetServiceByNameAndTenant 根据名称和租户获取服务
func (s *TileServiceService) GetServiceByNameAndTenant(serviceName string, tenantID uint) (*models.TileServiceDTO, error) {
	service, err := s.repo.GetServiceByNameAndTenant(serviceName, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}
	return s.convertToDTO(service), nil
}

// GetServiceModelByName 获取服务模型（用于瓦片端点）
func (s *TileServiceService) GetServiceModelByName(serviceName string) (*models.TileService, error) {
	return s.repo.GetServiceByName(serviceName)
}

// ListServices 列出租户下的所有瓦片服务
func (s *TileServiceService) ListServices(tenantID uint, offset int, limit int) ([]models.TileServiceDTO, int64, error) {
	services, total, err := s.repo.ListServices(tenantID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list services failed: %w", err)
	}

	dtos := make([]models.TileServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *s.convertToDTO(&svc)
	}

	return dtos, total, nil
}

// SearchServices 搜索瓦片服务
func (s *TileServiceService) SearchServices(tenantID uint, keyword string, offset int, limit int) ([]models.TileServiceDTO, int64, error) {
	services, total, err := s.repo.SearchServices(tenantID, keyword, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("search services failed: %w", err)
	}

	dtos := make([]models.TileServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *s.convertToDTO(&svc)
	}

	return dtos, total, nil
}

// UpdateService 更新瓦片服务
func (s *TileServiceService) UpdateService(id uint, req *models.UpdateTileServiceRequest) (*models.TileServiceDTO, error) {
	// 获取现有服务
	service, err := s.repo.GetServiceByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	// 构建更新字段
	updates := make(map[string]interface{})

	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Keywords != nil {
		updates["keywords"] = models.StringArray(req.Keywords)
	}
	if req.Protocols != nil {
		// 合并现有协议配置和新配置
		currentProtocols := service.Protocols
		if currentProtocols == nil {
			currentProtocols = make(map[string]interface{})
		}
		for k, v := range req.Protocols {
			currentProtocols[k] = v
		}
		updates["protocols"] = currentProtocols
	}
	if req.PublicAccess != nil {
		updates["public_access"] = *req.PublicAccess
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	// 执行更新
	if err := s.repo.UpdateService(id, updates); err != nil {
		return nil, fmt.Errorf("update service failed: %w", err)
	}

	// 重新加载服务
	service, err = s.repo.GetServiceByID(id)
	if err != nil {
		return nil, fmt.Errorf("get updated service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// DeleteService 删除瓦片服务（会级联删除所有图层）
func (s *TileServiceService) DeleteService(id uint) error {
	if err := s.repo.DeleteService(id); err != nil {
		return fmt.Errorf("delete service failed: %w", err)
	}
	return nil
}

// ============================================================================
// 图层管理
// ============================================================================

// AddLayer 为服务添加新图层
func (s *TileServiceService) AddLayer(serviceID uint, req *models.CreateTileLayerRequest) (*models.TileServiceLayerDTO, error) {
	// 1. 验证服务是否存在
	_, err := s.repo.GetServiceByID(serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	// 2. 验证图层名称唯一性
	unique, err := s.repo.CheckLayerNameUnique(serviceID, req.LayerName, nil)
	if err != nil {
		return nil, fmt.Errorf("check layer name failed: %w", err)
	}
	if !unique {
		return nil, errors.New("layer name already exists in this service")
	}

	// 3. 验证图层配置
	if err := s.validateLayerConfig(req.LayerType, req.LayerConfig); err != nil {
		return nil, fmt.Errorf("invalid layer config: %w", err)
	}

	// 4. 创建图层模型
	layer := &models.TileServiceLayer{
		ServiceID:    serviceID,
		LayerName:    req.LayerName,
		Title:        req.Title,
		Description:  req.Description,
		LayerType:    req.LayerType,
		LayerConfig:  req.LayerConfig,
		DisplayOrder: 0, // 新图层默认显示顺序为 0
		Enabled:      true,
	}

	// 5. 保存到数据库
	if err := s.repo.CreateLayer(layer); err != nil {
		return nil, fmt.Errorf("create layer failed: %w", err)
	}

	// 6. 重新加载图层
	layer, err = s.repo.GetLayerByID(layer.ID)
	if err != nil {
		return nil, fmt.Errorf("get created layer failed: %w", err)
	}

	return s.convertLayerToDTO(layer), nil
}

// GetLayer 获取图层详情
func (s *TileServiceService) GetLayer(layerID uint) (*models.TileServiceLayerDTO, error) {
	layer, err := s.repo.GetLayerByID(layerID)
	if err != nil {
		return nil, fmt.Errorf("get layer failed: %w", err)
	}
	return s.convertLayerToDTO(layer), nil
}

// ListLayers 列出服务下的所有图层
func (s *TileServiceService) ListLayers(serviceID uint) ([]models.TileServiceLayerDTO, error) {
	layers, err := s.repo.ListLayers(serviceID)
	if err != nil {
		return nil, fmt.Errorf("list layers failed: %w", err)
	}

	dtos := make([]models.TileServiceLayerDTO, len(layers))
	for i, layer := range layers {
		dtos[i] = *s.convertLayerToDTO(&layer)
	}

	return dtos, nil
}

// UpdateLayer 更新图层
func (s *TileServiceService) UpdateLayer(layerID uint, req *models.UpdateTileLayerRequest) (*models.TileServiceLayerDTO, error) {
	// 获取现有图层
	layer, err := s.repo.GetLayerByID(layerID)
	if err != nil {
		return nil, fmt.Errorf("get layer failed: %w", err)
	}

	// 构建更新字段
	updates := make(map[string]interface{})

	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.LayerConfig != nil {
		// 验证新配置
		if err := s.validateLayerConfig(layer.LayerType, req.LayerConfig); err != nil {
			return nil, fmt.Errorf("invalid layer config: %w", err)
		}
		updates["layer_config"] = req.LayerConfig
	}
	if req.DisplayOrder != nil {
		updates["display_order"] = *req.DisplayOrder
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	// 执行更新
	if err := s.repo.UpdateLayer(layerID, updates); err != nil {
		return nil, fmt.Errorf("update layer failed: %w", err)
	}

	// 重新加载图层
	layer, err = s.repo.GetLayerByID(layerID)
	if err != nil {
		return nil, fmt.Errorf("get updated layer failed: %w", err)
	}

	return s.convertLayerToDTO(layer), nil
}

// DeleteLayer 删除图层
func (s *TileServiceService) DeleteLayer(layerID uint) error {
	if err := s.repo.DeleteLayer(layerID); err != nil {
		return fmt.Errorf("delete layer failed: %w", err)
	}
	return nil
}

// ============================================================================
// 辅助方法
// ============================================================================

// validateLayerConfig 验证图层配置
func (s *TileServiceService) validateLayerConfig(layerType string, config map[string]interface{}) error {
	// 验证图层类型
	if layerType != "dynamic" && layerType != "static" {
		return errors.New("layer_type must be 'dynamic' or 'static'")
	}

	// 动态图层验证
	if layerType == "dynamic" {
		source, ok := config["source"].(map[string]interface{})
		if !ok {
			return errors.New("dynamic layer requires 'source' configuration")
		}

		// 验证必需字段
		requiredFields := []string{"engine_id", "schema", "table", "geometry_column", "srid"}
		for _, field := range requiredFields {
			if _, ok := source[field]; !ok {
				return fmt.Errorf("dynamic layer source missing required field: %s", field)
			}
		}

		// 验证 engine_id 类型（应该是数字）
		switch source["engine_id"].(type) {
		case float64, int, int64, uint, uint64:
			// OK
		default:
			return errors.New("engine_id must be a number")
		}
	}

	// 静态图层验证
	if layerType == "static" {
		requiredFields := []string{"source", "tile_path", "format", "zoom_range"}
		for _, field := range requiredFields {
			if _, ok := config[field]; !ok {
				return fmt.Errorf("static layer missing required field: %s", field)
			}
		}

		// 验证 zoom_range 格式
		zoomRange, ok := config["zoom_range"].([]interface{})
		if !ok || len(zoomRange) != 2 {
			return errors.New("zoom_range must be an array of 2 numbers [minZoom, maxZoom]")
		}
	}

	return nil
}

// buildProtocolsConfig 构建协议配置
func (s *TileServiceService) buildProtocolsConfig(userProtocols map[string]interface{}) models.JSONB {
	// 默认协议配置
	protocols := map[string]interface{}{
		"xyz": map[string]interface{}{
			"enabled": true,
			"formats": []string{"mvt"},
		},
		"ogc_tiles": map[string]interface{}{
			"enabled": false,
			"version": "1.0",
		},
		"tms": map[string]interface{}{
			"enabled": false,
		},
	}

	// 用户自定义的协议配置覆盖默认值
	if userProtocols != nil {
		for k, v := range userProtocols {
			protocols[k] = v
		}
	}

	return protocols
}

// convertToDTO 将服务模型转换为 DTO
func (s *TileServiceService) convertToDTO(service *models.TileService) *models.TileServiceDTO {
	dto := &models.TileServiceDTO{
		ID:           service.ID,
		TenantID:     service.TenantID,
		ServiceName:  service.ServiceName,
		Title:        service.Title,
		Description:  service.Description,
		Keywords:     []string(service.Keywords),
		DefaultSRID:  service.DefaultSRID,
		Extent:       service.Extent,
		Protocols:    service.Protocols,
		PublicAccess: service.PublicAccess,
		Status:       service.Status,
		ErrorMessage: service.ErrorMessage,
		CreatedBy:    service.CreatedBy,
		CreatedAt:    service.CreatedAt,
		UpdatedAt:    service.UpdatedAt,
	}

	// 转换图层
	if len(service.Layers) > 0 {
		dto.Layers = make([]models.TileServiceLayerDTO, len(service.Layers))
		for i, layer := range service.Layers {
			dto.Layers[i] = *s.convertLayerToDTO(&layer)
		}
	}

	// 构建服务端点
	dto.Endpoints = s.buildEndpoints(service)

	return dto
}

// convertLayerToDTO 将图层模型转换为 DTO
func (s *TileServiceService) convertLayerToDTO(layer *models.TileServiceLayer) *models.TileServiceLayerDTO {
	return &models.TileServiceLayerDTO{
		ID:           layer.ID,
		ServiceID:    layer.ServiceID,
		LayerName:    layer.LayerName,
		Title:        layer.Title,
		Description:  layer.Description,
		LayerType:    layer.LayerType,
		LayerConfig:  layer.LayerConfig,
		DisplayOrder: layer.DisplayOrder,
		Enabled:      layer.Enabled,
		CreatedAt:    layer.CreatedAt,
		UpdatedAt:    layer.UpdatedAt,
	}
}

// buildEndpoints 构建服务端点URL
func (s *TileServiceService) buildEndpoints(service *models.TileService) map[string]string {
	endpoints := make(map[string]string)

	// XYZ Tiles 端点
	if service.IsXYZEnabled() {
		// 示例: /tiles/{serviceName}/{layerName}/{z}/{x}/{y}.mvt
		endpoints["xyz_tiles"] = fmt.Sprintf("%s/tiles/%s/{layerName}/{z}/{x}/{y}.{format}", s.baseURL, service.ServiceName)
	}

	// OGC Tiles API 端点
	if service.IsOGCTilesEnabled() {
		endpoints["ogc_tiles"] = fmt.Sprintf("%s/ogc/tiles/%s", s.baseURL, service.ServiceName)
		endpoints["ogc_tiles_tilesets"] = fmt.Sprintf("%s/ogc/tiles/%s/tiles", s.baseURL, service.ServiceName)
	}

	// WMTS 端点
	endpoints["wmts"] = fmt.Sprintf("%s/wmts/%s?request=GetCapabilities", s.baseURL, service.ServiceName)

	return endpoints
}
