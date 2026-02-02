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
	// 验证至少有一种服务类型启用
	if !req.EnabledWFS && !req.EnabledOGCAPI && !req.EnabledWMTS && !req.EnabledWMS {
		return nil, errors.New("at least one service type must be enabled")
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
	defaultSRID := req.DefaultSRID
	if defaultSRID == 0 {
		defaultSRID = 4326
	}

	maxFeatures := req.MaxFeatures
	if maxFeatures == 0 {
		maxFeatures = 1000
	}

	service := &models.InternalService{
		TenantID:       tenantID,
		ServiceName:    req.ServiceName,
		Title:          req.Title,
		Abstract:       req.Abstract,
		Keywords:       models.StringArray(req.Keywords),
		EnabledWFS:     req.EnabledWFS,
		EnabledOGCAPI:  req.EnabledOGCAPI,
		EnabledWMTS:    req.EnabledWMTS,
		EnabledWMS:     req.EnabledWMS,
		DefaultSRID:    defaultSRID,
		MaxFeatures:    maxFeatures,
		ProviderName:   req.ProviderName,
		ProviderSite:   req.ProviderSite,
		ContactPerson:  req.ContactPerson,
		ContactEmail:   req.ContactEmail,
		EngineID:       req.EngineID,
		Status:         "active",
		CreatedBy:      createdBy,
	}

	if err := s.repo.Create(service); err != nil {
		return nil, fmt.Errorf("create service failed: %w", err)
	}

	return s.convertToDTO(service), nil
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
	// 验证至少有一种服务类型启用（如果要更新的话）
	if (req.EnabledWFS != nil || req.EnabledOGCAPI != nil || req.EnabledWMTS != nil || req.EnabledWMS != nil) {
		// 获取当前服务的状态
		currentService, err := s.repo.GetByID(id)
		if err != nil {
			return nil, fmt.Errorf("get service failed: %w", err)
		}

		// 检查更新后是否至少有一种服务启用
		enabledWFS := currentService.EnabledWFS
		enabledOGCAPI := currentService.EnabledOGCAPI
		enabledWMTS := currentService.EnabledWMTS
		enabledWMS := currentService.EnabledWMS

		if req.EnabledWFS != nil {
			enabledWFS = *req.EnabledWFS
		}
		if req.EnabledOGCAPI != nil {
			enabledOGCAPI = *req.EnabledOGCAPI
		}
		if req.EnabledWMTS != nil {
			enabledWMTS = *req.EnabledWMTS
		}
		if req.EnabledWMS != nil {
			enabledWMS = *req.EnabledWMS
		}

		if !enabledWFS && !enabledOGCAPI && !enabledWMTS && !enabledWMS {
			return nil, errors.New("at least one service type must be enabled")
		}
	}

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
	if req.EnabledWFS != nil {
		updates["enabled_wfs"] = *req.EnabledWFS
	}
	if req.EnabledOGCAPI != nil {
		updates["enabled_ogc_api"] = *req.EnabledOGCAPI
	}
	if req.EnabledWMTS != nil {
		updates["enabled_wmts"] = *req.EnabledWMTS
	}
	if req.EnabledWMS != nil {
		updates["enabled_wms"] = *req.EnabledWMS
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

	return &models.InternalServiceDTO{
		ID:             service.ID,
		TenantID:       service.TenantID,
		ServiceName:    service.ServiceName,
		Title:          service.Title,
		Abstract:       service.Abstract,
		Keywords:       service.Keywords,
		EnabledWFS:     service.EnabledWFS,
		EnabledOGCAPI:  service.EnabledOGCAPI,
		EnabledWMTS:    service.EnabledWMTS,
		EnabledWMS:     service.EnabledWMS,
		DefaultSRID:    service.DefaultSRID,
		MaxFeatures:    service.MaxFeatures,
		ProviderName:   service.ProviderName,
		ProviderSite:   service.ProviderSite,
		ContactPerson:  service.ContactPerson,
		ContactEmail:   service.ContactEmail,
		EngineID:       service.EngineID,
		Status:         service.Status,
		Layers:         layers,
		CreatedBy:      service.CreatedBy,
		CreatedAt:      service.CreatedAt,
		UpdatedAt:      service.UpdatedAt,
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
