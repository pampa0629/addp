package dbbridge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	commonquery "github.com/addp/common/query"
)

var (
	ErrSampleQueryUnavailable   = errors.New("当前引擎没有可生成样例查询的真实数据")
	ErrSampleQueryResourceEmpty = errors.New("所选资源没有可生成查询模板的真实数据")
)

// ExecutableSampleQueryOptions separates the query returned to the caller from
// the bounded query used to validate it. QueryLimit only applies to generated
// SQL; ValidationLimit never changes the returned query.
type ExecutableSampleQueryOptions struct {
	QueryLimit      int
	ValidationLimit int
	Path            *plugin.EngineCatalogPath
}

// GenerateSampleQuery 从当前引擎的实时 Catalog 生成一个可直接执行的样例查询。
func GenerateSampleQuery(ctx context.Context, engine *models.Engine, queryLimit int) (query string, language string, err error) {
	return generateSampleQueryWithPath(ctx, engine, queryLimit, nil)
}

func generateSampleQueryWithPath(ctx context.Context, engine *models.Engine, queryLimit int, selectedPath *plugin.EngineCatalogPath) (query string, language string, err error) {
	if engine == nil {
		return "", "", fmt.Errorf("%w: 引擎不能为空", ErrSampleQueryUnavailable)
	}
	engineType := strings.ToLower(engine.EngineType)

	sampleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	p, err := plugin.Get(engineType)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrSampleQueryUnavailable, err)
	}
	connInfo := plugin.ConnectionInfo(engine.ConnectionInfo)
	if selectedPath != nil {
		qp, ok := p.(plugin.QueryRuntimeProvider)
		if !ok {
			return "", "", fmt.Errorf("%w: 引擎未提供查询运行时", ErrSampleQueryUnavailable)
		}
		q, language := qp.GenerateSampleQuery(sampleCtx, connInfo, plugin.SampleQueryOptions{Path: *selectedPath})
		if strings.TrimSpace(q) == "" || strings.TrimSpace(language) == "" {
			return "", "", ErrSampleQueryUnavailable
		}
		return q, language, nil
	}

	if _, ok := p.(plugin.SQLQueryRuntimeProvider); ok {
		cp, ok := p.(plugin.EngineCatalogProvider)
		if !ok {
			return "", "", fmt.Errorf("%w: 引擎未提供实时 Catalog", ErrSampleQueryUnavailable)
		}
		q, catalogErr := generateCatalogSampleQuery(sampleCtx, p, cp, connInfo, engine.ID, engineType, queryLimit)
		if catalogErr != nil {
			return "", "", catalogErr
		}
		return q, "sql", nil
	}

	qp, ok := p.(plugin.QueryRuntimeProvider)
	if !ok {
		return "", "", fmt.Errorf("%w: 引擎未提供查询运行时", ErrSampleQueryUnavailable)
	}
	q, language := qp.GenerateSampleQuery(sampleCtx, connInfo, plugin.SampleQueryOptions{})
	if strings.TrimSpace(q) == "" || strings.TrimSpace(language) == "" {
		return "", "", ErrSampleQueryUnavailable
	}
	return q, language, nil
}

// GenerateExecutableSampleQuery returns a real catalog-based sample only after
// the same read-only runtime path has produced at least one row.
func GenerateExecutableSampleQuery(
	ctx context.Context,
	engine *models.Engine,
	requiredLanguage string,
	options ExecutableSampleQueryOptions,
) (string, string, error) {
	query, language, err := generateSampleQueryWithPath(ctx, engine, options.QueryLimit, options.Path)
	if err != nil {
		return "", "", err
	}
	if requiredLanguage != "" && !strings.EqualFold(language, requiredLanguage) {
		return "", "", fmt.Errorf("%w: 查询语言 %s 不受当前入口支持", ErrSampleQueryUnavailable, language)
	}
	validationQuery := query
	if options.ValidationLimit > 0 && strings.EqualFold(language, "sql") {
		validationQuery = commonquery.ForEngine(engine.EngineType).PaginateQuerySQL(query, options.ValidationLimit, 0)
	}
	result, err := ExecuteReadOnlyRuntimeQueryWithPath(ctx, engine, language, validationQuery, nil, 0, options.Path)
	if err != nil {
		return "", "", fmt.Errorf("%w: 样例查询执行失败: %v", ErrSampleQueryUnavailable, err)
	}
	if result == nil || len(result.Rows) == 0 {
		if options.Path != nil {
			return "", "", fmt.Errorf("%w: 样例查询没有返回数据", ErrSampleQueryResourceEmpty)
		}
		return "", "", fmt.Errorf("%w: 样例查询没有返回数据", ErrSampleQueryUnavailable)
	}
	return query, language, nil
}

