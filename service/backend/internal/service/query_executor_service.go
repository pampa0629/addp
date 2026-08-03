package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/sqldialect"
	"github.com/addp/service/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// QueryExecutorService 执行已发布查询服务的结构化查询计划。
type QueryExecutorService struct {
	systemClient  *client.SystemClient
	systemService *client.SystemServiceClient
	tokenCodec    *queryTokenCodec
}

func NewQueryExecutorService(
	systemClient *client.SystemClient,
	systemService *client.SystemServiceClient,
	encryptionKey []byte,
) *QueryExecutorService {
	return &QueryExecutorService{
		systemClient: systemClient, systemService: systemService,
		tokenCodec: newQueryTokenCodec(encryptionKey),
	}
}

// ExecuteQuery 执行 REST 查询服务请求。
func (s *QueryExecutorService) ExecuteQuery(
	ctx context.Context,
	queryService *models.QueryService,
	request *models.QueryExecutionRequest,
) (*models.QueryExecutionResult, error) {
	return s.execute(ctx, queryService, request, queryProtocolREST)
}

// ExecuteOGCQuery 执行 OGC API Features 适配后的结构化请求。
func (s *QueryExecutorService) ExecuteOGCQuery(
	ctx context.Context,
	queryService *models.QueryService,
	request *models.QueryExecutionRequest,
) (*models.QueryExecutionResult, error) {
	return s.execute(ctx, queryService, request, queryProtocolOGC)
}

func (s *QueryExecutorService) execute(
	ctx context.Context,
	queryService *models.QueryService,
	request *models.QueryExecutionRequest,
	protocol queryProtocol,
) (*models.QueryExecutionResult, error) {
	if queryService == nil || request == nil {
		return nil, fmt.Errorf("%w: query service request is incomplete", ErrInvalidStructuredQuery)
	}
	if queryService.UsesFederatedQueryRuntime() {
		return s.executeFederatedQuery(ctx, queryService, request, protocol)
	}
	return s.executeDirectQuery(ctx, queryService, request, protocol)
}

func (s *QueryExecutorService) executeDirectQuery(
	ctx context.Context,
	queryService *models.QueryService,
	request *models.QueryExecutionRequest,
	protocol queryProtocol,
) (*models.QueryExecutionResult, error) {
	engine, err := s.systemClient.GetEngine(queryService.GetEngineID())
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}
	commonEngine := &commonModels.Engine{
		ID: engine.ID, EngineType: engine.EngineType,
		ConnectionInfo: commonModels.ConnectionInfo(engine.ConnectionInfo),
	}
	db, err := dbbridge.GetOrCreatePool(commonEngine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}
	baseSQL, err := directSourceSQL(queryService, engine.EngineType)
	if err != nil {
		return nil, err
	}
	plan, err := compileQueryPlan(queryService, request, protocol, engine.EngineType, baseSQL, s.tokenCodec)
	if err != nil {
		return nil, err
	}
	var rows []map[string]interface{}
	if err := db.WithContext(ctx).Raw(plan.SQL, plan.Args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("execute query plan: %w", err)
	}
	return s.finalizeResult(queryService, plan, rows)
}

func directSourceSQL(queryService *models.QueryService, engineType string) (string, error) {
	if queryService.ConfigType == "sql" {
		query := strings.TrimSpace(queryService.SqlQuery)
		if query == "" {
			return "", fmt.Errorf("query service SQL is missing")
		}
		return query, nil
	}
	if queryService.ConfigType != "table" || strings.TrimSpace(queryService.TargetTable) == "" {
		return "", fmt.Errorf("query service table source is missing")
	}
	dialect := sqldialect.ForEngine(engineType)
	table := dialect.QuoteIdentifier(queryService.TargetTable)
	if strings.TrimSpace(queryService.SchemaName) != "" {
		table = dialect.QuoteIdentifier(queryService.SchemaName) + "." + table
	}
	return "SELECT * FROM " + table, nil
}

