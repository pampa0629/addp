package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/federatedquery"
	commonModels "github.com/addp/common/models"
	serviceModels "github.com/addp/service/internal/models"
	"github.com/google/uuid"
)

var ErrQuerySampleUnavailable = dbbridge.ErrSampleQueryUnavailable

type QuerySampleService struct {
	system  *client.SystemServiceClient
	issuer  *client.SystemExecutionAuthorizationClient
	catalog *federatedquery.Catalog
}

// DescribeFederatedSQL 通过已授权的 DuckDB Runtime 获取 SQL 的真实输出结构。
func (s *QuerySampleService) DescribeFederatedSQL(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	runtimeEngineID uint,
	query string,
) (*serviceModels.QueryServiceOutputContract, error) {
	if s == nil || s.system == nil || s.issuer == nil || s.catalog == nil || tenantID == 0 || runtimeEngineID == 0 ||
		!strings.HasPrefix(userAccessToken, "addp_at_") || strings.TrimSpace(query) == "" {
		return nil, errors.New("联邦 SQL 输出契约服务未正确初始化")
	}
	descriptor, err := s.system.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, runtimeEngineID)
	if err != nil {
		return nil, fmt.Errorf("获取联邦查询 Runtime 失败: %w", err)
	}
	enginePlugin, err := plugin.Get(descriptor.EngineType)
	if err != nil {
		return nil, err
	}
	provider, ok := enginePlugin.(plugin.FederatedQueryRuntimeProvider)
	if !ok {
		return nil, errors.New("所选引擎不是联邦查询 Runtime")
	}
	sources, err := s.catalog.Sources(ctx, tenantID, runtimeEngineID, provider)
	if err != nil {
		return nil, err
	}
	candidates := make([]plugin.FederatedQuerySource, 0, len(sources))
	for _, source := range sources {
		candidates = append(candidates, plugin.FederatedQuerySource{
			ID: source.EngineID, Name: source.EngineName, EngineType: source.EngineType,
			LifecycleState: commonModels.EngineLifecycleActive,
		})
	}
	sourceEngineIDs := provider.ResolveSourceEngineIDs(query, candidates)
	if len(sourceEngineIDs) == 0 {
		return nil, errors.New("联邦 SQL 未引用已发布的业务数据源")
	}
	objectTables := federatedDescribeObjectTables(query, provider, candidates, sources)
	executionID := uuid.New()
	engineIDs := make([]string, len(sourceEngineIDs))
	for index, engineID := range sourceEngineIDs {
		engineIDs[index] = strconv.FormatUint(uint64(engineID), 10)
	}
	issued, err := s.issuer.Issue(ctx, userAccessToken, client.IssueExecutionAuthorizationRequest{
		Audience: "duckdb", ExecutionID: executionID.String(), EngineIDs: engineIDs,
		Effects: []string{"read"}, ExpiresIn: 60,
	})
	if err != nil {
		return nil, fmt.Errorf("签发联邦 SQL 输出契约执行授权失败: %w", err)
	}
	callerToken, err := s.system.TenantServiceAccessToken(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("获取 Service Runtime 凭据失败: %w", err)
	}
	result, err := provider.ExecuteFederatedQuery(ctx, plugin.ConnectionInfo(descriptor.AsEngine().ConnectionInfo), plugin.FederatedQueryRequest{
		ExecutionID: executionID.String(), ExecutionAuthorizationID: issued.ID,
		SourceEngineIDs: sourceEngineIDs, ObjectTables: objectTables,
		Query: query, Language: "sql",
		Options:           plugin.QueryOptions{Limit: 1, Timeout: 30 * time.Second, ReadOnly: true, Describe: true, Spatial: true},
		CallerAccessToken: callerToken,
	})
	if err != nil {
		return nil, err
	}
	table, spatial, err := duckDBDescribeContract(result)
	if err != nil {
		return nil, err
	}
	return &serviceModels.QueryServiceOutputContract{Table: table, Spatial: spatial}, nil
}

func federatedDescribeObjectTables(
	query string,
	provider plugin.FederatedQueryRuntimeProvider,
	candidates []plugin.FederatedQuerySource,
	sources []federatedquery.Source,
) map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, reference := range provider.ResolveObjectTableReferences(query, candidates) {
		for _, source := range sources {
			if reference.SourceName != source.EngineName && reference.SourceName != sanitizeFederatedIdentifier(source.EngineName) {
				continue
			}
			for _, table := range source.Tables {
				qualified := table.Table
				if table.Schema != "" {
					qualified = table.Schema + "." + table.Table
				}
				if reference.TableName != qualified && reference.TableName != table.Table {
					continue
				}
				if result[reference.SourceName] == nil {
					result[reference.SourceName] = make(map[string]string)
				}
				result[reference.SourceName][reference.TableName] = table.PhysicalPath
			}
		}
	}
	return result
}

