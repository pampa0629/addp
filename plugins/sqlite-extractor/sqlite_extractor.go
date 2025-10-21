// Package sqliteextractor SQLite数据库文件元数据提取器插件
package sqliteextractor

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/addp/meta-extractor-sdk"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteMetadata SQLite数据库的类型化元数据
type SQLiteMetadata struct {
	Version      string      `json:"version"`
	TableCount   int         `json:"table_count"`
	Tables       []TableInfo `json:"tables"`
	TotalRows    int64       `json:"total_rows"`
	DatabaseSize int64       `json:"database_size"`
	PageSize     int         `json:"page_size"`
	Encoding     string      `json:"encoding"`
}

// TableInfo 表信息
type TableInfo struct {
	Name       string       `json:"name"`
	RowCount   int64        `json:"row_count"`
	Columns    []ColumnInfo `json:"columns"`
	PrimaryKey []string     `json:"primary_key"`
	Indexes    []string     `json:"indexes"`
}

// ColumnInfo 列信息
type ColumnInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	NotNull      bool   `json:"not_null"`
	DefaultValue string `json:"default_value"`
}

// TypeName 实现 TypedMetadata 接口
func (m *SQLiteMetadata) TypeName() string {
	return "sqlite.metadata"
}

// Schema 实现 TypedMetadata 接口
func (m *SQLiteMetadata) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"version":       map[string]string{"type": "string", "description": "SQLite version"},
			"table_count":   map[string]string{"type": "integer", "description": "Number of tables"},
			"tables":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "object"}},
			"total_rows":    map[string]string{"type": "integer", "description": "Total rows across all tables"},
			"database_size": map[string]string{"type": "integer", "description": "Database file size in bytes"},
			"page_size":     map[string]string{"type": "integer", "description": "Database page size"},
			"encoding":      map[string]string{"type": "string", "description": "Database encoding"},
		},
		"required": []string{"version", "table_count"},
	}
}

// ToMap 实现 TypedMetadata 接口
func (m *SQLiteMetadata) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"version":       m.Version,
		"table_count":   m.TableCount,
		"tables":        m.Tables,
		"total_rows":    m.TotalRows,
		"database_size": m.DatabaseSize,
		"page_size":     m.PageSize,
		"encoding":      m.Encoding,
	}
}

// FromMap 实现 TypedMetadata 接口
func (m *SQLiteMetadata) FromMap(data map[string]interface{}) error {
	if v, ok := data["version"].(string); ok {
		m.Version = v
	}
	if v, ok := data["table_count"].(float64); ok {
		m.TableCount = int(v)
	}
	if v, ok := data["total_rows"].(float64); ok {
		m.TotalRows = int64(v)
	}
	if v, ok := data["database_size"].(float64); ok {
		m.DatabaseSize = int64(v)
	}
	if v, ok := data["page_size"].(float64); ok {
		m.PageSize = int(v)
	}
	if v, ok := data["encoding"].(string); ok {
		m.Encoding = v
	}
	// tables 的反序列化较复杂，这里简化处理
	return nil
}

// init 函数：注册自定义元数据类型
func init() {
	sdk.RegisterMetadataType(&SQLiteMetadata{})
}

// SQLiteExtractor SQLite数据库的元数据提取器
type SQLiteExtractor struct{}

// SupportedTypes 返回支持的MIME类型
func (e *SQLiteExtractor) SupportedTypes() []string {
	return []string{
		"application/vnd.sqlite3",
		"application/x-sqlite3",
		"application/octet-stream", // SQLite文件有时被识别为通用二进制
	}
}

// Priority 返回优先级
func (e *SQLiteExtractor) Priority() int {
	return 65
}

// Extract 提取SQLite数据库元数据
func (e *SQLiteExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
	// 1. 创建临时文件（SQLite需要文件路径访问）
	tmpFile, err := os.CreateTemp("", "sqlite-*.db")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 2. 将数据写入临时文件
	written, err := io.Copy(tmpFile, input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close() // 关闭文件以便SQLite可以打开

	// 3. 打开SQLite数据库
	db, err := sql.Open("sqlite3", tmpFile.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}
	defer db.Close()

	// 4. 提取数据库元数据
	sqliteMeta, err := e.extractDatabaseInfo(db)
	if err != nil {
		return nil, fmt.Errorf("failed to extract database info: %w", err)
	}

	sqliteMeta.DatabaseSize = written

	// 5. 创建基础元数据
	metadata := sdk.NewMetadata(
		filepath.Base(input.ObjectKey),
		"SQLite Database",
		input.Size,
	)

	metadata.BasicInfo.ContentType = input.ContentType
	metadata.BasicInfo.LastModified = input.LastModified
	metadata.BasicInfo.ETag = input.ETag

	// 6. 添加类型化元数据
	metadata.AddTypedMetadata("sqlite_metadata", sqliteMeta)

	// 7. 添加SchemaInfo（使用第一个表的结构作为示例）
	if len(sqliteMeta.Tables) > 0 {
		firstTable := sqliteMeta.Tables[0]
		columns := make([]sdk.ColumnInfo, len(firstTable.Columns))
		for i, col := range firstTable.Columns {
			columns[i] = sdk.ColumnInfo{
				Name:     col.Name,
				Type:     col.Type,
				Nullable: !col.NotNull,
			}
		}

		metadata.SchemaInfo = &sdk.SchemaMetadata{
			Columns:  columns,
			RowCount: firstTable.RowCount,
		}
	}

	// 8. 添加自定义属性
	metadata.CustomAttrs["database_version"] = sqliteMeta.Version
	metadata.CustomAttrs["table_count"] = sqliteMeta.TableCount
	metadata.CustomAttrs["total_rows"] = sqliteMeta.TotalRows
	metadata.CustomAttrs["file_size"] = input.Size
	metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)

	if summary := buildSQLitePlainText(sqliteMeta); summary != "" {
		trimmed := truncateRunes(summary, 20000)
		metadata.CustomAttrs["plain_text"] = trimmed
		metadata.CustomAttrs["plain_text_preview"] = truncateRunes(trimmed, 400)
	}

	return metadata, nil
}

