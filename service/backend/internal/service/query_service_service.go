package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	commonquery "github.com/addp/common/query"
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
		if err := commonquery.RequireReadOnly(req.SqlQuery); err != nil {
			return nil, fmt.Errorf("sql_query must be read-only: %w", err)
		}
		if req.OutputContract == nil || req.OutputContract.Table == nil || len(req.OutputContract.Table.Fields) == 0 {
			return nil, errors.New("sql mode requires a detected output_contract")
		}
		if req.SchemaName != "" || req.TableName != "" {
			return nil, errors.New("schema_name and table_name should not be provided in sql mode")
		}
		if req.RuntimeEngineID != nil && *req.RuntimeEngineID > 0 {
			if req.EngineID != nil {
				return nil, errors.New("federated sql mode must not provide engine_id")
			}
		} else if req.EngineID == nil || *req.EngineID == 0 {
			return nil, errors.New("sql mode requires engine_id or runtime_engine_id")
		} else if err := s.validateDirectSQLQueryEngine(*req.EngineID); err != nil {
			return nil, err
		}
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
		if req.RuntimeEngineID != nil && *req.RuntimeEngineID > 0 {
			if err := s.validateFederatedRuntime(*req.RuntimeEngineID, ""); err != nil {
				return nil, err
			}
			sourceEngineIDs, objectTables, err := s.captureFederatedDependencies(tenantID, *req.RuntimeEngineID, req.SqlQuery)
			if err != nil {
				return nil, err
			}
			snapshot.FederatedSourceEngineIDs = sourceEngineIDs
			snapshot.FederatedObjectTables = objectTables
			snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
		}
	}
	stableKey, err := publishedStableKey(req.ConfigType, dataConfig, snapshot)
	if err != nil {
		return nil, err
	}
	if err := validateQueryFieldPolicy(dataConfig, snapshot.Table); err != nil {
		return nil, err
	}
	snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
	dataConfig["stable_key"] = stableKey
	if req.ConfigType == "table" {
		if snapshot != nil && snapshot.ObjectTable != nil {
			if req.RuntimeEngineID == nil || *req.RuntimeEngineID == 0 {
				return nil, errors.New("object table service requires runtime_engine_id")
			}
			if err := s.validateFederatedRuntime(*req.RuntimeEngineID, snapshot.ObjectTable.Format); err != nil {
				return nil, err
			}
			snapshot.FederatedSourceEngineIDs = []uint{tableRef.EngineID}
			snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
		} else if req.RuntimeEngineID != nil {
			return nil, errors.New("relational table service must not provide runtime_engine_id")
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

		ConfigType:      req.ConfigType,
		EngineID:        req.EngineID,
		RuntimeEngineID: req.RuntimeEngineID,

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
	if err := s.recordLineagePublication(service); err != nil {
		return nil, fmt.Errorf("record service lineage publication failed: %w", err)
	}

	// 11. 重新加载服务（获取完整数据）
	service, err = s.repo.GetByID(service.ID)
	if err != nil {
		return nil, fmt.Errorf("get created service failed: %w", err)
	}

	return s.convertToDTO(service), nil
}

func (s *QueryServiceService) validateDirectSQLQueryEngine(engineID uint) error {
	if s.systemClient == nil || engineID == 0 {
		return errors.New("System client and engine_id are required")
	}
	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return fmt.Errorf("get SQL query engine %d failed: %w", engineID, err)
	}
	if !engineselection.IsAvailableForComputeEntrypoint(engine, "query") {
		return fmt.Errorf("SQL query engine %d is not currently available", engineID)
	}
	enginePlugin, err := plugin.Get(engine.EngineType)
	if err != nil {
		return err
	}
	provider, ok := enginePlugin.(plugin.SQLQueryRuntimeProvider)
	if !ok || !containsQueryLanguage(provider.QueryLanguages(), "sql") {
		return fmt.Errorf("engine %d does not support SQL query runtime", engineID)
	}
	return nil
}

