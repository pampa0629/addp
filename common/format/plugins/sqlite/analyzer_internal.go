package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

const (
	defaultTableLimit     = 10
	defaultSampleRowLimit = 20
)

// Options 控制 SQLite 分析行为
type Options struct {
	TableLimit     int
	SampleRowLimit int
	IncludeViews   bool
}

// DefaultOptions 返回默认分析选项
func DefaultOptions() Options {
	return Options{
		TableLimit:     defaultTableLimit,
		SampleRowLimit: defaultSampleRowLimit,
		IncludeViews:   true,
	}
}

// Metadata 表示 SQLite 数据库的元信息
type Metadata struct {
	Version    string      `json:"version"`
	PageSize   int         `json:"page_size"`
	PageCount  int64       `json:"page_count"`
	TableCount int         `json:"table_count"`
	ViewCount  int         `json:"view_count"`
	IndexCount int         `json:"index_count"`
	Tables     []TableInfo `json:"tables"`
}

// TableInfo 描述一张表/视图
type TableInfo struct {
	Name          string                   `json:"name"`
	Type          string                   `json:"type"` // table/view
	RowCount      *int64                   `json:"row_count,omitempty"`
	Columns       []ColumnInfo             `json:"columns"`
	SampleRows    []map[string]interface{} `json:"sample_rows,omitempty"`
	RowsTruncated bool                     `json:"rows_truncated,omitempty"`
}

// ColumnInfo 描述一列属性
type ColumnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null"`
	PrimaryKey bool   `json:"primary_key"`
}

// AnalysisResult 封装分析结果
type AnalysisResult struct {
	Metadata Metadata
}

// Analyze 分析 SQLite 数据库，返回元数据
func Analyze(ctx context.Context, db *sql.DB, opts *Options) (*AnalysisResult, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite analyzer: db is nil")
	}

	options := DefaultOptions()
	if opts != nil {
		if opts.TableLimit >= 0 {
			options.TableLimit = opts.TableLimit
		}
		if opts.SampleRowLimit >= 0 {
			options.SampleRowLimit = opts.SampleRowLimit
		}
		options.IncludeViews = opts.IncludeViews
	}

	meta := Metadata{
		Tables: make([]TableInfo, 0),
	}

	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&meta.Version); err != nil {
		return nil, fmt.Errorf("sqlite analyzer: fetch version failed: %w", err)
	}

	var pageSize sql.NullInt64
	if err := db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err == nil && pageSize.Valid {
		meta.PageSize = int(pageSize.Int64)
	}

	var pageCount sql.NullInt64
	if err := db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err == nil && pageCount.Valid {
		meta.PageCount = pageCount.Int64
	}

	if err := countDatabaseObjects(ctx, db, &meta); err != nil {
		return nil, err
	}

	tableTypes := []string{"table"}
	if options.IncludeViews {
		tableTypes = append(tableTypes, "view")
	}

	query := `
		SELECT name, type
		FROM sqlite_master
		WHERE type IN (` + placeholders(len(tableTypes)) + `)
		  AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`

	args := make([]interface{}, 0, len(tableTypes)+1)
	for _, t := range tableTypes {
		args = append(args, t)
	}
	if options.TableLimit > 0 {
		query += " LIMIT ?"
		args = append(args, options.TableLimit)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite analyzer: list tables failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var objectType string
		if err := rows.Scan(&name, &objectType); err != nil {
			continue
		}

		tableInfo, err := analyzeTable(ctx, db, name, strings.ToLower(objectType), options)
		if err != nil {
			continue
		}
		meta.Tables = append(meta.Tables, tableInfo)
	}

	return &AnalysisResult{Metadata: meta}, nil
}

func analyzeTable(ctx context.Context, db *sql.DB, name, objectType string, opts Options) (TableInfo, error) {
	info := TableInfo{
		Name:    name,
		Type:    objectType,
		Columns: make([]ColumnInfo, 0),
	}

	if err := populateColumns(ctx, db, &info); err != nil {
		return info, err
	}

	if count, err := queryRowCount(ctx, db, name); err == nil {
		info.RowCount = count
	}

	if opts.SampleRowLimit != 0 && strings.EqualFold(objectType, "table") {
		sample, truncated, err := sampleTableRows(ctx, db, name, opts.SampleRowLimit)
		if err == nil {
			info.SampleRows = sample
			info.RowsTruncated = truncated
		}
	}

	return info, nil
}

func populateColumns(ctx context.Context, db *sql.DB, table *TableInfo) error {
	pragmaQuery := fmt.Sprintf("PRAGMA table_info(%s)", escapeIdentifier(table.Name))
	rows, err := db.QueryContext(ctx, pragmaQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			colName   string
			colType   string
			notnull   int
			dfltValue interface{}
			pk        int
		)
		if err := rows.Scan(&cid, &colName, &colType, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		table.Columns = append(table.Columns, ColumnInfo{
			Name:       colName,
			Type:       colType,
			NotNull:    notnull == 1,
			PrimaryKey: pk == 1,
		})
	}
	return nil
}

func queryRowCount(ctx context.Context, db *sql.DB, table string) (*int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", escapeIdentifier(table))
	var rowCount int64
	if err := db.QueryRowContext(ctx, query).Scan(&rowCount); err != nil {
		return nil, err
	}
	return &rowCount, nil
}

func sampleTableRows(ctx context.Context, db *sql.DB, table string, limit int) ([]map[string]interface{}, bool, error) {
	if limit < 0 {
		limit = defaultSampleRowLimit
	}
	if limit == 0 {
		return nil, false, nil
	}

	query := fmt.Sprintf("SELECT * FROM %s LIMIT %d", escapeIdentifier(table), limit+1)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, false, err
	}

	sample := make([]map[string]interface{}, 0, limit)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		row := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			row[col] = normalizeSQLValue(values[i])
		}
		sample = append(sample, row)
		if len(sample) >= limit {
			break
		}
	}

	truncated := false
	if len(sample) > limit {
		sample = sample[:limit]
		truncated = true
	}

	return sample, truncated, nil
}

func normalizeSQLValue(value interface{}) interface{} {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	default:
		return v
	}
}

func countDatabaseObjects(ctx context.Context, db *sql.DB, meta *Metadata) error {
	query := `
		SELECT
			SUM(CASE WHEN type = 'table' THEN 1 ELSE 0 END) as table_count,
			SUM(CASE WHEN type = 'view' THEN 1 ELSE 0 END) as view_count,
			SUM(CASE WHEN type = 'index' THEN 1 ELSE 0 END) as index_count
		FROM sqlite_master
		WHERE name NOT LIKE 'sqlite_%'
	`
	return db.QueryRowContext(ctx, query).Scan(&meta.TableCount, &meta.ViewCount, &meta.IndexCount)
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func escapeIdentifier(name string) string {
	if identifierPattern.MatchString(name) {
		return name
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func placeholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}
