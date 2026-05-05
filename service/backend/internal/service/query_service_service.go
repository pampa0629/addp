package service

import (
	"errors"
	"fmt"
	"log"

	commonAttrs "github.com/addp/common/attributes"
	"github.com/addp/common/client"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

type QueryServiceService struct {
	repo       *repository.QueryServiceRepository
	metaClient *client.MetaClient
	baseURL    string // 服务的基础URL（用于构建端点）
}

func NewQueryServiceService(repo *repository.QueryServiceRepository, metaClient *client.MetaClient, baseURL string) *QueryServiceService {
	return &QueryServiceService{
		repo:       repo,
		metaClient: metaClient,
		baseURL:    baseURL,
	}
}

// CreateService 创建新的查询服务
func (s *QueryServiceService) CreateService(req *models.CreateQueryServiceRequest, tenantID uint, createdBy uint) (*models.QueryServiceDTO, error) {
	// 1. 验证配置类型
	if req.ConfigType != "table" && req.ConfigType != "sql" {
		return nil, errors.New("invalid config_type: must be 'table' or 'sql'")
	}

	// 2. Table模式的特殊验证
	if req.ConfigType == "table" {
		if req.EngineID == nil || *req.EngineID == 0 {
			return nil, errors.New("engine_id is required for table mode")
		}
		if req.SchemaName == "" || req.TableName == "" {
			return nil, errors.New("schema_name and table_name are required for table mode")
		}
		if req.SqlQuery != "" {
			return nil, errors.New("sql_query should not be provided in table mode")
		}
	}

	// 3. SQL模式的特殊验证
	if req.ConfigType == "sql" {
		if req.SqlQuery == "" {
			return nil, errors.New("sql_query is required for sql mode")
		}
		if req.SchemaName != "" || req.TableName != "" {
			return nil, errors.New("schema_name and table_name should not be provided in sql mode")
		}
		// engine_id 为 nil 时表示 DuckDB 联邦查询模式，合法
	}

	// 4. 检查服务名称是否唯一
	unique, err := s.repo.CheckServiceNameUnique(req.ServiceName, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("check service name failed: %w", err)
	}
	if !unique {
		return nil, errors.New("service name already exists")
	}

	// 5. 构建数据配置
	dataConfig := make(map[string]interface{})
	if req.DataConfig != nil {
		dataConfig = req.DataConfig
	}

	// 6. Table模式：自动检测空间字段 / 湖表信息
	if req.ConfigType == "table" {
		engineID := *req.EngineID
		// 先尝试从 Meta 获取湖表信息（MinIO/S3 引擎）
		lakeMode := s.detectLakeMode(tenantID, engineID, req.SchemaName, req.TableName)
		if lakeMode != "" {
			// 湖表：写入 lake_mode，跳过空间字段检测
			dataConfig["lake_mode"] = lakeMode
		} else {
			// 关系型表：检测空间字段
			spatialMeta, err := s.metaClient.WithTenantID(tenantID).GetItemSpatialMetadata(engineID, req.SchemaName, req.TableName)
			if err != nil {
				log.Printf("Warning: failed to get spatial metadata for table %s.%s: %v", req.SchemaName, req.TableName, err)
			} else if spatialMeta != nil && spatialMeta.GeometryColumn != "" {
				dataConfig["geometry"] = map[string]interface{}{
					"has_geometry": true,
					"column":       spatialMeta.GeometryColumn,
					"srid":         spatialMeta.SRID,
					"types":        spatialMeta.GeometryTypes,
					"extent":       spatialMeta.Extent,
				}
			}
		}
	}

	// 7. 构建协议配置
	protocols := s.buildProtocolsConfig(req.Protocols, dataConfig)

	// 8. 设置默认值
	maxFeatures := req.MaxFeatures
	if maxFeatures == 0 {
		maxFeatures = 1000
	}

	// 9. 创建服务模型
	service := &models.QueryService{
		TenantID:    tenantID,
		ServiceName: req.ServiceName,
		Title:       req.Title,
		Description: req.Description,
		Keywords:    models.StringArray(req.Keywords),

		ConfigType: req.ConfigType,
		EngineID:   req.EngineID,

		SchemaName:  req.SchemaName,
		TargetTable: req.TableName,
		SqlQuery:    req.SqlQuery,

		DataConfig: dataConfig,
		Protocols:  protocols,

		PublicAccess: req.PublicAccess,
		MaxFeatures:  maxFeatures,

		Status:    "active",
		CreatedBy: createdBy,
	}

	// 10. 保存到数据库
	if err := s.repo.Create(service); err != nil {
		return nil, fmt.Errorf("create service failed: %w", err)
	}

	// 11. 重新加载服务（获取完整数据）
	service, err = s.repo.GetByID(service.ID)
	if err != nil {
		return nil, fmt.Errorf("get created service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// detectLakeMode 通过 Meta 判断是否为湖表，返回 lake_mode（"directory"/"file"）或空字符串
func (s *QueryServiceService) detectLakeMode(tenantID, engineID uint, schemaName, tableName string) string {
	if s.metaClient == nil {
		log.Printf("[detectLakeMode] metaClient is nil, skipping lake mode detection")
		return ""
	}
	tree, err := s.metaClient.WithTenantID(tenantID).GetMetadataTree(engineID)
	if err != nil {
		log.Printf("[detectLakeMode] GetMetadataTree(engineID=%d) failed: %v", engineID, err)
		return ""
	}
	log.Printf("[detectLakeMode] engineID=%d, got %d items", engineID, len(tree.Items))
	// 构建期望的 full_name
	expectedFullName := tableName
	if schemaName != "" {
		expectedFullName = schemaName + "/" + tableName
	}
	for _, item := range tree.Items {
		if item.ItemType != "lake_table" {
			continue
		}
		if item.Name != tableName && item.FullName != expectedFullName {
			continue
		}
		if item.Attributes != nil {
			if mode := commonAttrs.String(item.Attributes, "item", "mode"); mode != "" {
				return mode
			}
		}
		// 找到湖表但没有 mode 属性，默认 directory
		return "directory"
	}
	return ""
}
func (s *QueryServiceService) buildProtocolsConfig(userProtocols map[string]interface{}, dataConfig map[string]interface{}) models.JSONB {
	// 默认协议配置
	protocols := map[string]interface{}{
		"rest_api": map[string]interface{}{
			"enabled": true,
			"formats": []string{"json", "csv", "geojson"},
		},
		"ogc_features": map[string]interface{}{
			"enabled": false,
			"version": "1.0",
		},
	}

	// 如果检测到空间字段，自动启用 OGC Features
	if geometry, ok := dataConfig["geometry"].(map[string]interface{}); ok {
		if hasGeometry, ok := geometry["has_geometry"].(bool); ok && hasGeometry {
			protocols["ogc_features"] = map[string]interface{}{
				"enabled": true,
				"version": "1.0",
			}
		}
	}

	// 用户自定义的协议配置覆盖默认值
	if userProtocols != nil {
		for k, v := range userProtocols {
			protocols[k] = v
		}
	}

	return protocols
}

// GetService 获取服务详情
func (s *QueryServiceService) GetService(id uint) (*models.QueryServiceDTO, error) {
	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// GetServiceByName 根据名称获取服务
func (s *QueryServiceService) GetServiceByName(serviceName string, tenantID uint) (*models.QueryServiceDTO, error) {
	service, err := s.repo.GetByNameAndTenant(serviceName, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// GetServiceModelByName 根据名称获取服务模型（用于查询执行）
func (s *QueryServiceService) GetServiceModelByName(serviceName string, tenantID uint) (*models.QueryService, error) {
	service, err := s.repo.GetByNameAndTenant(serviceName, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	return service, nil
}

// GetServiceModelByNameOnly 仅根据名称获取服务模型（不过滤租户，用于公开服务查询）
func (s *QueryServiceService) GetServiceModelByNameOnly(serviceName string) (*models.QueryService, error) {
	service, err := s.repo.GetByName(serviceName)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	return service, nil
}

// ListServices 列出租户下的所有查询服务
func (s *QueryServiceService) ListServices(tenantID uint, offset int, limit int) ([]models.QueryServiceDTO, int64, error) {
	services, total, err := s.repo.List(tenantID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list services failed: %w", err)
	}

	dtos := make([]models.QueryServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *s.convertToDTO(&svc)
	}

	return dtos, total, nil
}

// SearchServices 搜索服务
func (s *QueryServiceService) SearchServices(tenantID uint, keyword string, offset int, limit int) ([]models.QueryServiceDTO, int64, error) {
	services, total, err := s.repo.Search(tenantID, keyword, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("search services failed: %w", err)
	}

	dtos := make([]models.QueryServiceDTO, len(services))
	for i, svc := range services {
		dtos[i] = *s.convertToDTO(&svc)
	}

	return dtos, total, nil
}

// UpdateService 更新服务
func (s *QueryServiceService) UpdateService(id uint, req *models.UpdateQueryServiceRequest) (*models.QueryServiceDTO, error) {
	// 获取现有服务
	service, err := s.repo.GetByID(id)
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
	if req.DataConfig != nil {
		// 合并现有配置和新配置
		currentConfig := service.DataConfig
		if currentConfig == nil {
			currentConfig = make(map[string]interface{})
		}
		for k, v := range req.DataConfig {
			currentConfig[k] = v
		}
		updates["data_config"] = currentConfig
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
	if req.MaxFeatures != nil {
		updates["max_features"] = *req.MaxFeatures
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	// 执行更新
	if err := s.repo.Update(id, updates); err != nil {
		return nil, fmt.Errorf("update service failed: %w", err)
	}

	// 重新加载服务
	service, err = s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get updated service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

// DeleteService 删除服务
func (s *QueryServiceService) DeleteService(id uint) error {
	if err := s.repo.Delete(id); err != nil {
		return fmt.Errorf("delete service failed: %w", err)
	}
	return nil
}

// UpdateServiceStatus 更新服务状态
func (s *QueryServiceService) UpdateServiceStatus(id uint, status string, errorMessage string) error {
	if err := s.repo.UpdateStatus(id, status, errorMessage); err != nil {
		return fmt.Errorf("update service status failed: %w", err)
	}
	return nil
}

// convertToDTO 将服务模型转换为 DTO
func (s *QueryServiceService) convertToDTO(service *models.QueryService) *models.QueryServiceDTO {
	dto := &models.QueryServiceDTO{
		ID: service.ID,

		TenantID:    service.TenantID,
		ServiceName: service.ServiceName,
		Title:       service.Title,
		Description: service.Description,
		Keywords:    []string(service.Keywords),

		ConfigType: service.ConfigType,
		EngineID:   service.EngineID,

		DataConfig: service.DataConfig,
		Protocols:  service.Protocols,

		PublicAccess: service.PublicAccess,
		MaxFeatures:  service.MaxFeatures,

		Status:       service.Status,
		ErrorMessage: service.ErrorMessage,

		CreatedBy: service.CreatedBy,
		CreatedAt: service.CreatedAt,
		UpdatedAt: service.UpdatedAt,
	}

	// 根据配置类型设置相应字段
	if service.IsTableMode() {
		dto.SchemaName = service.SchemaName
		dto.TableName = service.TargetTable
	} else {
		dto.SqlQuery = service.SqlQuery
	}

	// 构建服务端点
	dto.Endpoints = s.buildEndpoints(service)

	return dto
}

// buildEndpoints 构建服务端点URL
func (s *QueryServiceService) buildEndpoints(service *models.QueryService) map[string]string {
	endpoints := make(map[string]string)

	// REST API 端点
	if service.IsRESTAPIEnabled() {
		endpoints["rest_api"] = fmt.Sprintf("%s/api/query/%s", s.baseURL, service.ServiceName)
	}

	// OGC Features 端点
	if service.IsOGCFeaturesEnabled() {
		endpoints["ogc_features"] = fmt.Sprintf("%s/ogc/features/%s", s.baseURL, service.ServiceName)
		endpoints["ogc_features_collections"] = fmt.Sprintf("%s/ogc/features/%s/collections", s.baseURL, service.ServiceName)
	}

	return endpoints
}
