package extractors

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/meta/internal/scanner"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteExtractor SQLite数据库文件的元数据提取器
type SQLiteExtractor struct{}

func (e *SQLiteExtractor) SupportedTypes() []string {
	return []string{
		"application/x-sqlite3",
		"application/vnd.sqlite3",
		"application/octet-stream", // SQLite文件可能被识别为通用二进制
	}
}

func (e *SQLiteExtractor) Priority() int {
	// 优先级75，低于PDF (80)，但高于Default (50)
	return 75
}

func (e *SQLiteExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
	// 1. 将内容写入临时文件（SQLite需要文件路径）
	tmpFile, err := os.CreateTemp("", "sqlite-extract-*.db")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	// 2. 复制内容到临时文件
	written, err := io.Copy(tmpFile, input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// 3. 验证是否为有效的SQLite文件
	dsn := fmt.Sprintf("file:%s?mode=ro&_query_only=1&_busy_timeout=5000", strings.ReplaceAll(tmpPath, "\\", "/"))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("not a valid SQLite database: %w", err)
	}
	defer db.Close()

	// 4. 提取数据库元数据
	dbMeta, err := extractSQLiteMetadata(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("failed to extract SQLite metadata: %w", err)
	}

	// 5. 构建基础元数据
	metadata := &scanner.Metadata{
		BasicInfo: scanner.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "SQLite Database",
			Size:         written, // 使用实际写入的大小
			ContentType:  "application/x-sqlite3",
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 6. 添加SQLite专用元数据
	metadata.CustomAttrs["sqlite_metadata"] = dbMeta

	// 7. SQLite的schema信息已经包含在sqlite_metadata中
	// 如果需要统一的SchemaMetadata格式，可以在这里转换
	// 但SQLite数据库包含多个表，不适合用单一SchemaMetadata表示

	return metadata, nil
}

// sqliteMetadata SQLite数据库元数据结构
type sqliteMetadata struct {
	Version    string             `json:"version"`
	PageSize   int                `json:"page_size"`
	PageCount  int64              `json:"page_count"`
	TableCount int                `json:"table_count"`
	ViewCount  int                `json:"view_count"`
	IndexCount int                `json:"index_count"`
	Tables     []sqliteTableInfo  `json:"tables,omitempty"`
}

// sqliteTableInfo SQLite表信息
type sqliteTableInfo struct {
	Name       string              `json:"name"`
	Type       string              `json:"type"` // "table" 或 "view"
	RowCount   *int64              `json:"row_count,omitempty"`
	Columns    []sqliteColumnInfo  `json:"columns"`
}

// sqliteColumnInfo SQLite列信息
type sqliteColumnInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	NotNull   bool   `json:"not_null"`
	PrimaryKey bool  `json:"primary_key"`
}

// extractSQLiteMetadata 提取SQLite数据库元数据
func extractSQLiteMetadata(ctx context.Context, db *sql.DB) (*sqliteMetadata, error) {
	meta := &sqliteMetadata{
		Tables: make([]sqliteTableInfo, 0),
	}

	// 1. 获取SQLite版本
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&meta.Version); err != nil {
		return nil, fmt.Errorf("failed to get SQLite version: %w", err)
	}

	// 2. 获取数据库配置信息
	var pageSize sql.NullInt64
	if err := db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err == nil && pageSize.Valid {
		meta.PageSize = int(pageSize.Int64)
	}

	var pageCount sql.NullInt64
	if err := db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err == nil && pageCount.Valid {
		meta.PageCount = pageCount.Int64
	}

	// 3. 获取表、视图、索引统计
	countQuery := `
		SELECT
			SUM(CASE WHEN type = 'table' THEN 1 ELSE 0 END) as table_count,
			SUM(CASE WHEN type = 'view' THEN 1 ELSE 0 END) as view_count,
			SUM(CASE WHEN type = 'index' THEN 1 ELSE 0 END) as index_count
		FROM sqlite_master
		WHERE name NOT LIKE 'sqlite_%'
	`
	if err := db.QueryRowContext(ctx, countQuery).Scan(&meta.TableCount, &meta.ViewCount, &meta.IndexCount); err != nil {
		return nil, fmt.Errorf("failed to count database objects: %w", err)
	}

	// 4. 提取表和视图详细信息（限制前10个）
	tablesQuery := `
		SELECT name, type
		FROM sqlite_master
		WHERE type IN ('table', 'view')
		  AND name NOT LIKE 'sqlite_%'
		ORDER BY name
		LIMIT 10
	`
	rows, err := db.QueryContext(ctx, tablesQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name, objectType string
		if err := rows.Scan(&name, &objectType); err != nil {
			continue
		}

		tableInfo := sqliteTableInfo{
			Name:    name,
			Type:    objectType,
			Columns: make([]sqliteColumnInfo, 0),
		}

		// 获取列信息
		pragmaQuery := fmt.Sprintf("PRAGMA table_info(%s)", escapeSQLiteIdentifier(name))
		colRows, err := db.QueryContext(ctx, pragmaQuery)
		if err != nil {
			continue
		}

		for colRows.Next() {
			var (
				cid       int
				colName   string
				colType   string
				notnull   int
				dfltValue interface{}
				pk        int
			)
			if err := colRows.Scan(&cid, &colName, &colType, &notnull, &dfltValue, &pk); err != nil {
				continue
			}

			tableInfo.Columns = append(tableInfo.Columns, sqliteColumnInfo{
				Name:       colName,
				Type:       colType,
				NotNull:    notnull != 0,
				PrimaryKey: pk != 0,
			})
		}
		colRows.Close()

		// 获取行数（仅对table类型）
		if objectType == "table" {
			var rowCount int64
			countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", escapeSQLiteIdentifier(name))
			if err := db.QueryRowContext(ctx, countQuery).Scan(&rowCount); err == nil {
				tableInfo.RowCount = &rowCount
			}
		}

		meta.Tables = append(meta.Tables, tableInfo)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tables: %w", err)
	}

	return meta, nil
}

// escapeSQLiteIdentifier 转义SQLite标识符
func escapeSQLiteIdentifier(name string) string {
	escaped := strings.ReplaceAll(name, `"`, `""`)
	return `"` + escaped + `"`
}
