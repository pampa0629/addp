package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/common/datatype"
	commoni18n "github.com/addp/common/middleware/i18n"
)

// QueryResult 查询结果
type QueryResult struct {
	Columns         []string                 `json:"columns"`
	Fields          []datatype.FieldInfo     `json:"fields"`
	Rows            []map[string]interface{} `json:"rows"`
	RowCount        int                      `json:"row_count"`
	ExecutionTimeMs int64                    `json:"execution_time_ms"`
}

// ExecuteQuery 在给定连接上执行 SQL 并返回结果
func ExecuteQuery(ctx context.Context, conn *sql.Conn, sqlStr string, args ...interface{}) (*QueryResult, error) {
	start := time.Now()

	rows, err := conn.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("DuckDB 查询执行失败: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("获取列信息失败: %w", err)
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("read DuckDB output metadata: %w", err)
	}
	fields := make([]datatype.FieldInfo, len(types))
	seen := make(map[string]bool, len(types))
	for i, column := range types {
		if seen[column.Name()] {
			return nil, commoni18n.NewMessageError("engine.query_runtime.output_invalid")
		}
		seen[column.Name()] = true
		fields[i], err = outputField(column, i+1)
		if err != nil {
			return nil, err
		}
	}

	var resultRows []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("读取行数据失败: %w", err)
		}
		row := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			row[col] = outputValue(values[i])
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历结果集失败: %w", err)
	}

	if resultRows == nil {
		resultRows = []map[string]interface{}{}
	}

	return &QueryResult{
		Columns:         cols,
		Fields:          fields,
		Rows:            resultRows,
		RowCount:        len(resultRows),
		ExecutionTimeMs: time.Since(start).Milliseconds(),
	}, nil
}