func duckDBDescribeContract(result *plugin.QueryResult) (*datatype.TableInfo, *datatype.SpatialInfo, error) {
	if result == nil || len(result.Rows) == 0 {
		return nil, nil, errors.New("DuckDB 未返回 SQL 输出结构")
	}
	table := &datatype.TableInfo{Kind: "query", Fields: make([]datatype.FieldInfo, 0, len(result.Rows))}
	var geometryColumns []datatype.GeometryColumnInfo
	for index, row := range result.Rows {
		name := strings.TrimSpace(fmt.Sprint(row["column_name"]))
		nativeType := strings.ToLower(strings.TrimSpace(fmt.Sprint(row["column_type"])))
		if name == "" || nativeType == "" {
			return nil, nil, errors.New("DuckDB SQL 输出结构缺少字段名或类型")
		}
		fieldType := duckDBCommonFieldType(nativeType)
		nullable := strings.EqualFold(strings.TrimSpace(fmt.Sprint(row["null"])), "YES")
		table.Fields = append(table.Fields, datatype.FieldInfo{
			Name: name, Type: fieldType, NativeType: nativeType, Nullable: nullable, OrdinalPosition: index + 1,
		})
		if datatype.IsSpatialFieldType(fieldType) {
			geometryColumns = append(geometryColumns, datatype.GeometryColumnInfo{Name: name, GeometryType: "Geometry"})
		}
	}
	if len(geometryColumns) == 0 {
		return table, nil, nil
	}
	return table, &datatype.SpatialInfo{
		GeometryColumns: geometryColumns, PrimaryGeometryColumn: geometryColumns[0].Name,
	}, nil
}

func duckDBCommonFieldType(nativeType string) datatype.FieldType {
	typeName := strings.ToUpper(strings.TrimSpace(nativeType))
	switch {
	case strings.Contains(typeName, "GEOMETRY"):
		return datatype.FieldTypeGeometry
	case strings.HasSuffix(typeName, "[]") || strings.HasPrefix(typeName, "LIST"):
		return datatype.FieldTypeArray
	case strings.HasPrefix(typeName, "STRUCT") || strings.HasPrefix(typeName, "MAP") || typeName == "JSON":
		return datatype.FieldTypeJSON
	case typeName == "BOOLEAN" || typeName == "BOOL":
		return datatype.FieldTypeBool
	case typeName == "TINYINT" || typeName == "SMALLINT" || typeName == "INTEGER" || typeName == "INT":
		return datatype.FieldTypeInt
	case typeName == "BIGINT" || typeName == "HUGEINT" || typeName == "UBIGINT":
		return datatype.FieldTypeBigInt
	case typeName == "REAL" || typeName == "FLOAT":
		return datatype.FieldTypeFloat
	case typeName == "DOUBLE":
		return datatype.FieldTypeDouble
	case strings.HasPrefix(typeName, "DECIMAL") || strings.HasPrefix(typeName, "NUMERIC"):
		return datatype.FieldTypeDecimal
	case typeName == "DATE":
		return datatype.FieldTypeDate
	case typeName == "TIME" || strings.HasPrefix(typeName, "TIME "):
		return datatype.FieldTypeTime
	case strings.HasPrefix(typeName, "TIMESTAMP"):
		return datatype.FieldTypeTimestamp
	case typeName == "UUID":
		return datatype.FieldTypeUUID
	case typeName == "BLOB" || typeName == "BYTEA":
		return datatype.FieldTypeBytes
	case strings.Contains(typeName, "CHAR") || typeName == "VARCHAR" || typeName == "STRING":
		return datatype.FieldTypeString
	default:
		return datatype.FieldTypeUnknown
	}
}

func NewQuerySampleService(
	system *client.SystemServiceClient,
	issuer *client.SystemExecutionAuthorizationClient,
	meta *client.MetaClient,
) *QuerySampleService {
	return &QuerySampleService{
		system:  system,
		issuer:  issuer,
		catalog: federatedquery.NewCatalog(system, meta),
	}
}

func (s *QuerySampleService) Generate(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	engineID uint,
) (string, string, error) {
	if s == nil || s.system == nil || s.issuer == nil || s.catalog == nil || tenantID == 0 || engineID == 0 ||
		!strings.HasPrefix(userAccessToken, "addp_at_") {
		return "", "", fmt.Errorf("查询样例服务未正确初始化")
	}
	descriptor, err := s.system.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, engineID)
	if err != nil {
		return "", "", fmt.Errorf("获取查询引擎描述失败: %w", err)
	}
	if descriptor.LifecycleState != commonModels.EngineLifecycleActive {
		return "", "", fmt.Errorf("%w: 查询引擎未启用", ErrQuerySampleUnavailable)
	}
	enginePlugin, err := plugin.Get(descriptor.EngineType)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrQuerySampleUnavailable, err)
	}
	if provider, ok := enginePlugin.(plugin.FederatedQueryRuntimeProvider); ok {
		return s.generateFederated(ctx, tenantID, userAccessToken, descriptor, provider)
	}
	provider, ok := enginePlugin.(plugin.SQLQueryRuntimeProvider)
	if !ok || !containsQueryLanguage(provider.QueryLanguages(), "sql") {
		return "", "", fmt.Errorf("%w: 当前查询服务仅支持 SQL 引擎", ErrQuerySampleUnavailable)
	}
	return s.generateDirect(ctx, tenantID, userAccessToken, engineID)
}

