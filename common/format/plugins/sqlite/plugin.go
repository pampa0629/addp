package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	_ "github.com/mattn/go-sqlite3"
)

// Plugin 实现 SQLite / GeoPackage 格式插件。
type Plugin struct {
	formatType format.FormatType
	options    *format.ParseOptions
}

// NewPlugin 创建 SQLite 插件。
func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{formatType: format.FormatSQLite, options: opts}
}

func NewGeoPackagePlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{formatType: format.FormatGeoPackage, options: opts}
}

func (p *Plugin) Format() format.FormatType {
	if p.formatType == "" {
		return format.FormatSQLite
	}
	return p.formatType
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	if p.Format() == format.FormatGeoPackage {
		return format.FormatDescriptor{
			ID:             "builtin-geopackage",
			Format:         format.FormatGeoPackage,
			I18nKey:        "format.geopackage",
			DataType:       format.FormatDataTypeContainer,
			Layouts:        []string{format.FormatLayoutSingle},
			ProviderHints:  []string{format.FormatProviderContainer, format.FormatProviderTable, format.FormatProviderSpatial},
			Identification: format.FormatIdentification{Extensions: []string{".gpkg"}, MimeTypes: []string{"application/geopackage+sqlite3"}},
			Providers:      format.FormatProviderDescriptor{ContainerInfo: true, TableInfo: true, TableSample: true, Table: true},
			ContentReaders: []string{string(format.ContentReaderTableSample), string(format.ContentReaderRawContent), string(format.ContentReaderContainerEntry)},
			Spatial:        true,
			EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile},
		}
	}
	return format.FormatDescriptor{
		ID:             "builtin-sqlite",
		Format:         format.FormatSQLite,
		I18nKey:        "format.sqlite",
		DataType:       format.FormatDataTypeContainer,
		Layouts:        []string{format.FormatLayoutSingle},
		ProviderHints:  []string{format.FormatProviderContainer, format.FormatProviderTable},
		Identification: format.FormatIdentification{Extensions: []string{".sqlite", ".sqlite3", ".db"}, MimeTypes: []string{"application/x-sqlite3", "application/vnd.sqlite3", "application/sqlite"}},
		Providers:      format.FormatProviderDescriptor{ContainerInfo: true, TableInfo: true, TableSample: true, Table: true},
		ContentReaders: []string{string(format.ContentReaderTableSample), string(format.ContentReaderRawContent), string(format.ContentReaderContainerEntry)},
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.Format())
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        p.Format(),
		DataType:      format.FormatDataTypeContainer,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderContainer, format.FormatProviderTable},
		ContentReaders: []string{
			string(format.ContentReaderTableSample),
			string(format.ContentReaderRawContent),
			string(format.ContentReaderContainerEntry),
		},
		Parse: true,
	}
}

func (p *Plugin) DescribeFormat(ctx context.Context, input io.Reader, options *format.ParseOptions) (map[string]interface{}, error) {
	db, cleanup, err := p.openDatabase(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	result, err := Analyze(ctx, db, p.analysisOptions(options))
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"version":     result.Metadata.Version,
		"page_size":   result.Metadata.PageSize,
		"page_count":  result.Metadata.PageCount,
		"table_count": result.Metadata.TableCount,
		"view_count":  result.Metadata.ViewCount,
		"index_count": result.Metadata.IndexCount,
	}, nil
}

func (p *Plugin) ResolveContainerChild(_ context.Context, parent contentio.Reader, parentRef contentio.Ref, child format.ContainerChildInfo, _ *format.ParseOptions) (*format.ContainerChildResource, error) {
	return format.NativeContainerChildResource(parent, parentRef, p.Format(), child, format.ChildTableParseOptions(child.Name, child.Properties)), nil
}