func generateCatalogSampleQuery(ctx context.Context, enginePlugin plugin.EnginePlugin, cp plugin.EngineCatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, engineType string, queryLimit int) (string, error) {
	modelProvider, ok := enginePlugin.(plugin.EngineCatalogModelProvider)
	if !ok {
		return "", fmt.Errorf("%w: 引擎未声明 Catalog 模型", ErrSampleQueryUnavailable)
	}
	model := modelProvider.EngineCatalogModel()
	if plugin.EngineCatalogLeafTerm(model) != plugin.EngineCatalogTermTable {
		return "", fmt.Errorf("%w: Catalog leaf 不是表", ErrSampleQueryUnavailable)
	}

	namespaces, err := cp.ListChildren(ctx, connInfo, plugin.EngineCatalogRootPath(model, engineID), plugin.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("%w: 列出 Catalog namespace 失败: %v", ErrSampleQueryUnavailable, err)
	}

	resource := &plugin.Engine{ID: engineID, EngineType: engineType, ConnectionInfo: connInfo}
	foundTable := false
	for _, namespace := range namespaces {
		if namespace.Role != plugin.EngineCatalogRoleBranch {
			continue
		}

		items, err := cp.ListChildren(ctx, connInfo, namespace.Path, plugin.ListOptions{})
		if err != nil {
			return "", fmt.Errorf("%w: 列出 Catalog leaf 失败: %v", ErrSampleQueryUnavailable, err)
		}

		for _, item := range items {
			if item.Role != plugin.EngineCatalogRoleLeaf {
				continue
			}
			foundTable = true
			if catalogEntryRowCount(item) > 0 {
				return tableSampleSQL(engineType, namespace.Name, item.Name, queryLimit), nil
			}
			count, countErr := plugin.CountEngineCatalogItemRows(ctx, resource, item.Path)
			if countErr == nil && count > 0 {
				return tableSampleSQL(engineType, namespace.Name, item.Name, queryLimit), nil
			}
		}
	}

	if foundTable {
		return "", fmt.Errorf("%w: Catalog 中没有有数据的表", ErrSampleQueryUnavailable)
	}
	return "", fmt.Errorf("%w: Catalog 中没有表", ErrSampleQueryUnavailable)
}

func tableSampleSQL(engineType, namespace, table string, limit int) string {
	return commonquery.SelectAllSampleSQL(engineType, namespace, table, limit)
}

func catalogEntryRowCount(entry plugin.EngineCatalogEntry) int64 {
	if entry.Table == nil || entry.Table.RowCount == nil {
		return 0
	}
	return *entry.Table.RowCount
}

// ExecuteQuery executes an ordinary query through the Provider-owned
// PrepareQuery -> Execute route for every query language and engine.
func ExecuteQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.QueryResult, error) {
	if engine == nil {
		return nil, fmt.Errorf("引擎不能为空")
	}
	engineType := strings.ToLower(engine.EngineType)
	p, err := plugin.Get(engineType)
	if err != nil {
		return nil, err
	}
	provider, ok := p.(plugin.QueryRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("引擎不支持普通查询运行时")
	}
	prepared, err := provider.PrepareQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.QueryRequest{
		EngineID: engine.ID,
		Language: firstQueryLanguage(provider.QueryLanguages()),
		Query:    query,
		Options: plugin.QueryOptions{
			EngineID: engine.ID, EngineType: engine.EngineType,
		},
	})
	if err != nil {
		return nil, err
	}
	return prepared.Execute(ctx)
}