func (s *QueryServiceService) validateFederatedRuntime(engineID uint, objectFormat string) error {
	if s.systemClient == nil || engineID == 0 {
		return errors.New("System client and runtime_engine_id are required")
	}
	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return fmt.Errorf("get federated query runtime %d failed: %w", engineID, err)
	}
	enginePlugin, err := plugin.Get(engine.EngineType)
	if err != nil {
		return err
	}
	if _, ok := enginePlugin.(plugin.FederatedQueryRuntimeProvider); !ok {
		return fmt.Errorf("engine %d does not support federated query runtime", engineID)
	}
	if !engineselection.IsAvailableForComputeEntrypoint(engine, "query") {
		return fmt.Errorf("federated query runtime %d is not currently available", engineID)
	}
	if objectFormat != "" && !supportsFederatedObjectFormat(enginePlugin.Capabilities(), objectFormat) {
		return fmt.Errorf("federated query runtime %d does not support object format %s", engineID, objectFormat)
	}
	return nil
}

func supportsFederatedObjectFormat(capabilities plugin.EngineCapabilities, objectFormat string) bool {
	if capabilities.Compute == nil || capabilities.Compute.Query == nil || capabilities.Compute.Query.Federation == nil {
		return false
	}
	for _, supported := range capabilities.Compute.Query.Federation.ObjectFormats {
		if supported == objectFormat {
			return true
		}
	}
	return false
}

func (s *QueryServiceService) captureFederatedDependencies(
	tenantID, runtimeEngineID uint,
	sql string,
) ([]uint, map[string]map[string]string, error) {
	if s.systemClient == nil {
		return nil, nil, errors.New("system client is required for federated SQL publication")
	}
	runtimeEngine, err := s.systemClient.GetEngine(runtimeEngineID)
	if err != nil {
		return nil, nil, fmt.Errorf("get federated query runtime failed: %w", err)
	}
	enginePlugin, err := plugin.Get(runtimeEngine.EngineType)
	if err != nil {
		return nil, nil, err
	}
	provider, ok := enginePlugin.(plugin.FederatedQueryRuntimeProvider)
	if !ok {
		return nil, nil, fmt.Errorf("engine %d is not a federated query runtime", runtimeEngineID)
	}
	engines, err := s.systemClient.ListEngines("", tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("list federated engines failed: %w", err)
	}
	candidates := make([]plugin.FederatedQuerySource, 0, len(engines))
	for _, engine := range engines {
		candidates = append(candidates, plugin.FederatedQuerySource{
			ID: engine.ID, Name: engine.Name, EngineType: engine.EngineType, LifecycleState: engine.LifecycleState,
		})
	}
	referencedIDs := provider.ResolveSourceEngineIDs(sql, candidates)
	if len(referencedIDs) == 0 {
		return nil, nil, errors.New("federated SQL must reference at least one active supported source engine")
	}
	referenced := make(map[uint]struct{}, len(referencedIDs))
	for _, id := range referencedIDs {
		referenced[id] = struct{}{}
	}
	result := make(map[string]map[string]string)
	objectReferences := provider.ResolveObjectTableReferences(sql, candidates)
	for _, engine := range engines {
		if _, ok := referenced[engine.ID]; !ok || (engine.EngineType != "minio" && engine.EngineType != "s3") {
			continue
		}
		if s.metaClient == nil {
			return nil, nil, errors.New("meta client is required for object table publication")
		}
		items, itemErr := s.metaClient.WithTenantID(tenantID).ListEngineItems(engine.ID, "")
		if itemErr != nil {
			return nil, nil, fmt.Errorf("list object tables for engine %d failed: %w", engine.ID, itemErr)
		}
		engineAlias := sanitizeFederatedIdentifier(engine.Name)
		matchedReferences := 0
		for _, reference := range objectReferences {
			if reference.SourceName != engine.Name && reference.SourceName != engineAlias {
				continue
			}
			physicalPath := ""
			for _, item := range items {
				descriptor, descriptorOK := objectTableDescriptorFromMetaItem(item)
				if !descriptorOK || !supportsFederatedObjectFormat(enginePlugin.Capabilities(), descriptor.Format) ||
					!matchesFederatedObjectTable(reference.TableName, item.Name, item.FullName) {
					continue
				}
				physicalPath = descriptor.PhysicalPath
				break
			}
			if physicalPath == "" {
				return nil, nil, fmt.Errorf("object table %s.%s could not be resolved from the published Meta catalog", reference.SourceName, reference.TableName)
			}
			if result[reference.SourceName] == nil {
				result[reference.SourceName] = make(map[string]string)
			}
			result[reference.SourceName][reference.TableName] = physicalPath
			matchedReferences++
		}
		if matchedReferences == 0 {
			return nil, nil, fmt.Errorf("object source engine %s has no referenced table", engine.Name)
		}
	}
	return sortedEngineIDs(referencedIDs), result, nil
}

