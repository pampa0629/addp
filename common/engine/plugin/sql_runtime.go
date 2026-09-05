package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	commonquery "github.com/addp/common/query"
	"gorm.io/gorm"
)

// ExecuteSQLWithConnectionPool executes SQL through a plugin-provided GORM pool.
// It is intended for SQLQueryRuntimeProvider implementations that do not need
// engine-id based pool reuse.
func ExecuteSQLWithConnectionPool(ctx context.Context, poolPlugin interface {
	ConnectionPoolPlugin
	SQLQueryRuntimeProvider
}, connInfo ConnectionInfo, sql string, opts QueryOptions) (*QueryResult, error) {
	if poolPlugin == nil {
		return nil, fmt.Errorf("connection pool plugin cannot be nil")
	}
	boundSQL, boundArgs, err := BindSQLRuntimeParameters(poolPlugin.SQLDialect(), sql, opts)
	if err != nil {
		return nil, err
	}
	if opts.Parameters != nil {
		sql = boundSQL
		opts.Args = boundArgs
	}
	if opts.ReadOnly {
		if err := commonquery.RequireReadOnly(sql); err != nil {
			return nil, fmt.Errorf("read-only SQL validation failed: %w", err)
		}
	}
	if opts.Limit > 0 {
		sql = commonquery.ForDialect(poolPlugin.SQLDialect()).PaginateQuerySQL(sql, opts.Limit, 0)
	}

	var db *gorm.DB
	var closeAfter bool
	if opts.EngineID != 0 && opts.EngineType != "" {
		pooled, err := GetOrCreatePoolFromFactory(&Engine{
			ID:             opts.EngineID,
			EngineType:     opts.EngineType,
			ConnectionInfo: connInfo,
		}, DefaultPoolConfig())
		if err != nil {
			return nil, fmt.Errorf("获取连接池失败：%w", err)
		}
		db = pooled
	} else {
		created, err := poolPlugin.CreateConnectionPool(connInfo, DefaultPoolConfig())
		if err != nil {
			return nil, fmt.Errorf("获取连接池失败：%w", err)
		}
		db = created
		closeAfter = true
	}

	if closeAfter {
		sqlDB, err := db.DB()
		if err == nil {
			defer sqlDB.Close()
		}
	}

	if opts.ReadOnly {
		return executeReadOnlySQL(ctx, db, poolPlugin.SQLDialect(), sql, opts.Args)
	}
	rows, err := db.WithContext(ctx).Raw(sql, opts.Args...).Rows()
	if err != nil {
		return nil, err
	}
	return scanRuntimeSQLRows(rows)
}

func executeReadOnlySQL(ctx context.Context, db *gorm.DB, dialect, query string, args []interface{}) (*QueryResult, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败：%w", err)
	}
	dialectName := strings.ToLower(strings.TrimSpace(dialect))
	var txOptions *sql.TxOptions
	switch dialectName {
	case commonquery.DialectPostgreSQL, commonquery.DialectMySQL:
		txOptions = &sql.TxOptions{ReadOnly: true}
	case commonquery.DialectOracle:
		// go-ora rejects database/sql's ReadOnly option; Oracle exposes the
		// same database-enforced boundary through SET TRANSACTION READ ONLY.
		txOptions = nil
	default:
		return nil, fmt.Errorf("引擎 %s 不支持受控只读 SQL 执行", dialect)
	}
	tx, err := sqlDB.BeginTx(ctx, txOptions)
	if err != nil {
		return nil, fmt.Errorf("开启只读事务失败：%w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if dialectName == commonquery.DialectOracle {
		if _, err := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
			return nil, fmt.Errorf("设置只读事务失败：%w", err)
		}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	result, err := scanRuntimeSQLRows(rows)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交只读事务失败：%w", err)
	}
	committed = true
	return result, nil
}

func scanRuntimeSQLRows(rows *sql.Rows) (*QueryResult, error) {
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("获取列名失败：%w", err)
	}

	var resultRows []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("扫描行失败：%w", err)
		}
		row := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历结果失败：%w", err)
	}

	return &QueryResult{Columns: columns, Rows: resultRows}, nil
}