func (s *QueryExecutorService) executeFederatedQuery(
	ctx context.Context,
	queryService *models.QueryService,
	request *models.QueryExecutionRequest,
	protocol queryProtocol,
) (*models.QueryExecutionResult, error) {
	snapshot := queryService.SourceSnapshot()
	if snapshot == nil || queryService.RuntimeEngineID == nil || *queryService.RuntimeEngineID == 0 || s.systemService == nil {
		return nil, fmt.Errorf("query service dependency snapshot is missing")
	}
	if snapshot.DependencyHash == "" || queryServiceDependencyHash(snapshot) != snapshot.DependencyHash {
		return nil, fmt.Errorf("query service dependency snapshot hash is invalid")
	}
	runtimeDescriptor, err := s.systemService.WithTenantID(queryService.TenantID).
		GetEngineRuntimeDescriptor(ctx, *queryService.RuntimeEngineID)
	if err != nil {
		return nil, fmt.Errorf("get federated query runtime: %w", err)
	}
	enginePlugin, err := plugin.Get(runtimeDescriptor.EngineType)
	if err != nil {
		return nil, err
	}
	provider, ok := enginePlugin.(plugin.FederatedQueryRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("engine %d does not implement federated query runtime", *queryService.RuntimeEngineID)
	}
	baseSQL, sourceEngineIDs, objectTables, err := s.federatedSourceSQL(ctx, queryService)
	if err != nil {
		return nil, err
	}
	if len(sourceEngineIDs) == 0 {
		return nil, fmt.Errorf("query service dependency snapshot has no source engine")
	}
	plan, err := compileQueryPlan(queryService, request, protocol, runtimeDescriptor.EngineType, baseSQL, s.tokenCodec)
	if err != nil {
		return nil, err
	}
	executionID := uuid.New()
	issued, err := s.systemService.WithTenantID(queryService.TenantID).
		IssueExecutionAuthorizationFromServiceDefinition(ctx, client.IssueExecutionAuthorizationFromServiceDefinitionRequest{
			ExecutionID: executionID.String(), EngineIDs: formatServiceEngineIDs(sourceEngineIDs),
			DefinitionID: strconv.FormatUint(uint64(queryService.ID), 10), DefinitionVersion: snapshot.DependencyHash,
			ExpiresIn: 60,
		})
	if err != nil {
		return nil, fmt.Errorf("issue service definition execution authorization: %w", err)
	}
	callerToken, err := s.systemService.TenantServiceAccessToken(ctx, queryService.TenantID)
	if err != nil {
		return nil, err
	}
	result, err := provider.ExecuteFederatedQuery(ctx, plugin.ConnectionInfo(runtimeDescriptor.AsEngine().ConnectionInfo), plugin.FederatedQueryRequest{
		ExecutionID: executionID.String(), ExecutionAuthorizationID: issued.ID,
		SourceEngineIDs: sourceEngineIDs, ObjectTables: objectTables,
		Query: plan.SQL, Language: "sql",
		Options:           federatedQueryOptions(queryService, plan),
		CallerAccessToken: callerToken,
	})
	if err != nil {
		return nil, fmt.Errorf("execute federated query plan: %w", err)
	}
	return s.finalizeResult(queryService, plan, result.Rows)
}

func federatedQueryOptions(queryService *models.QueryService, plan *compiledQueryPlan) plugin.QueryOptions {
	return plugin.QueryOptions{
		Limit: plan.Limit + 1, Timeout: 60 * time.Second, ReadOnly: true, Args: plan.Args,
		Spatial: queryService.HasGeometry(),
	}
}