func matchesFederatedObjectTable(reference, itemName, itemFullName string) bool {
	for _, candidate := range []string{itemName, sanitizeFederatedIdentifier(itemName), itemFullName} {
		if candidate != "" && reference == candidate {
			return true
		}
	}
	if itemFullName != "" {
		for _, separator := range []string{".", "/"} {
			if strings.HasSuffix(itemFullName, separator+reference) {
				return true
			}
		}
	}
	return false
}

func sanitizeFederatedIdentifier(value string) string {
	var result strings.Builder
	for _, char := range value {
		if char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			result.WriteRune(char)
		} else {
			result.WriteByte('_')
		}
	}
	value = result.String()
	if value == "" {
		return "engine"
	}
	if value[0] >= '0' && value[0] <= '9' {
		return "_" + value
	}
	return value
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
			result[key] = value
		case "locator":
			if configType != "table" {
				return nil, errors.New("data_config.locator is only valid in table mode")
			}
			result[key] = value
		case "stable_key":
			if configType != "sql" {
				return nil, errors.New("data_config.stable_key is managed by Service in table mode")
			}
			stableKey, err := normalizeStableKey(value)
			if err != nil {
				return nil, err
			}
			result[key] = stableKey
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
			result[key] = value
		case "locator", "stable_key", models.QueryServiceSourceSnapshotKey, "geometry", "object_table":
			return nil, fmt.Errorf("data_config.%s cannot be changed after publication", key)
		default:
			return nil, fmt.Errorf("unsupported data_config field %s", key)
		}
	}
	return result, nil
}

func publishedStableKey(configType string, dataConfig models.JSONB, snapshot *models.QueryServiceDependencySnapshot) ([]string, error) {
	if snapshot == nil {
		return nil, errors.New("query service output contract is missing")
	}
	if configType == "table" {
		if snapshot.Table == nil || len(snapshot.Table.PrimaryKey) == 0 {
			return nil, errors.New("table query service requires a non-empty primary key as stable_key")
		}
		return validateStableKey(snapshot.Table.PrimaryKey, snapshot.Table)
	}
	stableKey, err := normalizeStableKey(dataConfig["stable_key"])
	if err != nil {
		return nil, err
	}
	if snapshot.Table != nil && len(snapshot.Table.Fields) > 0 {
		validated, err := validateStableKeyFields(stableKey, snapshot.Table, false)
		if err != nil {
			return nil, err
		}
		stableFields := make(map[string]struct{}, len(validated))
		for _, name := range validated {
			stableFields[name] = struct{}{}
		}
		for index := range snapshot.Table.Fields {
			if _, stable := stableFields[snapshot.Table.Fields[index].Name]; stable {
				// SQL 模式的 stable_key 是发布者对非空唯一性的显式契约。
				snapshot.Table.Fields[index].Nullable = false
			}
		}
		return validated, nil
	}
	return stableKey, nil
}

func normalizeStableKey(value interface{}) ([]string, error) {
	var values []interface{}
	switch typed := value.(type) {
	case []interface{}:
		values = typed
	case []string:
		values = make([]interface{}, len(typed))
		for index := range typed {
			values[index] = typed[index]
		}
	default:
		return nil, errors.New("data_config.stable_key is required and must be an array")
	}
	if len(values) == 0 {
		return nil, errors.New("data_config.stable_key must not be empty")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		field, ok := value.(string)
		field = strings.TrimSpace(field)
		if !ok || field == "" {
			return nil, errors.New("data_config.stable_key contains an invalid field")
		}
		key := strings.ToLower(field)
		if _, exists := seen[key]; exists {
			return nil, errors.New("data_config.stable_key contains duplicate fields")
		}
		seen[key] = struct{}{}
		result = append(result, field)
	}
	return result, nil
}

func validateStableKey(stableKey []string, table *datatype.TableInfo) ([]string, error) {
	return validateStableKeyFields(stableKey, table, true)
}