// BindSQLRuntimeParameters applies the same parameter contract used by SQL
// execution. Dialect providers may use it before parsing the execution-bound
// statement for query planning facts such as QueryReadSet.
func BindSQLRuntimeParameters(dialect, sql string, opts QueryOptions) (string, []interface{}, error) {
	if opts.Parameters != nil && len(opts.Args) > 0 {
		return "", nil, fmt.Errorf("query options cannot contain both named parameters and positional args")
	}
	if opts.Parameters == nil {
		return sql, opts.Args, nil
	}
	boundSQL, boundArgs, err := commonquery.BindSQL(sql, opts.Parameters, commonquery.SQLPlaceholderStyleForDialect(dialect))
	if err != nil {
		return "", nil, fmt.Errorf("bind SQL query parameters: %w", err)
	}
	return boundSQL, boundArgs, nil
}

// PrepareSQLRuntimeQuery binds parameters once and returns the only execution
// route for an ordinary SQL query. A nil resolver means the dialect has not yet
// implemented a complete QueryReadSet; execution remains available, while
// ReadSet fails closed with ErrQueryReadSetUnresolved.
func PrepareSQLRuntimeQuery(
	provider SQLQueryRuntimeProvider,
	connInfo ConnectionInfo,
	req QueryRequest,
	resolveReadSet func(context.Context, ConnectionInfo, QueryRequest) (*QueryReadSet, error),
	resolveLineage func(context.Context, ConnectionInfo, QueryRequest, *QueryReadSet) (*QueryOutputLineage, error),
) (PreparedQuery, error) {
	if provider == nil {
		return nil, fmt.Errorf("SQL query runtime provider cannot be nil")
	}
	preparedReq, err := prepareQueryRequest(provider.Type(), req)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(preparedReq.Language, "sql") {
		return nil, fmt.Errorf("SQL query runtime requires language sql")
	}
	boundSQL, boundArgs, err := BindSQLRuntimeParameters(provider.SQLDialect(), preparedReq.Query, preparedReq.Options)
	if err != nil {
		return nil, err
	}
	preparedReq.Query = boundSQL
	preparedReq.Options.Parameters = nil
	preparedReq.Options.Args = cloneQueryValues(boundArgs)
	if preparedReq.Options.ReadOnly {
		if err := commonquery.RequireReadOnly(preparedReq.Query); err != nil {
			return nil, fmt.Errorf("read-only SQL validation failed: %w", err)
		}
	}
	analysis, err := NewQueryAnalysis(preparedReq.Language, QuerySchemaCoverageUnknown)
	if err != nil {
		return nil, err
	}
	preparedConnInfo := cloneQueryConnectionInfo(connInfo)
	var readSet func(context.Context) (*QueryReadSet, error)
	if resolveReadSet != nil {
		readSet = func(ctx context.Context) (*QueryReadSet, error) {
			return resolveReadSet(ctx, cloneQueryConnectionInfo(preparedConnInfo), cloneQueryRequest(preparedReq))
		}
	}
	var lineage func(context.Context, *QueryReadSet) (*QueryOutputLineage, error)
	if resolveLineage != nil {
		lineage = func(ctx context.Context, readSet *QueryReadSet) (*QueryOutputLineage, error) {
			return resolveLineage(ctx, cloneQueryConnectionInfo(preparedConnInfo), cloneQueryRequest(preparedReq), readSet.Clone())
		}
	}
	prepared, err := NewPreparedQuery(analysis, readSet, lineage, func(ctx context.Context) (*QueryResult, error) {
		request := cloneQueryRequest(preparedReq)
		return provider.ExecuteSQL(ctx, cloneQueryConnectionInfo(preparedConnInfo), request.Query, request.Options)
	})
	if err != nil {
		return nil, err
	}
	return &preparedSQLQuery{
		preparedQuery: prepared.(*preparedQuery),
		providerType:  provider.Type(),
		connInfo:      preparedConnInfo,
		request:       preparedReq,
	}, nil
}