func (s *QuerySampleService) generateDirect(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	engineID uint,
) (string, string, error) {
	executionID := uuid.New()
	issued, err := s.issuer.Issue(ctx, userAccessToken, client.IssueExecutionAuthorizationRequest{
		Audience:    "service",
		ExecutionID: executionID.String(),
		EngineIDs:   []string{strconv.FormatUint(uint64(engineID), 10)},
		Effects:     []string{"read"},
		ExpiresIn:   40,
	})
	if err != nil {
		return "", "", fmt.Errorf("签发查询样例执行授权失败: %w", err)
	}
	access, err := s.system.WithTenantID(tenantID).GetExecutionEngineAccess(ctx, issued.ID, client.ExecutionEngineAccessRequest{
		ExecutionID:     executionID.String(),
		EngineID:        strconv.FormatUint(uint64(engineID), 10),
		RequiredEffects: []string{"read"},
	})
	if err != nil {
		return "", "", fmt.Errorf("消费查询样例执行授权失败: %w", err)
	}
	return dbbridge.GenerateExecutableSampleQuery(ctx, access.Engine, "sql", dbbridge.ExecutableSampleQueryOptions{
		ValidationLimit: 10,
	})
}

func (s *QuerySampleService) generateFederated(
	ctx context.Context,
	tenantID uint,
	userAccessToken string,
	descriptor *commonModels.EngineRuntimeDescriptor,
	provider plugin.FederatedQueryRuntimeProvider,
) (string, string, error) {
	if descriptor == nil || descriptor.RuntimeEndpoint == nil {
		return "", "", fmt.Errorf("%w: 联邦查询 Runtime 不可用", ErrQuerySampleUnavailable)
	}
	sources, err := s.catalog.Sources(ctx, tenantID, descriptor.ID, provider)
	if err != nil {
		return "", "", err
	}
	candidates := federatedquery.Candidates(sources, 0)
	callerToken, err := s.system.TenantServiceAccessToken(ctx, tenantID)
	if err != nil {
		return "", "", fmt.Errorf("获取 Service Runtime 凭据失败: %w", err)
	}
	var firstExecutionFailure error
	for _, candidate := range candidates {
		executionID := uuid.New()
		issued, issueErr := s.issuer.Issue(ctx, userAccessToken, client.IssueExecutionAuthorizationRequest{
			Audience:    "duckdb",
			ExecutionID: executionID.String(),
			EngineIDs:   []string{strconv.FormatUint(uint64(candidate.EngineID), 10)},
			Effects:     []string{"read"},
			ExpiresIn:   60,
		})
		if issueErr != nil {
			return "", "", fmt.Errorf("签发联邦查询样例执行授权失败: %w", issueErr)
		}
		result, executeErr := provider.ExecuteFederatedQuery(ctx, plugin.ConnectionInfo(descriptor.AsEngine().ConnectionInfo), plugin.FederatedQueryRequest{
			ExecutionID:              executionID.String(),
			ExecutionAuthorizationID: issued.ID,
			SourceEngineIDs:          []uint{candidate.EngineID},
			Query:                    candidate.Query,
			Language:                 "sql",
			Options: plugin.QueryOptions{
				Limit:    10,
				Timeout:  30 * time.Second,
				ReadOnly: true,
				Spatial:  true,
			},
			CallerAccessToken: callerToken,
		})
		if executeErr == nil && result != nil && len(result.Rows) > 0 {
			return candidate.Query, "sql", nil
		}
		if firstExecutionFailure == nil {
			if executeErr != nil {
				firstExecutionFailure = executeErr
			} else {
				firstExecutionFailure = errors.New("样例查询没有返回数据")
			}
		}
	}
	if firstExecutionFailure != nil {
		return "", "", fmt.Errorf("%w: %v", ErrQuerySampleUnavailable, firstExecutionFailure)
	}
	return "", "", fmt.Errorf("%w: 当前业务 Catalog 没有可执行候选", ErrQuerySampleUnavailable)
}

func containsQueryLanguage(languages []string, required string) bool {
	for _, language := range languages {
		if strings.EqualFold(strings.TrimSpace(language), required) {
			return true
		}
	}
	return false
}
