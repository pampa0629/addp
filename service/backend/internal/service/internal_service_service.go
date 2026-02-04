package service

import (
	"errors"
	"fmt"
	"log"

	"github.com/addp/common/client"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

type InternalServiceService struct {
	repo       *repository.InternalServiceRepository
	metaClient *client.MetaClient
}

func NewInternalServiceService(repo *repository.InternalServiceRepository, metaClient *client.MetaClient) *InternalServiceService {
	return &InternalServiceService{
		repo:       repo,
		metaClient: metaClient,
	}
}

// CreateService 创建新的内部服务
func (s *InternalServiceService) CreateService(req *models.CreateInternalServiceRequest, tenantID uint, createdBy uint) (*models.InternalServiceDTO, error) {
	// 1. 验证服务类型
	if req.ServiceType != "spatial" && req.ServiceType != "table" {
		return nil, errors.New("invalid service_type: must be 'spatial' or 'table'")
	}

	// 2. 空间服务的特殊验证
	if req.ServiceType == "spatial" {
		if req.DefaultSRID == nil || *req.DefaultSRID == 0 {
			return nil, errors.New("default_srid is required for spatial service")
		}
		if req.FirstLayer == nil {
			return nil, errors.New("first_layer is required for spatial service")
		}
		if req.FirstLayer.GeometryColumn == "" {
			return nil, errors.New("geometry_column is required for spatial service layer")
		}
	}

	// 3. 数据表服务的特殊验证
	if req.ServiceType == "table" {
		if req.FirstLayer != nil && req.FirstLayer.GeometryColumn != "" {
			return nil, errors.New("table service layer cannot have geometry_column")
		}
	}

	// 检查服务名称是否唯一
	unique, err := s.repo.CheckServiceNameUnique(req.ServiceName, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("check service name failed: %w", err)
	}
	if !unique {
		return nil, errors.New("service name already exists")
	}

	// 设置默认值
	defaultSRID := 4326
	if req.DefaultSRID != nil {
		defaultSRID = *req.DefaultSRID
	}

	maxFeatures := req.MaxFeatures
	if maxFeatures == 0 {
		maxFeatures = 1000
	}

	// 构建 config JSON
	config := s.buildServiceConfig(req)

	service := &models.InternalService{
		TenantID:      tenantID,
		ServiceName:   req.ServiceName,
		Title:         req.Title,
		Abstract:      req.Abstract,
		Keywords:      models.StringArray(req.Keywords),
		ServiceType:   req.ServiceType,
		Config:        config,
		PublicAccess:  req.PublicAccess,
		DefaultSRID:   defaultSRID,
		MaxFeatures:   maxFeatures,
		ProviderName:  req.ProviderName,
		ProviderSite:  req.ProviderSite,
		ContactPerson: req.ContactPerson,
		ContactEmail:  req.ContactEmail,
		EngineID:      req.EngineID,
		Status:        "active",
		CreatedBy:     createdBy,
	}

	if err := s.repo.Create(service); err != nil {
		return nil, fmt.Errorf("create service failed: %w", err)
	}

	// 如果提供了第一个图层，自动创建
	if req.FirstLayer != nil {
		if _, err := s.AddLayer(service.ID, req.FirstLayer); err != nil {
			// 回滚服务
			s.repo.Delete(service.ID)
			return nil, fmt.Errorf("failed to create first layer: %w", err)
		}
	}

	// 重新加载服务（包含图层）
	service, err = s.repo.GetByID(service.ID)
	if err != nil {
		return nil, fmt.Errorf("get created service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// buildServiceConfig 构建服务配置
func (s *InternalServiceService) buildServiceConfig(req *models.CreateInternalServiceRequest) models.JSONB {
	config := make(map[string]interface{})

	if req.ServiceType == "spatial" {
		// 空间服务的默认配置
		protocols := map[string]interface{}{
			"wfs":        map[string]interface{}{"enabled": true, "version": "2.0.0"},
			"wmts":       map[string]interface{}{"enabled": true, "version": "1.0.0"},
			"ogc_api":    map[string]interface{}{"enabled": true, "version": "1.0"},
			"rest_query": map[string]interface{}{"enabled": true},
		}

		// 用户自定义的协议配置覆盖默认值
		if req.ProtocolsConfig != nil {
			for k, v := range req.ProtocolsConfig {
				protocols[k] = v
			}
		}

		config["protocols"] = protocols
		config["allow_multiple_layers"] = true
	} else {
		// 数据表服务的默认配置
		config["protocols"] = map[string]interface{}{
			"rest_query": map[string]interface{}{
				"enabled": true,
				"pagination": map[string]interface{}{
					"default_limit": 20,
					"max_limit":     1000,
				},
				"export_formats": []string{"json", "csv"},
			},
		}
		config["allow_multiple_layers"] = false
	}

	return config
}

// GetService 获取服务详情
func (s *InternalServiceService) GetService(id uint) (*models.InternalServiceDTO, error) {
	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// ListServices 列出租户下的所有服务
func (s *InternalServiceService) ListServices(tenantID uint, offset int, limit int) ([]models.InternalServiceDTO, int64, error) {
	services, total, err := s.repo.List(tenantID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list services failed: %w", err)
	}

	dtos := make([]models.InternalServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *s.convertToDTO(&svc)
	}

	return dtos, total, nil
}

// SearchServices 搜索服务
func (s *InternalServiceService) SearchServices(tenantID uint, keyword string, offset int, limit int) ([]models.InternalServiceDTO, int64, error) {
	services, total, err := s.repo.Search(tenantID, keyword, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("search services failed: %w", err)
	}

	dtos := make([]models.InternalServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *s.convertToDTO(&svc)
	}

	return dtos, total, nil
}

// UpdateService 更新服务
func (s *InternalServiceService) UpdateService(id uint, req *models.UpdateInternalServiceRequest) (*models.InternalServiceDTO, error) {
	updates := make(map[string]interface{})
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Abstract != nil {
		updates["abstract"] = *req.Abstract
	}
	if req.Keywords != nil {
		updates["keywords"] = models.StringArray(req.Keywords)
	}
	if req.PublicAccess != nil {
		updates["public_access"] = *req.PublicAccess
	}
	if req.DefaultSRID != nil {
		updates["default_srid"] = *req.DefaultSRID
	}
	if req.MaxFeatures != nil {
		updates["max_features"] = *req.MaxFeatures
	}
	if req.ProviderName != nil {
		updates["provider_name"] = *req.ProviderName
	}
	if req.ProviderSite != nil {
		updates["provider_site"] = *req.ProviderSite
	}
	if req.ContactPerson != nil {
		updates["contact_person"] = *req.ContactPerson
	}
	if req.ContactEmail != nil {
		updates["contact_email"] = *req.ContactEmail
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	// 更新协议配置
	if req.ProtocolsConfig != nil {
		// 获取当前 config
		currentService, err := s.repo.GetByID(id)
		if err != nil {
			return nil, fmt.Errorf("get service failed: %w", err)
		}

		config := currentService.Config
		if config == nil {
			config = make(models.JSONB)
		}

		// 合并协议配置
		if config["protocols"] == nil {
			config["protocols"] = make(map[string]interface{})
		}
		protocols := config["protocols"].(map[string]interface{})
		for k, v := range req.ProtocolsConfig {
			protocols[k] = v
		}
		config["protocols"] = protocols

		updates["config"] = config
	}

	if err := s.repo.Update(id, updates); err != nil {
		return nil, fmt.Errorf("update service failed: %w", err)
	}

	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get updated service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// DeleteService 删除服务
func (s *InternalServiceService) DeleteService(id uint) error {
	if err := s.repo.Delete(id); err != nil {
		return fmt.Errorf("delete service failed: %w", err)
	}
	return nil
}

// AddLayer 添加图层
func (s *InternalServiceService) AddLayer(serviceID uint, req *models.AddLayerRequest) (*models.InternalServiceLayerDTO, error) {
	// 验证服务是否存在并获取 engine_id
	service, err := s.repo.GetByID(serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	// 检查是否允许多图层
	if !service.AllowMultipleLayers() {
		layers, err := s.repo.ListLayers(serviceID)
		if err != nil {
			return nil, fmt.Errorf("failed to check existing layers: %w", err)
		}
		if len(layers) >= 1 {
			return nil, errors.New("table service only supports single layer")
		}
	}

	// 空间服务的图层必须有几何列
	if service.IsSpatialService() && req.GeometryColumn == "" {
		// 空间服务允许在后续从 Meta 获取几何列信息，所以这里不强制要求
		// 但如果最终还是没有几何列，会在后面的验证中报错
	}

	// 数据表服务的图层不能有几何列
	if service.ServiceType == "table" && req.GeometryColumn != "" {
		return nil, errors.New("table service layer cannot have geometry_column")
	}

	// 从请求中获取或设置默认值
	geometryColumn := req.GeometryColumn
	srid := req.SRID
	geometryTypes := req.GeometryTypes
	extent4326 := req.Extent4326

	// 如果未提供空间元数据，尝试从 Meta 模块获取
	if geometryColumn == "" || srid == 0 || len(geometryTypes) == 0 {
		if s.metaClient != nil {
			// 设置租户 ID（用于服务间调用时的租户隔离）
			s.metaClient.SetTenantID(&service.TenantID)

			log.Printf("📡 Fetching spatial metadata from Meta for %s.%s (tenant_id=%d, engine_id=%d)",
				req.SchemaName, req.DBTableName, service.TenantID, service.EngineID)

			spatialMeta, err := s.metaClient.GetTableSpatialMetadata(service.EngineID, req.SchemaName, req.DBTableName)
			if err != nil {
				log.Printf("⚠️  Failed to get spatial metadata from Meta: %v", err)
				// 如果 Meta 获取失败且用户未提供必填字段，返回错误
				if geometryColumn == "" {
					return nil, fmt.Errorf("geometry_column is required (Meta service unavailable)")
				}
				if srid == 0 {
					return nil, fmt.Errorf("srid is required (Meta service unavailable)")
				}
			} else {
				// 使用 Meta 提供的元数据（仅在用户未提供时）
				if geometryColumn == "" {
					geometryColumn = spatialMeta.GeometryColumn
					log.Printf("✅ Using geometry_column from Meta: %s", geometryColumn)
				}
				if srid == 0 {
					srid = spatialMeta.SRID
					log.Printf("✅ Using SRID from Meta: %d", srid)
				}
				if len(geometryTypes) == 0 && len(spatialMeta.GeometryTypes) > 0 {
					geometryTypes = spatialMeta.GeometryTypes
					log.Printf("✅ Using geometry_types from Meta: %v", geometryTypes)
				}
				// TODO: 将 Meta 的 extent 转换为 extent_4326 (需要坐标转换)
				// 暂时跳过，让用户手动提供或后续优化
			}
		}
	}

	// 最终验证必填字段
	if geometryColumn == "" {
		return nil, fmt.Errorf("geometry_column is required")
	}
	if srid == 0 {
		return nil, fmt.Errorf("srid is required")
	}

	layer := &models.InternalServiceLayer{
		ServiceID:      serviceID,
		LayerName:      req.LayerName,
		Title:          req.Title,
		Abstract:       req.Abstract,
		Keywords:       models.StringArray(req.Keywords),
		MetaItemID:     req.MetaItemID,
		SchemaName:     req.SchemaName,
		DBTableName:    req.DBTableName,
		GeometryColumn: geometryColumn,
		SRID:           srid,
		Extent4326:     extent4326,
		GeometryTypes:  models.StringArray(geometryTypes),
		Queryable:      req.Queryable,
		MaxFeatures:    req.MaxFeatures,
		FilterColumns:  models.StringArray(req.FilterColumns),
		DefaultStyle:   req.DefaultStyle,
		DisplayOrder:   req.DisplayOrder,
		Enabled:        true,
	}

	if err := s.repo.AddLayer(layer); err != nil {
		return nil, fmt.Errorf("add layer failed: %w", err)
	}

	return s.convertLayerToDTO(layer), nil
}

// GetLayer 获取图层详情
func (s *InternalServiceService) GetLayer(layerID uint) (*models.InternalServiceLayerDTO, error) {
	layer, err := s.repo.GetLayerByID(layerID)
	if err != nil {
		return nil, fmt.Errorf("get layer failed: %w", err)
	}

	return s.convertLayerToDTO(layer), nil
}

// ListLayers 列出服务下的所有图层
func (s *InternalServiceService) ListLayers(serviceID uint) ([]models.InternalServiceLayerDTO, error) {
	layers, err := s.repo.ListLayers(serviceID)
	if err != nil {
		return nil, fmt.Errorf("list layers failed: %w", err)
	}

	dtos := make([]models.InternalServiceLayerDTO, len(layers))
	for i, layer := range layers {
		dtos[i] = *s.convertLayerToDTO(&layer)
	}

	return dtos, nil
}

// UpdateLayer 更新图层
func (s *InternalServiceService) UpdateLayer(layerID uint, req *models.UpdateLayerRequest) (*models.InternalServiceLayerDTO, error) {
	updates := make(map[string]interface{})
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Abstract != nil {
		updates["abstract"] = *req.Abstract
	}
	if req.Keywords != nil {
		updates["keywords"] = models.StringArray(req.Keywords)
	}
	if req.Queryable != nil {
		updates["queryable"] = *req.Queryable
	}
	if req.MaxFeatures != nil {
		updates["max_features"] = *req.MaxFeatures
	}
	if req.FilterColumns != nil {
		updates["filter_columns"] = models.StringArray(req.FilterColumns)
	}
	if req.DefaultStyle != nil {
		updates["default_style"] = *req.DefaultStyle
	}
	if req.DisplayOrder != nil {
		updates["display_order"] = *req.DisplayOrder
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if err := s.repo.UpdateLayer(layerID, updates); err != nil {
		return nil, fmt.Errorf("update layer failed: %w", err)
	}

	layer, err := s.repo.GetLayerByID(layerID)
	if err != nil {
		return nil, fmt.Errorf("get updated layer failed: %w", err)
	}

	return s.convertLayerToDTO(layer), nil
}

// DeleteLayer 删除图层
func (s *InternalServiceService) DeleteLayer(layerID uint) error {
	if err := s.repo.DeleteLayer(layerID); err != nil {
		return fmt.Errorf("delete layer failed: %w", err)
	}
	return nil
}

// Helper methods

func (s *InternalServiceService) convertToDTO(service *models.InternalService) *models.InternalServiceDTO {
	layers := make([]models.InternalServiceLayerDTO, len(service.Layers))
	for i, layer := range service.Layers {
		layers[i] = *s.convertLayerToDTO(&layer)
	}

	// Convert Config JSONB to map for DTO
	var config map[string]interface{}
	if service.Config != nil {
		config = service.Config
	} else {
		config = make(map[string]interface{})
	}

	return &models.InternalServiceDTO{
		ID:            service.ID,
		TenantID:      service.TenantID,
		ServiceName:   service.ServiceName,
		Title:         service.Title,
		Abstract:      service.Abstract,
		Keywords:      service.Keywords,
		ServiceType:   service.ServiceType,
		Config:        config,
		PublicAccess:  service.PublicAccess,
		DefaultSRID:   service.DefaultSRID,
		MaxFeatures:   service.MaxFeatures,
		ProviderName:  service.ProviderName,
		ProviderSite:  service.ProviderSite,
		ContactPerson: service.ContactPerson,
		ContactEmail:  service.ContactEmail,
		EngineID:      service.EngineID,
		Status:        service.Status,
		Layers:        layers,
		CreatedBy:     service.CreatedBy,
		CreatedAt:     service.CreatedAt,
		UpdatedAt:     service.UpdatedAt,
	}
}

func (s *InternalServiceService) convertLayerToDTO(layer *models.InternalServiceLayer) *models.InternalServiceLayerDTO {
	return &models.InternalServiceLayerDTO{
		ID:             layer.ID,
		ServiceID:      layer.ServiceID,
		LayerName:      layer.LayerName,
		Title:          layer.Title,
		Abstract:       layer.Abstract,
		Keywords:       layer.Keywords,
		MetaItemID:     layer.MetaItemID,
		SchemaName:     layer.SchemaName,
		DBTableName:    layer.DBTableName,
		GeometryColumn: layer.GeometryColumn,
		SRID:           layer.SRID,
		Extent4326:     layer.Extent4326,
		GeometryTypes:  layer.GeometryTypes,
		Queryable:      layer.Queryable,
		MaxFeatures:    layer.MaxFeatures,
		FilterColumns:  layer.FilterColumns,
		DefaultStyle:   layer.DefaultStyle,
		DisplayOrder:   layer.DisplayOrder,
		Enabled:        layer.Enabled,
		CreatedAt:      layer.CreatedAt,
		UpdatedAt:      layer.UpdatedAt,
	}
}