type preparedSQLQuery struct {
	*preparedQuery
	providerType string
	connInfo     ConnectionInfo
	request      QueryRequest
}

// ConsumeSQLPreparedQuery transfers the already-bound SQL request from the
// shared one-shot PreparedQuery to a streaming session owned by the same
// provider type. It is the only SQL query-session bridge; providers must not
// re-bind the original request or call Execute after consuming it.
func ConsumeSQLPreparedQuery(prepared PreparedQuery, provider SQLQueryRuntimeProvider) (ConnectionInfo, QueryRequest, error) {
	plan, ok := prepared.(*preparedSQLQuery)
	if !ok || provider == nil || plan.providerType != provider.Type() {
		return nil, QueryRequest{}, fmt.Errorf("SQL query read session requires a PreparedQuery from the same provider type")
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	if plan.consumed {
		return nil, QueryRequest{}, ErrPreparedQueryConsumed
	}
	plan.consumed = true
	return cloneQueryConnectionInfo(plan.connInfo), cloneQueryRequest(plan.request), nil
}

func prepareQueryRequest(engineType string, req QueryRequest) (QueryRequest, error) {
	prepared := cloneQueryRequest(req)
	prepared.Language = strings.ToLower(strings.TrimSpace(prepared.Language))
	prepared.Query = strings.TrimSpace(prepared.Query)
	if prepared.Language == "" || prepared.Query == "" {
		return QueryRequest{}, fmt.Errorf("query language and text are required")
	}
	if prepared.EngineID != 0 && prepared.Options.EngineID != 0 && prepared.EngineID != prepared.Options.EngineID {
		return QueryRequest{}, fmt.Errorf("query engine identity is inconsistent")
	}
	if prepared.EngineID == 0 {
		prepared.EngineID = prepared.Options.EngineID
	}
	prepared.Options.EngineID = prepared.EngineID
	if prepared.Options.EngineType == "" {
		prepared.Options.EngineType = strings.TrimSpace(engineType)
	}
	return prepared, nil
}

func cloneQueryRequest(req QueryRequest) QueryRequest {
	cloned := req
	if req.TargetPath != nil {
		path := cloneEngineCatalogPath(*req.TargetPath)
		cloned.TargetPath = &path
	}
	cloned.Options.Args = cloneQueryValues(req.Options.Args)
	cloned.Options.Parameters = cloneQueryValueMap(req.Options.Parameters)
	return cloned
}

func cloneQueryConnectionInfo(connInfo ConnectionInfo) ConnectionInfo {
	if connInfo == nil {
		return nil
	}
	cloned := make(ConnectionInfo, len(connInfo))
	for key, value := range connInfo {
		cloned[key] = cloneQueryValue(value)
	}
	return cloned
}

func cloneQueryValueMap(values map[string]interface{}) map[string]interface{} {
	if values == nil {
		return nil
	}
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = cloneQueryValue(value)
	}
	return cloned
}

func cloneQueryValues(values []interface{}) []interface{} {
	if values == nil {
		return nil
	}
	cloned := make([]interface{}, len(values))
	for index, value := range values {
		cloned[index] = cloneQueryValue(value)
	}
	return cloned
}

func cloneQueryValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case []byte:
		return append([]byte(nil), typed...)
	case []interface{}:
		return cloneQueryValues(typed)
	case map[string]interface{}:
		return cloneQueryValueMap(typed)
	default:
		return value
	}
}