// ExecuteGraphQuery 统一图查询执行入口
// 对支持 GraphQueryProvider 的引擎（Neo4j 等）同时返回表格数据和图结构数据（节点/关系）
// 对其他引擎回退到 ExecuteQuery 并包装结果（GraphData 为 nil）
func ExecuteGraphQuery(ctx context.Context, engine *models.Engine, query string) (*plugin.GraphQueryResult, error) {
	engineType := strings.ToLower(engine.EngineType)

	p, err := plugin.Get(engineType)
	if err == nil {
		if gqp, ok := p.(plugin.GraphQueryProvider); ok {
			return gqp.ExecuteGraphQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), query, plugin.QueryOptions{
				EngineID:   engine.ID,
				EngineType: engine.EngineType,
			})
		}
	}

	// 回退：普通查询，无图数据
	qr, err := ExecuteQuery(ctx, engine, query)
	if err != nil {
		return nil, err
	}
	return &plugin.GraphQueryResult{QueryResult: *qr}, nil
}

// SupportsReadOnlySQLExecution reports whether dbbridge can establish a real
// database read-only transaction for the engine. Unsupported engines must be
// rejected instead of falling back to an ordinary privileged connection.
func SupportsReadOnlySQLExecution(engineType string) bool {
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "postgresql", "oracle", "mysql", "doris", "spark":
		return true
	default:
		return false
	}
}

func readOnlyTxOptions(engineType string) *sql.TxOptions {
	if strings.EqualFold(strings.TrimSpace(engineType), "oracle") {
		// The Oracle driver rejects ReadOnly=true in database/sql BeginTx.
		return nil
	}
	return &sql.TxOptions{ReadOnly: true}
}

func requiresSQLReadOnlyStatement(engineType string) bool {
	return strings.EqualFold(strings.TrimSpace(engineType), "oracle")
}

func beginReadOnlyTransaction(ctx context.Context, db *sql.DB, engineType string) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, readOnlyTxOptions(engineType))
	if err != nil {
		return nil, err
	}
	if requiresSQLReadOnlyStatement(engineType) {
		if _, err := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	return tx, nil
}

// ExecuteReadOnlyQuery executes one SQL query in a database-enforced read-only
// transaction. It is the only dbbridge path for User executions classified as
// read.
func ExecuteReadOnlyQuery(ctx context.Context, engine *models.Engine, query string, parameters map[string]interface{}, limit int) (*plugin.QueryResult, error) {
	if engine == nil || !SupportsReadOnlySQLExecution(engine.EngineType) {
		return nil, fmt.Errorf("引擎不支持受控只读 SQL 执行")
	}
	if strings.EqualFold(engine.EngineType, "spark") {
		if parameters != nil {
			return nil, fmt.Errorf("Spark SQL 查询运行时不支持命名参数")
		}
	}
	if strings.EqualFold(engine.EngineType, "spark") || strings.EqualFold(engine.EngineType, "doris") {
		p, err := plugin.Get(engine.EngineType)
		if err != nil {
			return nil, err
		}
		sqlRuntime, ok := p.(plugin.SQLQueryRuntimeProvider)
		if !ok {
			return nil, fmt.Errorf("%s 引擎未提供 SQL 查询运行时", engine.EngineType)
		}
		return sqlRuntime.ExecuteSQL(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), query, plugin.QueryOptions{
			EngineID: engine.ID, EngineType: engine.EngineType, Limit: limit, ReadOnly: true, Parameters: parameters,
		})
	}
	boundQuery, args, err := bindSQLExecutionParameters(engine.EngineType, query, parameters)
	if err != nil {
		return nil, err
	}
	query = boundQuery
	db, err := GetOrCreatePool(engine, DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("获取连接池失败：%w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败：%w", err)
	}
	tx, err := beginReadOnlyTransaction(ctx, sqlDB, engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("开启只读事务失败：%w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if strings.EqualFold(strings.TrimSpace(engine.EngineType), "oracle") {
		query, err = rewriteOracleSpatialSelect(ctx, tx, query)
		if err != nil {
			return nil, fmt.Errorf("准备 Oracle Spatial 查询失败：%w", err)
		}
	}
	if limit > 0 {
		query = commonquery.ForEngine(engine.EngineType).PaginateQuerySQL(query, limit, 0)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	result, err := scanSQLRows(rows)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交只读事务失败：%w", err)
	}
	committed = true
	return result, nil
}