func (s *QueryExecutorService) federatedSourceSQL(
	ctx context.Context,
	queryService *models.QueryService,
) (string, []uint, map[string]map[string]string, error) {
	snapshot := queryService.SourceSnapshot()
	if queryService.ConfigType == "table" && queryService.IsObjectTable() {
		if queryService.EngineID == nil || *queryService.EngineID == 0 || queryService.GetObjectTablePhysicalPath() == "" ||
			len(snapshot.FederatedSourceEngineIDs) != 1 || snapshot.FederatedSourceEngineIDs[0] != *queryService.EngineID {
			return "", nil, nil, fmt.Errorf("object table source snapshot is incomplete")
		}
		descriptor, err := s.systemService.WithTenantID(queryService.TenantID).GetEngineRuntimeDescriptor(ctx, *queryService.EngineID)
		if err != nil {
			return "", nil, nil, err
		}
		engineName := sanitizeFederatedIdentifier(descriptor.Name)
		tableName := sanitizeFederatedIdentifier(queryService.TargetTable)
		return fmt.Sprintf("SELECT * FROM %s.%s", engineName, tableName),
			[]uint{*queryService.EngineID},
			map[string]map[string]string{engineName: {tableName: queryService.GetObjectTablePhysicalPath()}}, nil
	}
	if queryService.ConfigType != "sql" || strings.TrimSpace(queryService.SqlQuery) == "" {
		return "", nil, nil, fmt.Errorf("federated query service SQL is missing")
	}
	return queryService.SqlQuery, append([]uint(nil), snapshot.FederatedSourceEngineIDs...),
		cloneObjectTableMap(snapshot.FederatedObjectTables), nil
}

func (s *QueryExecutorService) finalizeResult(
	queryService *models.QueryService,
	plan *compiledQueryPlan,
	rows []map[string]interface{},
) (*models.QueryExecutionResult, error) {
	hasMore := len(rows) > plan.Limit
	if hasMore {
		rows = rows[:plan.Limit]
	}
	featureIDs := make([]string, len(rows))
	for index, row := range rows {
		featureID, err := s.encodeFeatureID(queryService, row)
		if err != nil {
			return nil, err
		}
		featureIDs[index] = featureID
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		values, err := rowValues(rows[len(rows)-1], plan.OrderBy)
		if err != nil {
			return nil, err
		}
		nextCursor, err = s.tokenCodec.encodeCursor(queryCursorPayload{
			ServiceID: queryService.ID, ServiceVersion: plan.ServiceVersion,
			QueryHash: plan.QueryHash, OrderBy: plan.OrderBy, Values: values,
		})
		if err != nil {
			return nil, err
		}
	}
	for _, row := range rows {
		for _, field := range plan.HiddenFields {
			delete(row, field)
		}
	}
	return &models.QueryExecutionResult{
		Data:           rows,
		Page:           models.QueryPageResult{Limit: plan.Limit, HasMore: hasMore, NextCursor: nextCursor},
		ServiceVersion: plan.ServiceVersion,
		Fields:         append([]string(nil), plan.SelectedFields...), FeatureIDs: featureIDs,
	}, nil
}

func rowValues(row map[string]interface{}, orderBy []models.QueryOrder) ([]interface{}, error) {
	values := make([]interface{}, len(orderBy))
	for index, order := range orderBy {
		value, exists := row[order.Field]
		if !exists || value == nil {
			return nil, fmt.Errorf("query result is missing stable order field %s", order.Field)
		}
		values[index] = value
	}
	return values, nil
}

func (s *QueryExecutorService) encodeFeatureID(queryService *models.QueryService, row map[string]interface{}) (string, error) {
	stableKey := queryService.GetStableKey()
	values := make([]interface{}, len(stableKey))
	for index, field := range stableKey {
		value, exists := row[field]
		if !exists || value == nil {
			return "", fmt.Errorf("query result is missing stable key field %s", field)
		}
		values[index] = value
	}
	return s.tokenCodec.encodeFeatureID(featureIDPayload{
		ServiceID: queryService.ID, ServiceVersion: serviceDependencyVersion(queryService),
		Fields: stableKey, Values: values,
	})
}