// formatFileSize 格式化文件大小为人类可读格式
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// extractDatabaseInfo 提取数据库信息
func (e *SQLiteExtractor) extractDatabaseInfo(db *sql.DB) (*SQLiteMetadata, error) {
	meta := &SQLiteMetadata{
		Tables: []TableInfo{},
	}

	// 1. 获取SQLite版本
	var version string
	err := db.QueryRow("SELECT sqlite_version()").Scan(&version)
	if err == nil {
		meta.Version = version
	}

	// 2. 获取数据库配置
	var pageSize int
	err = db.QueryRow("PRAGMA page_size").Scan(&pageSize)
	if err == nil {
		meta.PageSize = pageSize
	}

	var encoding string
	err = db.QueryRow("PRAGMA encoding").Scan(&encoding)
	if err == nil {
		meta.Encoding = encoding
	}

	// 3. 获取所有表
	tables, err := e.getTables(db)
	if err != nil {
		return nil, err
	}

	meta.TableCount = len(tables)
	meta.Tables = tables

	// 4. 统计总行数
	var totalRows int64
	for _, table := range tables {
		totalRows += table.RowCount
	}
	meta.TotalRows = totalRows

	return meta, nil
}

// getTables 获取所有表信息
func (e *SQLiteExtractor) getTables(db *sql.DB) ([]TableInfo, error) {
	// 查询所有表（排除系统表）
	rows, err := db.Query(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}

		tableInfo := TableInfo{
			Name:    tableName,
			Columns: []ColumnInfo{},
			Indexes: []string{},
		}

		// 获取表的行数
		var rowCount int64
		err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM \"%s\"", tableName)).Scan(&rowCount)
		if err == nil {
			tableInfo.RowCount = rowCount
		}

		// 获取表结构
		columns, primaryKey, err := e.getTableSchema(db, tableName)
		if err == nil {
			tableInfo.Columns = columns
			tableInfo.PrimaryKey = primaryKey
		}

		// 获取索引
		indexes, err := e.getTableIndexes(db, tableName)
		if err == nil {
			tableInfo.Indexes = indexes
		}

		tables = append(tables, tableInfo)
	}

	return tables, nil
}

// getTableSchema 获取表结构
func (e *SQLiteExtractor) getTableSchema(db *sql.DB, tableName string) ([]ColumnInfo, []string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(\"%s\")", tableName))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var columns []ColumnInfo
	var primaryKey []string

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue sql.NullString

		err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk)
		if err != nil {
			continue
		}

		column := ColumnInfo{
			Name:    name,
			Type:    colType,
			NotNull: notNull == 1,
		}
		if dfltValue.Valid {
			column.DefaultValue = dfltValue.String
		}

		columns = append(columns, column)

		if pk > 0 {
			primaryKey = append(primaryKey, name)
		}
	}

	return columns, primaryKey, nil
}

// getTableIndexes 获取表的索引
func (e *SQLiteExtractor) getTableIndexes(db *sql.DB, tableName string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA index_list(\"%s\")", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []string
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int

		err := rows.Scan(&seq, &name, &unique, &origin, &partial)
		if err != nil {
			continue
		}

		indexes = append(indexes, name)
	}

	return indexes, nil
}

// GetExtractor 返回提取器实例（供ADDP加载使用）
func GetExtractor() sdk.MetadataExtractor {
	return &SQLiteExtractor{}
}

func buildSQLitePlainText(meta *SQLiteMetadata) string {
	if meta == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("SQLite Version: %s\n", meta.Version))
	builder.WriteString(fmt.Sprintf("Tables: %d\n", meta.TableCount))
	builder.WriteString(fmt.Sprintf("Total Rows: %d\n", meta.TotalRows))

	limit := 5
	if len(meta.Tables) < limit {
		limit = len(meta.Tables)
	}
	for i := 0; i < limit; i++ {
		table := meta.Tables[i]
		builder.WriteString(fmt.Sprintf("Table %s (rows: %d)\n", table.Name, table.RowCount))
		colLimit := 10
		if len(table.Columns) < colLimit {
			colLimit = len(table.Columns)
		}
		for j := 0; j < colLimit; j++ {
			col := table.Columns[j]
			notNull := ""
			if col.NotNull {
				notNull = " NOT NULL"
			}
			builder.WriteString(fmt.Sprintf("  - %s %s%s\n", col.Name, col.Type, notNull))
		}
		if len(table.PrimaryKey) > 0 {
			builder.WriteString(fmt.Sprintf("  PK: %s\n", strings.Join(table.PrimaryKey, ", ")))
		}
		if len(table.Indexes) > 0 {
			builder.WriteString(fmt.Sprintf("  Indexes: %s\n", strings.Join(table.Indexes, ", ")))
		}
	}

	return strings.TrimSpace(builder.String())
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