func validateStableKeyFields(stableKey []string, table *datatype.TableInfo, requireNonNull bool) ([]string, error) {
	if len(stableKey) == 0 || table == nil {
		return nil, errors.New("query service stable_key is missing from output contract")
	}
	fields := make(map[string]datatype.FieldInfo, len(table.Fields))
	for _, field := range table.Fields {
		fields[field.Name] = field
	}
	for _, name := range stableKey {
		field, exists := fields[name]
		if !exists {
			return nil, fmt.Errorf("stable_key field %s is not present in output contract", name)
		}
		if !isStableOrderFieldType(field.Type) {
			return nil, fmt.Errorf("stable_key field %s must use a supported scalar type", name)
		}
		if requireNonNull && field.Nullable {
			return nil, fmt.Errorf("stable_key field %s must not be nullable", name)
		}
	}
	return append([]string(nil), stableKey...), nil
}

func validateQueryFieldPolicy(dataConfig models.JSONB, table *datatype.TableInfo) error {
	if table == nil {
		return errors.New("query service output contract is missing")
	}
	available := make(map[string]struct{}, len(table.Fields))
	for _, field := range table.Fields {
		available[field.Name] = struct{}{}
	}
	for _, key := range []string{"default_fields", "filterable_fields"} {
		value, exists := dataConfig[key]
		if !exists {
			continue
		}
		fields, err := normalizeQueryFieldList(value, key)
		if err != nil {
			return err
		}
		for _, field := range fields {
			if _, exists := available[field]; !exists {
				return fmt.Errorf("data_config.%s field %s is not present in output contract", key, field)
			}
		}
		dataConfig[key] = fields
	}
	return nil
}

func normalizeQueryFieldList(value interface{}, key string) ([]string, error) {
	var raw []interface{}
	switch typed := value.(type) {
	case []interface{}:
		raw = typed
	case []string:
		raw = make([]interface{}, len(typed))
		for index := range typed {
			raw[index] = typed[index]
		}
	default:
		return nil, fmt.Errorf("data_config.%s must be an array", key)
	}
	result := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		field, ok := item.(string)
		field = strings.TrimSpace(field)
		if !ok || field == "" {
			return nil, fmt.Errorf("data_config.%s contains an invalid field", key)
		}
		if _, duplicate := seen[field]; duplicate {
			return nil, fmt.Errorf("data_config.%s contains duplicate field %s", key, field)
		}
		seen[field] = struct{}{}
		result = append(result, field)
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
		if err := validateQueryFieldPolicy(currentConfig, service.GetTableInfo()); err != nil {
			return nil, err
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
	stableKey, err := publishedStableKey("table", service.DataConfig, current)
	if err != nil {
		return nil, fmt.Errorf("refresh stable key failed: %w", err)
	}
	dataConfig := service.DataConfig
	if dataConfig == nil {
		dataConfig = models.JSONB{}
	}
	dataConfig["stable_key"] = stableKey
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
	if err := s.recordLineagePublication(updated); err != nil {
		return nil, fmt.Errorf("record refreshed service lineage publication failed: %w", err)
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

func (s *QueryServiceService) recordLineagePublication(service *models.QueryService) error {
	if s == nil || s.metaClient == nil || service == nil {
		return nil
	}
	snapshot := service.SourceSnapshot()
	if snapshot == nil || snapshot.Source == nil || snapshot.Source.ItemID == 0 {
		return nil
	}
	revision := snapshot.DependencyHash
	if strings.TrimSpace(revision) == "" {
		return nil
	}
	return s.metaClient.WithTenantID(service.TenantID).RecordServicePublication(context.Background(), client.MetaLineageServicePublication{
		ServiceID: service.ID, PublishedRevision: revision, DependencyHash: snapshot.DependencyHash,
		Dependencies: []client.MetaLineageServiceDependency{{SourceItemID: snapshot.Source.ItemID, DependencyKind: service.ConfigType, Granularity: "item"}},
	})
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

		ConfigType:      service.ConfigType,
		EngineID:        service.EngineID,
		RuntimeEngineID: service.RuntimeEngineID,

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
		endpoints["rest_api"] = fmt.Sprintf("%s/api/query/%s/query", s.baseURL, service.ServiceName)
	}

	// OGC Features 端点
	if service.IsOGCFeaturesEnabled() {
		endpoints["ogc_features"] = fmt.Sprintf("%s/ogc/features/%s", s.baseURL, service.ServiceName)
		endpoints["ogc_features_collections"] = fmt.Sprintf("%s/ogc/features/%s/collections", s.baseURL, service.ServiceName)
	}

	return endpoints
}