func (s *QueryExecutorService) DecodeFeatureID(queryService *models.QueryService, token string) (*models.QueryFilter, error) {
	payload, err := s.tokenCodec.decodeFeatureID(token)
	stableKey := queryService.GetStableKey()
	if err != nil || payload.ServiceID != queryService.ID || payload.ServiceVersion != serviceDependencyVersion(queryService) ||
		len(payload.Fields) != len(stableKey) || len(payload.Values) != len(stableKey) {
		return nil, ErrInvalidFeatureID
	}
	filters := make([]models.QueryFilter, len(stableKey))
	for index, field := range stableKey {
		if payload.Fields[index] != field || payload.Values[index] == nil {
			return nil, ErrInvalidFeatureID
		}
		filters[index] = models.QueryFilter{Field: field, Op: "eq", Value: payload.Values[index]}
	}
	if len(filters) == 1 {
		return &filters[0], nil
	}
	return &models.QueryFilter{And: filters}, nil
}

func formatServiceEngineIDs(engineIDs []uint) []string {
	values := make([]string, len(engineIDs))
	for index, engineID := range engineIDs {
		values[index] = strconv.FormatUint(uint64(engineID), 10)
	}
	return values
}

func (s *QueryExecutorService) FormatAsCSV(result *models.QueryExecutionResult) ([]byte, error) {
	if result == nil || len(result.Data) == 0 {
		return []byte{}, nil
	}
	columns := append([]string(nil), result.Fields...)
	if len(columns) == 0 {
		for column := range result.Data[0] {
			columns = append(columns, column)
		}
		sort.Strings(columns)
	}
	var buffer strings.Builder
	writer := csv.NewWriter(&buffer)
	if err := writer.Write(columns); err != nil {
		return nil, err
	}
	for _, row := range result.Data {
		record := make([]string, len(columns))
		for index, column := range columns {
			record[index] = fmt.Sprintf("%v", row[column])
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return []byte(buffer.String()), nil
}

func (s *QueryExecutorService) FormatAsGeoJSON(result *models.QueryExecutionResult, queryService *models.QueryService) ([]byte, error) {
	features, err := s.geoJSONFeatures(result, queryService)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]interface{}{
		"type": "FeatureCollection", "features": features,
		"page": result.Page, "service_version": result.ServiceVersion,
	})
}

func (s *QueryExecutorService) FormatFirstAsGeoJSON(result *models.QueryExecutionResult, queryService *models.QueryService) ([]byte, error) {
	features, err := s.geoJSONFeatures(result, queryService)
	if err != nil {
		return nil, err
	}
	if len(features) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return json.Marshal(features[0])
}

func (s *QueryExecutorService) geoJSONFeatures(result *models.QueryExecutionResult, queryService *models.QueryService) ([]map[string]interface{}, error) {
	if result == nil || queryService == nil || !queryService.HasGeometry() {
		return nil, fmt.Errorf("service does not have geometry column")
	}
	if len(result.FeatureIDs) != len(result.Data) {
		return nil, errors.New("query result feature ids are incomplete")
	}
	geometryColumn := queryService.GetGeometryColumn()
	features := make([]map[string]interface{}, len(result.Data))
	for index, row := range result.Data {
		geometryValue, exists := row[geometryColumn]
		if !exists {
			return nil, fmt.Errorf("geometry column %s not found in row", geometryColumn)
		}
		var geometry map[string]interface{}
		switch value := geometryValue.(type) {
		case string:
			if err := json.Unmarshal([]byte(value), &geometry); err != nil {
				return nil, fmt.Errorf("failed to parse geometry: %w", err)
			}
		case []byte:
			if err := json.Unmarshal(value, &geometry); err != nil {
				return nil, fmt.Errorf("failed to parse geometry: %w", err)
			}
		case map[string]interface{}:
			geometry = value
		default:
			return nil, fmt.Errorf("geometry data has unsupported type %T", geometryValue)
		}
		properties := make(map[string]interface{}, len(row)-1)
		for key, value := range row {
			if key != geometryColumn {
				properties[key] = value
			}
		}
		features[index] = map[string]interface{}{
			"type": "Feature", "id": result.FeatureIDs[index],
			"geometry": geometry, "properties": properties,
		}
	}
	return features, nil
}
