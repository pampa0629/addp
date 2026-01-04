package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"

	"github.com/addp/common/format"
	_ "github.com/mattn/go-sqlite3"
)

// Parser 实现 SQLite 格式的解析器
type Parser struct {
	options *format.ParseOptions
}

// NewParser 创建 SQLite 解析器
func NewParser(opts *format.ParseOptions) *Parser {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Parser{options: opts}
}

// SupportedFormats 返回支持的格式
func (p *Parser) SupportedFormats() []format.FormatType {
	return []format.FormatType{format.FormatSQLite}
}

// ParseSchema 解析 SQLite Schema
// 注意：SQLite 需要文件路径，input 需要先保存到临时文件
func (p *Parser) ParseSchema(ctx context.Context, input io.Reader) (*format.Schema, error) {
	// 将 input 保存到临时文件
	tempFile, cleanup, err := p.saveToTempFile(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// 打开数据库
	db, err := sql.Open("sqlite3", tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	defer db.Close()

	// 使用内部 Analyze 函数
	analyzeOpts := p.buildAnalyzeOptions()
	result, err := Analyze(ctx, db, analyzeOpts)
	if err != nil {
		return nil, err
	}

	// 转换为标准 Schema
	return p.convertToSchema(result)
}

// ReadRecords 读取 SQLite 数据记录
func (p *Parser) ReadRecords(ctx context.Context, input io.Reader, offset, limit int64) ([]map[string]interface{}, error) {
	tempFile, cleanup, err := p.saveToTempFile(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	db, err := sql.Open("sqlite3", tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	defer db.Close()

	// 获取表名（默认第一个表）
	tableName, err := p.getTargetTable(ctx, db)
	if err != nil || tableName == "" {
		return []map[string]interface{}{}, nil
	}

	// 构建查询
	query := fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d",
		escapeIdentifier(tableName), limit, offset)

	if limit < 0 {
		query = fmt.Sprintf("SELECT * FROM %s OFFSET %d",
			escapeIdentifier(tableName), offset)
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table %s: %w", tableName, err)
	}
	defer rows.Close()

	// 获取列名
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// 读取数据
	records := make([]map[string]interface{}, 0)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		record := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			record[col] = normalizeSQLValue(values[i])
		}
		records = append(records, record)
	}

	return records, nil
}

// CountRecords 统计总记录数
func (p *Parser) CountRecords(ctx context.Context, input io.Reader) (int64, error) {
	tempFile, cleanup, err := p.saveToTempFile(input)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	db, err := sql.Open("sqlite3", tempFile)
	if err != nil {
		return 0, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	defer db.Close()

	tableName, err := p.getTargetTable(ctx, db)
	if err != nil || tableName == "" {
		return 0, nil
	}

	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", escapeIdentifier(tableName))
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count records: %w", err)
	}

	return count, nil
}

// ExtractMetadata 提取 SQLite 元数据
func (p *Parser) ExtractMetadata(ctx context.Context, input io.Reader) (map[string]interface{}, error) {
	tempFile, cleanup, err := p.saveToTempFile(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	db, err := sql.Open("sqlite3", tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	defer db.Close()

	// 使用内部 Analyze 获取详细信息
	analyzeOpts := p.buildAnalyzeOptions()
	result, err := Analyze(ctx, db, analyzeOpts)
	if err != nil {
		return nil, err
	}

	// 构建元数据
	metadata := map[string]interface{}{
		"version":     result.Metadata.Version,
		"page_size":   result.Metadata.PageSize,
		"page_count":  result.Metadata.PageCount,
		"table_count": result.Metadata.TableCount,
		"view_count":  result.Metadata.ViewCount,
		"index_count": result.Metadata.IndexCount,
		"tables":      result.Metadata.Tables,
	}

	return metadata, nil
}

// buildAnalyzeOptions 根据 ParseOptions 构建 Analyze 选项
func (p *Parser) buildAnalyzeOptions() *Options {
	opts := &Options{
		TableLimit:     defaultTableLimit,
		SampleRowLimit: defaultSampleRowLimit,
		IncludeViews:   true,
	}

	// 从 ExtraParams 中读取自定义参数
	if p.options.ExtraParams != nil {
		if v, ok := p.options.ExtraParams["table_limit"].(int); ok && v > 0 {
			opts.TableLimit = v
		}
		if v, ok := p.options.ExtraParams["sample_row_limit"].(int); ok && v >= 0 {
			opts.SampleRowLimit = v
		}
		if v, ok := p.options.ExtraParams["include_views"].(bool); ok {
			opts.IncludeViews = v
		}
	}

	return opts
}

// getTargetTable 获取目标表名
func (p *Parser) getTargetTable(ctx context.Context, db *sql.DB) (string, error) {
	// 从 ExtraParams 中读取指定的表名
	if p.options.ExtraParams != nil {
		if tableName, ok := p.options.ExtraParams["table_name"].(string); ok && tableName != "" {
			return tableName, nil
		}
	}

	// 默认返回第一个表
	query := `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		ORDER BY name
		LIMIT 1
	`

	var tableName string
	if err := db.QueryRowContext(ctx, query).Scan(&tableName); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}

	return tableName, nil
}

// convertToSchema 将 AnalysisResult 转换为标准 Schema
func (p *Parser) convertToSchema(result *AnalysisResult) (*format.Schema, error) {
	if len(result.Metadata.Tables) == 0 {
		return &format.Schema{Fields: []format.Field{}}, nil
	}

	// 使用第一个表的信息
	table := result.Metadata.Tables[0]

	// 构建字段列表
	fields := make([]format.Field, len(table.Columns))

	// 收集主键字段名
	primaryKeys := make([]string, 0)

	for i, col := range table.Columns {
		fields[i] = format.Field{
			Name:     col.Name,
			Type:     mapSQLiteTypeToFieldType(col.Type),
			Nullable: !col.NotNull,
		}

		if col.PrimaryKey {
			primaryKeys = append(primaryKeys, col.Name)
		}
	}

	schema := &format.Schema{
		Fields:     fields,
		PrimaryKey: primaryKeys,
	}

	// 添加记录数信息
	if table.RowCount != nil {
		schema.RecordCount = table.RowCount
	}

	return schema, nil
}

// mapSQLiteTypeToFieldType 将 SQLite 类型映射到 FieldType
func mapSQLiteTypeToFieldType(sqliteType string) format.FieldType {
	// SQLite 类型不区分大小写
	upperType := ""
	for _, r := range sqliteType {
		if r >= 'a' && r <= 'z' {
			upperType += string(r - 32)
		} else {
			upperType += string(r)
		}
	}

	// SQLite 类型亲和性规则
	switch {
	case contains(upperType, "INT"):
		return format.FieldTypeInt
	case contains(upperType, "CHAR") || contains(upperType, "CLOB") || contains(upperType, "TEXT"):
		return format.FieldTypeString
	case contains(upperType, "BLOB"):
		return format.FieldTypeBytes
	case contains(upperType, "REAL") || contains(upperType, "FLOA") || contains(upperType, "DOUB"):
		return format.FieldTypeFloat
	case contains(upperType, "DATE") || contains(upperType, "TIME"):
		return format.FieldTypeTimestamp
	case contains(upperType, "BOOL"):
		return format.FieldTypeBool
	default:
		return format.FieldTypeString
	}
}

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

// indexOf 查找子串位置
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// saveToTempFile 将 io.Reader 保存到临时文件
func (p *Parser) saveToTempFile(input io.Reader) (string, func(), error) {
	// 创建临时文件
	tempFile, err := os.CreateTemp("", "sqlite-*.db")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// 写入数据
	if _, err := io.Copy(tempFile, input); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return "", nil, fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		os.Remove(tempPath)
		return "", nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// 返回清理函数
	cleanup := func() {
		os.Remove(tempPath)
	}

	return tempPath, cleanup, nil
}

func init() {
	// TODO: SQLite parser 需要实现 FileTableParser 接口（ParseTableInfo、ReadPreview 方法）
	// 暂时不注册，等待实现新接口
	// parser := NewParser(nil)
	// _ = format.RegisterFileTableParser(parser)
}