var oracleSelectAllPattern = regexp.MustCompile(`(?is)^\s*SELECT\s+\*\s+FROM\s+((?:"[^"]+"|[A-Za-z0-9_$#]+)\s*\.\s*(?:"[^"]+"|[A-Za-z0-9_$#]+))(.*)$`)

// rewriteOracleSpatialSelect expands the one unambiguous table-sample form used
// by the query workbench. Oracle's UDT decoder can block on a bare SDO_GEOMETRY;
// projecting that column to GeoJSON keeps the result tabular and inspectable.
func rewriteOracleSpatialSelect(ctx context.Context, tx *sql.Tx, query string) (string, error) {
	matches := oracleSelectAllPattern.FindStringSubmatch(query)
	if len(matches) != 3 {
		return query, nil
	}
	suffix := matches[2]
	trimmedSuffix := strings.TrimSpace(suffix)
	if trimmedSuffix != "" && !strings.HasPrefix(strings.ToUpper(trimmedSuffix), "FETCH") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "WHERE") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "ORDER") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "GROUP") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "OFFSET") &&
		!strings.HasPrefix(strings.ToUpper(trimmedSuffix), "FOR") {
		return query, nil
	}
	parts := strings.Split(matches[1], ".")
	if len(parts) != 2 {
		return query, nil
	}
	owner := strings.Trim(strings.TrimSpace(parts[0]), `"`)
	table := strings.Trim(strings.TrimSpace(parts[1]), `"`)
	rows, err := tx.QueryContext(ctx, `
		SELECT column_name, data_type
		  FROM all_tab_columns
		 WHERE owner = :1 AND table_name = :2
		 ORDER BY column_id`, owner, table)
	if err != nil {
		return query, err
	}
	defer rows.Close()
	type column struct{ name, dataType string }
	columns := make([]column, 0)
	hasSpatial := false
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.dataType); err != nil {
			return query, err
		}
		columns = append(columns, c)
		if strings.EqualFold(strings.TrimSpace(c.dataType), "SDO_GEOMETRY") {
			hasSpatial = true
		}
	}
	if err := rows.Err(); err != nil {
		return query, err
	}
	if !hasSpatial || len(columns) == 0 {
		return query, nil
	}
	projection := make([]string, 0, len(columns))
	for _, c := range columns {
		quoted := `"` + strings.ReplaceAll(c.name, `"`, `""`) + `"`
		if strings.EqualFold(strings.TrimSpace(c.dataType), "SDO_GEOMETRY") {
			projection = append(projection, `SDO_UTIL.TO_GEOJSON(`+quoted+`) AS `+quoted)
		} else {
			projection = append(projection, quoted)
		}
	}
	return "SELECT " + strings.Join(projection, ", ") + " FROM " + matches[1] + suffix, nil
}

// ExecuteReadOnlyRuntimeQuery executes a non-SQL query through the engine's
// native QueryRuntimeProvider. The provider must enforce QueryOptions.ReadOnly.
func ExecuteReadOnlyRuntimeQuery(ctx context.Context, engine *models.Engine, language, query string, parameters map[string]interface{}, limit int) (*plugin.QueryResult, error) {
	return ExecuteReadOnlyRuntimeQueryWithPath(ctx, engine, language, query, parameters, limit, nil)
}