func (p *Plugin) DescribeContainer(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.ContainerInfo, error) {
	db, cleanup, err := p.openDatabase(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	result, err := Analyze(ctx, db, p.analysisOptions(options))
	if err != nil {
		return nil, err
	}

	layerByTable := map[string]geoPackageLayer{}
	if p.Format() == format.FormatGeoPackage {
		layerByTable = readGeoPackageLayers(ctx, db)
	}

	children := make([]format.ContainerChildInfo, 0, len(result.Metadata.Tables))
	for _, table := range result.Metadata.Tables {
		if p.Format() == format.FormatGeoPackage && isGeoPackageSystemTable(table.Name) {
			continue
		}
		columnCount := len(table.Columns)
		name := table.Name
		kind := table.Type
		if layer, ok := layerByTable[table.Name]; ok {
			kind = "layer"
			name = layer.Identifier
			if name == "" {
				name = table.Name
			}
		}
		children = append(children, format.ContainerChildInfo{
			Name:        name,
			ChildKind:   kind,
			DataType:    format.FormatDataTypeTable,
			RowCount:    table.RowCount,
			ColumnCount: &columnCount,
			Properties: map[string]interface{}{
				"table": table.Name,
			},
		})
	}
	defaultChild := ""
	if len(children) > 0 {
		defaultChild = children[0].Name
	}
	childCount := result.Metadata.TableCount + result.Metadata.ViewCount
	childrenTruncated := childCount > len(children)
	if p.Format() == format.FormatGeoPackage {
		childCount = len(children)
		childrenTruncated = false
	}
	return &format.ContainerInfo{
		Format:        p.Format(),
		ChildCount:    childCount,
		DefaultChild:  defaultChild,
		ResourceCount: 1,
		Children:      children,
		FormatInfo: map[string]interface{}{
			"version":            result.Metadata.Version,
			"page_size":          result.Metadata.PageSize,
			"page_count":         result.Metadata.PageCount,
			"table_count":        result.Metadata.TableCount,
			"view_count":         result.Metadata.ViewCount,
			"index_count":        result.Metadata.IndexCount,
			"sampled_children":   len(children),
			"children_truncated": childrenTruncated,
		},
	}, nil
}

func (p *Plugin) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	tableName := tableNameFromOptions(options)
	if tableName == "" {
		return nil, fmt.Errorf("sqlite table preview requires table option")
	}

	db, cleanup, err := p.openDatabase(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	table, err := describeSQLiteTable(ctx, db, tableName)
	if err != nil {
		return nil, err
	}
	info := sqliteTableInfoToFormatTable(table)
	if p.Format() == format.FormatGeoPackage {
		applyGeoPackageSpatialInfo(ctx, db, info)
	}
	return info, nil
}

func (p *Plugin) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	db, cleanup, err := p.openDatabase(input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	tableName := tableNameFromOptions(options)
	if tableName == "" {
		result, err := Analyze(ctx, db, p.analysisOptions(options))
		if err != nil {
			return nil, err
		}
		table := firstSQLiteTable(result)
		if table == nil {
			return []map[string]interface{}{}, nil
		}
		tableName = table.Name
	}
	return sampleSQLiteTableWindow(ctx, db, tableName, offset, limit)
}

func (p *Plugin) openDatabase(input io.Reader) (*sql.DB, func(), error) {
	tempPath, cleanupFile, err := p.saveToTempFile(input)
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("sqlite3", tempPath)
	if err != nil {
		cleanupFile()
		return nil, nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	cleanup := func() {
		_ = db.Close()
		cleanupFile()
	}
	return db, cleanup, nil
}

func (p *Plugin) analysisOptions(options *format.ParseOptions) *Options {
	result := DefaultOptions()
	if options == nil {
		options = p.options
	}
	if options == nil {
		return &result
	}
	if options.SampleSize > 0 {
		result.SampleRowLimit = options.SampleSize
	}
	if options.ExtraParams != nil {
		if v, ok := options.ExtraParams[format.ContainerChildLimitParam].(int); ok && v >= 0 {
			result.TableLimit = v
		}
		if v, ok := options.ExtraParams[format.ContainerRowLimitParam].(int); ok && v >= 0 {
			result.SampleRowLimit = v
		}
	}
	return &result
}

func firstSQLiteTable(result *AnalysisResult) *TableInfo {
	if result == nil {
		return nil
	}
	for i := range result.Metadata.Tables {
		if strings.EqualFold(result.Metadata.Tables[i].Type, "table") {
			return &result.Metadata.Tables[i]
		}
	}
	if len(result.Metadata.Tables) == 0 {
		return nil
	}
	return &result.Metadata.Tables[0]
}

func describeSQLiteTable(ctx context.Context, db *sql.DB, tableName string) (TableInfo, error) {
	objectType := "table"
	err := db.QueryRowContext(ctx, `
		SELECT type
		FROM sqlite_master
		WHERE name = ?
		  AND type IN ('table', 'view')
		  AND name NOT LIKE 'sqlite_%'
	`, tableName).Scan(&objectType)
	if err != nil {
		return TableInfo{}, fmt.Errorf("sqlite table %q not found: %w", tableName, err)
	}
	opts := DefaultOptions()
	opts.SampleRowLimit = 0
	return analyzeTable(ctx, db, tableName, strings.ToLower(objectType), opts)
}

func sqliteTableInfoToFormatTable(table TableInfo) *format.TableInfo {
	fields := make([]format.FieldInfo, 0, len(table.Columns))
	primaryKey := make([]string, 0)
	for _, column := range table.Columns {
		fields = append(fields, format.FieldInfo{
			Name:         column.Name,
			Type:         mapSQLiteTypeToFieldType(column.Type),
			Nullable:     !column.NotNull,
			IsPrimaryKey: column.PrimaryKey,
		})
		if column.PrimaryKey {
			primaryKey = append(primaryKey, column.Name)
		}
	}
	return &format.TableInfo{
		Name:       table.Name,
		RowCount:   table.RowCount,
		Fields:     fields,
		PrimaryKey: primaryKey,
	}
}

func tableNameFromOptions(options *format.ParseOptions) string {
	if options == nil || options.ExtraParams == nil {
		return ""
	}
	if v, ok := options.ExtraParams[format.ChildTableParam].(string); ok {
		return strings.TrimSpace(v)
	}
	if v, ok := options.ExtraParams[format.ChildNameParam].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func sampleSQLiteTableWindow(ctx context.Context, db *sql.DB, table string, offset, limit int64) ([]map[string]interface{}, error) {
	if limit < 0 {
		limit = defaultSampleRowLimit
	}
	if limit == 0 {
		return []map[string]interface{}{}, nil
	}
	if offset < 0 {
		offset = 0
	}
	query := fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d", escapeIdentifier(table), limit, offset)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

func scanRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, 0)
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
		for i, column := range columns {
			row[column] = normalizeSQLValue(values[i])
		}
		result = append(result, row)
	}
	return result, rows.Err()
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
		return format.FieldTypeDouble // SQLite REAL 是 8 字节双精度
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
func (p *Plugin) saveToTempFile(input io.Reader) (string, func(), error) {
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
	_ = format.RegisterFormatPlugin(NewPlugin(nil))
	_ = format.RegisterFormatPlugin(NewGeoPackagePlugin(nil))
}
