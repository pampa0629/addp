package service

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem"
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

	// 6. Table模式：自动检测空间字段 / 对象表信息
	if req.ConfigType == "table" {
		engineID := *req.EngineID
		// 先尝试从 Meta 获取对象存储表信息。
		objectTable := s.detectObjectTable(tenantID, engineID, req.SchemaName, req.TableName)
		if len(objectTable) > 0 {
			dataConfig["object_table"] = objectTable
		} else {
			// 关系型表：检测空间字段
			spatialMeta, err := s.metaClient.WithTenantID(tenantID).GetItemSpatialMetadataByCatalogPath(engineID, fmt.Sprintf("%s.%s", req.SchemaName, req.TableName))
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

// detectObjectTable 通过 Meta 标准 attributes 判断是否为对象存储表格资源。
func (s *QueryServiceService) detectObjectTable(tenantID, engineID uint, schemaName, tableName string) map[string]interface{} {
	if s.metaClient == nil {
		log.Printf("[detectObjectTable] metaClient is nil, skipping object table detection")
		return nil
	}
	catalogPath := tableName
	if schemaName != "" {
		catalogPath = strings.Trim(schemaName+"."+tableName, ".")
	}
	item, err := s.metaClient.WithTenantID(tenantID).GetItemByCatalogPath(engineID, catalogPath)
	if err != nil {
		log.Printf("[detectObjectTable] GetItemByCatalogPath(engineID=%d, catalogPath=%s) failed: %v", engineID, catalogPath, err)
		return nil
	}
	objectTable := objectTableConfigFromMetaAttributes(item.Attributes)
	if len(objectTable) == 0 {
		return nil
	}
	return objectTable
}

func objectTableConfigFromMetaAttributes(attrs map[string]interface{}) map[string]interface{} {
	descriptor, ok := objectTableDescriptorFromMetaAttributes(attrs)
	if !ok || descriptor.PhysicalPath == "" {
		return nil
	}
	layout := descriptor.Layout
	if layout == "" {
		layout = dataitem.LayoutSingle
	}
	return map[string]interface{}{
		"physical_path": descriptor.PhysicalPath,
		"layout":        string(layout),
		"format":        descriptor.Format,
	}
}

func objectTableDescriptorFromMetaAttributes(attrs map[string]interface{}) (dataitem.ItemDescriptor, bool) {
	descriptor := dataitem.DescriptorFromAttributes(attrs)
	if descriptor.DataType != dataitem.DataTypeTable {
		return dataitem.ItemDescriptor{}, false
	}
	switch descriptor.Format {
	case "parquet", "orc", "avro":
		return descriptor, true
	default:
		return dataitem.ItemDescriptor{}, false
	}
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