// ExecuteReadOnlyRuntimeQueryWithPath executes a native read-only query against
// the concrete catalog path selected by the caller.
func ExecuteReadOnlyRuntimeQueryWithPath(ctx context.Context, engine *models.Engine, language, query string, parameters map[string]interface{}, limit int, targetPath *plugin.EngineCatalogPath) (*plugin.QueryResult, error) {
	if engine == nil {
		return nil, fmt.Errorf("引擎不能为空")
	}
	p, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	qp, ok := p.(plugin.QueryRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("引擎不支持普通查询运行时")
	}
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" || !slices.Contains(qp.QueryLanguages(), language) {
		return nil, fmt.Errorf("引擎不支持查询语言: %s", language)
	}
	prepared, err := qp.PrepareQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.QueryRequest{
		EngineID:   engine.ID,
		Language:   language,
		Query:      query,
		TargetPath: targetPath,
		Options: plugin.QueryOptions{
			EngineID: engine.ID, EngineType: engine.EngineType, Limit: limit, ReadOnly: true, Parameters: parameters,
		},
	})
	if err != nil {
		return nil, err
	}
	return prepared.Execute(ctx)
}

// ExecuteReadOnlyGraphQuery executes a native graph query through the same
// read-only and bounded result contract as other ad-hoc queries.
func ExecuteReadOnlyGraphQuery(ctx context.Context, engine *models.Engine, language, query string, parameters map[string]interface{}, limit int) (*plugin.GraphQueryResult, error) {
	return ExecuteReadOnlyGraphQueryWithPath(ctx, engine, language, query, parameters, limit, nil)
}

func ExecuteReadOnlyGraphQueryWithPath(ctx context.Context, engine *models.Engine, language, query string, parameters map[string]interface{}, limit int, targetPath *plugin.EngineCatalogPath) (*plugin.GraphQueryResult, error) {
	if engine == nil {
		return nil, fmt.Errorf("引擎不能为空")
	}
	p, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	queryProvider, ok := p.(plugin.QueryRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("引擎不支持普通查询运行时")
	}
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" || !slices.Contains(queryProvider.QueryLanguages(), language) {
		return nil, fmt.Errorf("引擎不支持查询语言: %s", language)
	}
	graphProvider, ok := p.(plugin.GraphQueryProvider)
	if !ok {
		return nil, fmt.Errorf("引擎未提供图查询运行时")
	}
	if targetPathProvider, ok := p.(plugin.PathAwareGraphQueryProvider); ok && targetPath != nil {
		return targetPathProvider.ExecuteGraphQueryAtPath(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), *targetPath, query, plugin.QueryOptions{
			EngineID: engine.ID, EngineType: engine.EngineType, Limit: limit, ReadOnly: true, Parameters: parameters,
		})
	}
	return graphProvider.ExecuteGraphQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), query, plugin.QueryOptions{
		EngineID: engine.ID, EngineType: engine.EngineType, Limit: limit, ReadOnly: true, Parameters: parameters,
	})
}

// ExecuteStatement executes one non-read SQL statement and returns affected
// rows. The caller must classify and authorize the statement effect first.
func ExecuteStatement(ctx context.Context, engine *models.Engine, query string, parameters map[string]interface{}) (int64, error) {
	query, args, err := bindSQLExecutionParameters(engine.EngineType, query, parameters)
	if err != nil {
		return 0, err
	}
	db, err := GetOrCreatePool(engine, DefaultPoolConfig())
	if err != nil {
		return 0, fmt.Errorf("获取连接池失败：%w", err)
	}
	result := db.WithContext(ctx).Exec(query, args...)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func bindSQLExecutionParameters(engineType, query string, parameters map[string]interface{}) (string, []interface{}, error) {
	if parameters == nil {
		return query, nil, nil
	}
	bound, args, err := commonquery.BindSQL(query, parameters, commonquery.SQLPlaceholderStyleForEngine(engineType))
	if err != nil {
		return "", nil, fmt.Errorf("绑定 SQL 查询参数失败: %w", err)
	}
	return bound, args, nil
}

func scanSQLRows(rows *sql.Rows) (*plugin.QueryResult, error) {
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

	return &plugin.QueryResult{Columns: columns, Rows: resultRows}, nil
}

func firstQueryLanguage(languages []string) string {
	if len(languages) == 0 {
		return ""
	}
	return languages[0]
}
