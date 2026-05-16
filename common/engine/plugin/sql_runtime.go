package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/sqldialect"
	"gorm.io/gorm"
)

// ExecuteSQLWithConnectionPool executes SQL through a plugin-provided GORM pool.
// It is intended for SQLQueryRuntimeProvider implementations that do not need
// engine-id based pool reuse.
func ExecuteSQLWithConnectionPool(ctx context.Context, poolPlugin ConnectionPoolPlugin, connInfo ConnectionInfo, sql string, opts QueryOptions) (*QueryResult, error) {
	if poolPlugin == nil {
		return nil, fmt.Errorf("connection pool plugin cannot be nil")
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

	rows, err := db.WithContext(ctx).Raw(sql).Rows()
	if err != nil {
		return nil, err
	}
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

// ReadSQLBatch adapts SQLQueryRuntimeProvider to BatchReadableProvider.
func ReadSQLBatch(ctx context.Context, provider SQLQueryRuntimeProvider, connInfo ConnectionInfo, path CatalogPath, opts BatchReadOptions) (*BatchData, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		if len(path.Segments) < 2 {
			return nil, fmt.Errorf("SQL batch read requires item path or query")
		}
		namespace := path.Segments[len(path.Segments)-2].Name
		item := path.Segments[len(path.Segments)-1].Name
		query = sampleSQLForEngine(provider.Type(), namespace, item, opts.Limit, opts.Offset)
	} else if opts.Limit > 0 {
		query = sqldialect.PaginateQuerySQL(query, opts.Limit, int(opts.Offset))
	}
	result, err := provider.ExecuteSQL(ctx, connInfo, query, QueryOptions{Limit: opts.Limit})
	if err != nil {
		return nil, err
	}
	return QueryResultToBatchData(result, opts.Offset), nil
}

func sampleSQLForEngine(engineType, namespace, item string, limit int, offset int64) string {
	if limit <= 0 {
		limit = 1000
	}
	return sqldialect.ForEngine(engineType).SelectTableSQL("*", namespace, item, "", "", limit, int(offset))
}

func QueryResultToBatchData(result *QueryResult, offset int64) *BatchData {
	if result == nil {
		return &BatchData{Offset: offset}
	}
	fields := make([]FieldInfo, 0, len(result.Columns))
	for _, column := range result.Columns {
		fields = append(fields, FieldInfo{Name: column})
	}
	return &BatchData{
		Rows:   result.Rows,
		Fields: fields,
		Offset: offset,
	}
}