// ReadSQLBatch adapts SQLQueryRuntimeProvider to BatchReadableProvider.
func ReadSQLBatch(ctx context.Context, provider SQLQueryRuntimeProvider, connInfo ConnectionInfo, path EngineCatalogPath, opts BatchReadOptions) (*BatchData, error) {
	if len(opts.Args) > 0 {
		parameterized, ok := provider.(ParameterizedSQLQueryRuntimeProvider)
		if !ok || !parameterized.SupportsParameterizedQueries() {
			return nil, fmt.Errorf("engine %s does not support parameterized SQL batch reads", provider.Type())
		}
	}
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		segments := EngineCatalogPathWithoutRoot(path).Segments
		if len(segments) < 2 {
			return nil, fmt.Errorf("SQL batch read requires item path or query")
		}
		namespace := segments[len(segments)-2].Name
		item := segments[len(segments)-1].Name
		query = sampleSQLForDialect(provider.SQLDialect(), namespace, item, opts.Limit, opts.Offset)
	} else if opts.Limit > 0 {
		query = commonquery.ForDialect(provider.SQLDialect()).PaginateQuerySQL(query, opts.Limit, int(opts.Offset))
	}
	result, err := provider.ExecuteSQL(ctx, connInfo, query, QueryOptions{Limit: opts.Limit, Args: opts.Args})
	if err != nil {
		return nil, err
	}
	return QueryResultToBatchData(result, opts.Offset), nil
}

func sampleSQLForDialect(dialect, namespace, item string, limit int, offset int64) string {
	if limit <= 0 {
		limit = 1000
	}
	return commonquery.ForDialect(dialect).SelectTableSQL("*", namespace, item, "", "", limit, int(offset))
}

// SampleSQLForDialectCatalogPath builds a bounded query for one real tabular Catalog leaf.
func SampleSQLForDialectCatalogPath(dialect string, path EngineCatalogPath, limit int) string {
	segments := EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) < 2 {
		return ""
	}
	return sampleSQLForDialect(dialect, segments[len(segments)-2].Name, segments[len(segments)-1].Name, limit, 0)
}

func QueryResultToBatchData(result *QueryResult, offset int64) *BatchData {
	if result == nil {
		return &BatchData{Offset: offset}
	}
	fields := make([]datatype.FieldInfo, 0, len(result.Columns))
	for _, column := range result.Columns {
		fields = append(fields, datatype.FieldInfo{
			Name: column, Type: inferQueryResultFieldType(result.Rows, column), Nullable: true,
		})
	}
	return &BatchData{
		Rows:   result.Rows,
		Fields: fields,
		Offset: offset,
	}
}

func inferQueryResultFieldType(rows []map[string]interface{}, column string) datatype.FieldType {
	fieldType := datatype.FieldTypeUnknown
	for _, row := range rows {
		value := row[column]
		if value == nil {
			continue
		}
		candidate := queryResultValueFieldType(value)
		if fieldType == datatype.FieldTypeUnknown {
			fieldType = candidate
			continue
		}
		if candidate != fieldType {
			if datatype.IsNumericFieldType(fieldType) && datatype.IsNumericFieldType(candidate) {
				fieldType = datatype.FieldTypeDouble
				continue
			}
			return datatype.FieldTypeMixed
		}
	}
	return fieldType
}

func queryResultValueFieldType(value interface{}) datatype.FieldType {
	switch value.(type) {
	case bool:
		return datatype.FieldTypeBool
	case int, int8, int16, int32:
		return datatype.FieldTypeInt
	case int64, uint, uint8, uint16, uint32, uint64:
		return datatype.FieldTypeBigInt
	case float32:
		return datatype.FieldTypeFloat
	case float64:
		return datatype.FieldTypeDouble
	case []byte:
		return datatype.FieldTypeBytes
	case time.Time:
		return datatype.FieldTypeTimestamp
	case map[string]interface{}:
		return datatype.FieldTypeJSON
	case []interface{}:
		return datatype.FieldTypeArray
	case string, fmt.Stringer:
		return datatype.FieldTypeString
	default:
		kind := reflect.TypeOf(value).Kind()
		if kind == reflect.Map || kind == reflect.Struct {
			return datatype.FieldTypeJSON
		}
		if kind == reflect.Array || kind == reflect.Slice {
			return datatype.FieldTypeArray
		}
		return datatype.FieldTypeUnknown
	}
}
