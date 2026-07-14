package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/duckdb"
	"github.com/addp/common/resourcetree"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

type QueryServiceService struct {
	repo         *repository.QueryServiceRepository
	systemClient *client.SystemClient
	metaClient   *client.MetaClient
	baseURL      string // 服务的基础URL（用于构建端点）
}

type tableResourceRef struct {
	Locator    string
	EngineID   uint
	SchemaName string
	TableName  string
	ItemID     uint
}

func NewQueryServiceService(repo *repository.QueryServiceRepository, systemClient *client.SystemClient, metaClient *client.MetaClient, baseURL string) *QueryServiceService {
	return &QueryServiceService{
		repo:         repo,
		systemClient: systemClient,
		metaClient:   metaClient,
		baseURL:      baseURL,
	}
}

// CreateService 创建新的查询服务
func (s *QueryServiceService) CreateService(req *models.CreateQueryServiceRequest, tenantID uint, createdBy uint) (*models.QueryServiceDTO, error) {
	// 1. 验证配置类型
	if req.ConfigType != "table" && req.ConfigType != "sql" {
		return nil, errors.New("invalid config_type: must be 'table' or 'sql'")
	}

	var tableRef *tableResourceRef
	var err error

	// 2. Table模式的特殊验证
	if req.ConfigType == "table" {
		if req.SqlQuery != "" {
			return nil, errors.New("sql_query should not be provided in table mode")
		}
		tableRef, err = tableResourceRefFromRequest(req)
		if err != nil {
			return nil, err
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

	// 5. 构建仅包含用户配置的 data_config；依赖事实由 Service 生成。
	dataConfig, err := queryServiceUserDataConfig(req.DataConfig, req.ConfigType)
	if err != nil {
		return nil, err
	}

	// 6. 构建并冻结依赖快照。
	var snapshot *models.QueryServiceDependencySnapshot
	if req.ConfigType == "table" {
		engineID := tableRef.EngineID
		req.EngineID = &engineID
		req.SchemaName = tableRef.SchemaName
		req.TableName = tableRef.TableName
		dataConfig["locator"] = tableRef.Locator
		if s.metaClient == nil {
			return nil, errors.New("meta client is required for table mode")
		}
		item, err := s.metaClient.WithTenantID(tenantID).GetItemByID(tableRef.ItemID)
		if err != nil {
			return nil, fmt.Errorf("get meta item %d failed: %w", tableRef.ItemID, err)
		}
		if item.ID != tableRef.ItemID || item.EngineID != tableRef.EngineID {
			return nil, errors.New("meta item identity does not match data_config.locator")
		}
		snapshot, err = buildTableDependencySnapshot(item, time.Now())
		if err != nil {
			return nil, fmt.Errorf("build table dependency snapshot failed: %w", err)
		}
	} else {
		snapshot = buildSQLDependencySnapshot(req.SqlQuery, req.OutputContract, time.Now())
		if req.EngineID == nil || *req.EngineID == 0 {
			objectTables, err := s.captureFederatedObjectTables(tenantID, req.SqlQuery)
			if err != nil {
				return nil, err
			}
			snapshot.FederatedObjectTables = objectTables
			snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
		}
	}
	dataConfig[models.QueryServiceSourceSnapshotKey] = queryServiceSnapshotPayload(snapshot)

	// 7. 构建协议配置
	protocols := s.buildProtocolsConfig(req.Protocols, snapshot != nil && snapshot.Spatial != nil && snapshot.Spatial.PrimaryGeometryName() != "")

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

func (s *QueryServiceService) captureFederatedObjectTables(tenantID uint, sql string) (map[string]map[string]string, error) {
	referenced := duckdb.ExtractReferencedEngineNames(sql)
	if len(referenced) == 0 {
		return nil, nil
	}
	if s.systemClient == nil {
		return nil, errors.New("system client is required for DuckDB federated SQL publication")
	}
	engines, err := s.systemClient.ListEngines("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("list federated engines failed: %w", err)
	}
	engines = duckdb.FilterEnginesByName(engines, referenced)
	hasObjectEngine := false
	for _, engine := range engines {
		if duckdb.IsObjectTableEngine(engine.EngineType) {
			hasObjectEngine = true
			break
		}
	}
	if !hasObjectEngine {
		return nil, nil
	}
	if s.metaClient == nil {
		return nil, errors.New("meta client is required for DuckDB object table publication")
	}
	allObjectTables := duckdb.BuildObjectTableMap(context.Background(), tenantID, engines, s.metaClient.WithTenantID(tenantID))
	filtered := filterFederatedObjectTables(sql, allObjectTables)
	if len(filtered) == 0 {
		return nil, errors.New("no referenced object tables could be resolved from Meta")
	}
	return filtered, nil
}

func filterFederatedObjectTables(sql string, all map[string]map[string]string) map[string]map[string]string {
	result := map[string]map[string]string{}
	add := func(engineName, tableName string) {
		tables := all[engineName]
		if tables == nil {
			return
		}
		physicalPath, ok := tables[tableName]
		if !ok {
			return
		}
		if result[engineName] == nil {
			result[engineName] = map[string]string{}
		}
		result[engineName][tableName] = physicalPath
	}
	for _, ref := range duckdb.ExtractTableRefs(sql) {
		parts := strings.SplitN(ref, ".", 3)
		if len(parts) != 3 {
			continue
		}
		add(parts[0], parts[1]+"."+parts[2])
		add(parts[0], parts[2])
	}
	for _, ref := range duckdb.ExtractTwoPartTableRefs(sql) {
		parts := strings.SplitN(ref, ".", 2)
		if len(parts) == 2 {
			add(parts[0], parts[1])
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func tableResourceRefFromRequest(req *models.CreateQueryServiceRequest) (*tableResourceRef, error) {
	if req.DataConfig == nil {
		return nil, errors.New("data_config.locator is required for table mode")
	}
	locator, ok := req.DataConfig["locator"].(string)
	if !ok || locator == "" {
		return nil, errors.New("data_config.locator is required for table mode")
	}
	loc, err := resourcetree.ParseURI(locator)
	if err != nil {
		return nil, fmt.Errorf("invalid data_config.locator: %w", err)
	}
	if loc.Type != resourcetree.TypeTable {
		return nil, fmt.Errorf("data_config.locator must reference a table resource, got %s", loc.Type)
	}
	if loc.ItemID == nil || *loc.ItemID == 0 {
		return nil, errors.New("data_config.locator must include item_id for table mode")
	}
	if req.EngineID != nil && *req.EngineID != 0 && *req.EngineID != loc.EngineID {
		return nil, fmt.Errorf("engine_id %d does not match locator engine_id %d", *req.EngineID, loc.EngineID)
	}
	if len(loc.Path) < 2 {
		return nil, errors.New("data_config.locator path must include schema and table")
	}
	return &tableResourceRef{
		Locator:    locator,
		EngineID:   loc.EngineID,
		SchemaName: loc.Path[len(loc.Path)-2],
		TableName:  loc.Path[len(loc.Path)-1],
		ItemID:     *loc.ItemID,
	}, nil
}

func (s *QueryServiceService) buildProtocolsConfig(userProtocols map[string]interface{}, hasGeometry bool) models.JSONB {
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

	// 如果输出契约包含空间字段，自动启用 OGC Features。
	if hasGeometry {
		protocols["ogc_features"] = map[string]interface{}{
			"enabled": true,
			"version": "1.0",
		}
	}

	// 用户自定义的协议配置覆盖默认值
	if userProtocols != nil {
		for k, v := range userProtocols {
			protocols[k] = v
		}
	}
	if !hasGeometry {
		protocols["ogc_features"] = map[string]interface{}{
			"enabled": false,
			"version": "1.0",
		}
	}

	return protocols
}

func queryServiceUserDataConfig(input map[string]interface{}, configType string) (models.JSONB, error) {
	result := models.JSONB{}
	for key, value := range input {
		switch key {
		case "default_fields", "filterable_fields":
			if configType != "table" {
				return nil, fmt.Errorf("data_config.%s is only valid in table mode", key)
			}
			result[key] = value
		case "locator":
			if configType != "table" {
				return nil, errors.New("data_config.locator is only valid in table mode")
			}
			result[key] = value
		case models.QueryServiceSourceSnapshotKey, "geometry", "object_table":
			return nil, fmt.Errorf("data_config.%s is managed by Service and cannot be provided", key)
		default:
			return nil, fmt.Errorf("unsupported data_config field %s", key)
		}
	}
	return result, nil
}

func queryServiceMutableDataConfig(input map[string]interface{}, configType string) (models.JSONB, error) {
	result := models.JSONB{}
	for key, value := range input {
		switch key {
		case "default_fields", "filterable_fields":
			if configType != "table" {
				return nil, fmt.Errorf("data_config.%s is only valid in table mode", key)
			}
			result[key] = value
		case "locator", models.QueryServiceSourceSnapshotKey, "geometry", "object_table":
			return nil, fmt.Errorf("data_config.%s cannot be changed after publication", key)
		default:
			return nil, fmt.Errorf("unsupported data_config field %s", key)
		}
	}
	return result, nil
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
		mutableConfig, err := queryServiceMutableDataConfig(req.DataConfig, service.ConfigType)
		if err != nil {
			return nil, err
		}
		// 只合并用户可修改配置，locator 和 source_snapshot 保持发布时事实。
		currentConfig := service.DataConfig
		if currentConfig == nil {
			currentConfig = make(map[string]interface{})
		}
		for k, v := range mutableConfig {
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
		if !service.HasGeometry() {
			currentProtocols["ogc_features"] = map[string]interface{}{
				"enabled": false,
				"version": "1.0",
			}
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

// CheckSourceSnapshot 显式读取 Meta 当前事实并比较已发布快照。
func (s *QueryServiceService) CheckSourceSnapshot(id, tenantID uint) (*models.QueryServiceSnapshotDiff, error) {
	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}
	if service.TenantID != tenantID {
		return nil, errors.New("query service does not belong to current tenant")
	}
	published := service.SourceSnapshot()
	if service.IsSQLMode() {
		return &models.QueryServiceSnapshotDiff{
			ServiceID:               service.ID,
			Status:                  "not_applicable",
			PublishedDependencyHash: dependencyHashOf(published),
			PublishedSnapshot:       published,
		}, nil
	}
	current, err := s.currentTableDependencySnapshot(service, tenantID)
	if err != nil {
		return nil, err
	}
	return queryServiceSnapshotDiff(service.ID, published, current), nil
}

// RefreshSourceSnapshot 用 Meta 当前事实替换表模式查询服务的依赖快照。
func (s *QueryServiceService) RefreshSourceSnapshot(id, tenantID uint) (*models.QueryServiceDTO, error) {
	service, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get service failed: %w", err)
	}
	if service.TenantID != tenantID {
		return nil, errors.New("query service does not belong to current tenant")
	}
	if !service.IsTableMode() {
		return nil, errors.New("SQL mode uses an output contract snapshot and has no single Meta item to refresh")
	}
	current, err := s.currentTableDependencySnapshot(service, tenantID)
	if err != nil {
		return nil, err
	}
	dataConfig := service.DataConfig
	if dataConfig == nil {
		dataConfig = models.JSONB{}
	}
	dataConfig[models.QueryServiceSourceSnapshotKey] = queryServiceSnapshotPayload(current)

	updates := map[string]interface{}{"data_config": dataConfig}
	if current.Spatial == nil || current.Spatial.PrimaryGeometryName() == "" {
		protocols := service.Protocols
		if protocols == nil {
			protocols = models.JSONB{}
		}
		if ogc, ok := protocols["ogc_features"].(map[string]interface{}); ok {
			ogc["enabled"] = false
			protocols["ogc_features"] = ogc
		}
		updates["protocols"] = protocols
	}
	if err := s.repo.Update(id, updates); err != nil {
		return nil, fmt.Errorf("refresh source snapshot failed: %w", err)
	}
	updated, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get refreshed service failed: %w", err)
	}
	return s.convertToDTO(updated), nil
}

func (s *QueryServiceService) currentTableDependencySnapshot(service *models.QueryService, tenantID uint) (*models.QueryServiceDependencySnapshot, error) {
	if s.metaClient == nil {
		return nil, errors.New("meta client is required to check source snapshot")
	}
	published := service.SourceSnapshot()
	if published == nil || published.Source == nil || published.Source.ItemID == 0 {
		return nil, errors.New("query service source snapshot is missing Meta item identity")
	}
	item, err := s.metaClient.WithTenantID(tenantID).GetItemByID(published.Source.ItemID)
	if err != nil {
		return nil, fmt.Errorf("get current Meta item %d failed: %w", published.Source.ItemID, err)
	}
	current, err := buildTableDependencySnapshot(item, time.Now())
	if err != nil {
		return nil, fmt.Errorf("build current dependency snapshot failed: %w", err)
	}
	return current, nil
}

func dependencyHashOf(snapshot *models.QueryServiceDependencySnapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.DependencyHash
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
