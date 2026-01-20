package readers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/utils"
	_ "github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

// JDBCReader JDBC 数据读取器
// 支持 PostgreSQL, MySQL 等关系型数据库
type JDBCReader struct {
	db           *sql.DB
	query        string
	batchSize    int
	offset       int64
	columns      []string
	schema       *pipeline.Schema
	mode         pipeline.ReaderMode
	lastReadAt   time.Time
	pollInterval time.Duration
	driver       string   // 数据库驱动类型
	database     string   // 数据库名称（用于主键检测）
	table        string   // 表名（用于主键检测）

	// 游标寻址优化（避免 OFFSET 扫描）
	useCursor       bool        // 是否启用游标模式
	cursorColumn    string      // 游标列（主键或可排序唯一列）
	lastCursorValue interface{} // 上次读取的游标值
	baseQuery       string      // 不含 LIMIT/OFFSET 的基础查询
}

// JDBCConfig JDBC 连接配置
type JDBCConfig struct {
	Driver           string `json:"driver"` // postgres, mysql
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Database         string `json:"database"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	Query            string `json:"query"`         // SQL 查询语句
	Table            string `json:"table"`         // 表名（与 Query 二选一）
	WhereClause      string `json:"where_clause"`  // WHERE 条件
	OrderBy          string `json:"order_by"`      // 排序字段（用于分页）
	SSLMode          string `json:"ssl_mode"`      // SSL 模式
	PollInterval     int    `json:"poll_interval"` // 流式模式轮询间隔（秒）
	ConnectionString string `json:"connection_string"`

	// 游标寻址优化配置
	UseCursor    bool   `json:"use_cursor"`    // 是否启用游标模式（默认自动检测）
	CursorColumn string `json:"cursor_column"` // 指定游标列（不指定则自动检测主键）
}

// NewJDBCReader 创建 JDBC Reader
func NewJDBCReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	var jdbcConfig JDBCConfig
	if err := utils.MapToStruct(config.Config, &jdbcConfig); err != nil {
		return nil, fmt.Errorf("invalid jdbc config: %w", err)
	}

	if jdbcConfig.Driver == "" {
		if config.Type != "" {
			jdbcConfig.Driver = config.Type
		} else {
			jdbcConfig.Driver = "postgresql"
		}
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 5000 // 提升批次大小：1000 → 5000（减少查询次数，降低 OFFSET 扫描开销）
	}

	pollInterval := time.Duration(jdbcConfig.PollInterval) * time.Second
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}

	return &JDBCReader{
		batchSize:    batchSize,
		mode:         pipeline.ModeBatch,
		pollInterval: pollInterval,
	}, nil
}

// Open 打开数据库连接
func (r *JDBCReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var jdbcConfig JDBCConfig
	if err := utils.MapToStruct(config.Config, &jdbcConfig); err != nil {
		return err
	}

	if jdbcConfig.Driver == "" {
		if config.Type != "" {
			jdbcConfig.Driver = config.Type
		} else {
			jdbcConfig.Driver = "postgresql"
		}
	}

	// 构建连接字符串
	connStr, err := r.buildConnectionString(jdbcConfig)
	if err != nil {
		return err
	}

	// 标准化 driver 名称（postgresql -> postgres）
	driverName := r.normalizeDriverName(jdbcConfig.Driver)

	// 打开数据库连接
	db, err := sql.Open(driverName, connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 测试连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	r.db = db
	r.driver = jdbcConfig.Driver // 保存驱动类型用于标识符引用
	r.database = jdbcConfig.Database // 保存数据库名用于主键检测
	r.table = jdbcConfig.Table // 保存表名用于主键检测

	// 构建查询语句
	if jdbcConfig.Query != "" {
		r.query = jdbcConfig.Query
	} else if jdbcConfig.Table != "" {
		r.query = r.buildSelectQuery(jdbcConfig)
	} else {
		return fmt.Errorf("either query or table must be specified")
	}

	// 获取 schema
	schema, err := r.inferSchema(ctx)
	if err != nil {
		return fmt.Errorf("failed to infer schema: %w", err)
	}
	r.schema = schema

	// 初始化游标寻址优化
	if err := r.initializeCursor(ctx, jdbcConfig); err != nil {
		// 游标初始化失败不是致命错误，降级到 OFFSET 模式
		fmt.Printf("Warning: cursor initialization failed, falling back to OFFSET mode: %v\n", err)
		r.useCursor = false
	}

	return nil
}

// Read 读取一批数据
func (r *JDBCReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	// 每次都执行新的分页查询
	query := r.buildPaginatedQuery()
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// 获取列名（第一次或列名未初始化时）
	if r.columns == nil {
		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		r.columns = columns
	}

	// 读取批次数据
	var batchRows []map[string]interface{}
	for rows.Next() {
		row, err := r.scanRow(rows)
		if err != nil {
			return nil, err
		}
		batchRows = append(batchRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	// 检查是否读完
	if len(batchRows) == 0 {
		return nil, io.EOF
	}

	// 更新游标值（游标模式）或偏移量（OFFSET 模式）
	if r.useCursor && r.cursorColumn != "" && len(batchRows) > 0 {
		// 获取最后一行的游标列值
		lastRow := batchRows[len(batchRows)-1]
		if cursorValue, exists := lastRow[r.cursorColumn]; exists {
			r.lastCursorValue = cursorValue
		} else {
			// 如果游标列不存在，降级到 OFFSET 模式
			fmt.Printf("Warning: cursor column '%s' not found in result, falling back to OFFSET mode\n", r.cursorColumn)
			r.useCursor = false
			r.offset += int64(len(batchRows))
		}
	} else {
		// OFFSET 模式：更新偏移量
		r.offset += int64(len(batchRows))
	}

	r.lastReadAt = time.Now()

	return &pipeline.DataBatch{
		Rows:      batchRows,
		Schema:    r.schema,
		Offset:    r.offset,
		Timestamp: time.Now(),
	}, nil
}

// Schema 返回数据 schema
func (r *JDBCReader) Schema() (*pipeline.Schema, error) {
	if r.schema == nil {
		return nil, fmt.Errorf("schema not initialized, call Open first")
	}
	return r.schema, nil
}

// SeekTo 跳转到指定偏移量
func (r *JDBCReader) SeekTo(offset int64) error {
	r.offset = offset
	return nil
}

// Close 关闭连接
func (r *JDBCReader) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

// Mode 返回读取模式
func (r *JDBCReader) Mode() pipeline.ReaderMode {
	return r.mode
}

// normalizeDriverName 标准化 driver 名称为 sql.Open 识别的名称
func (r *JDBCReader) normalizeDriverName(driver string) string {
	switch driver {
	case "":
		return "postgres"
	case "postgresql":
		return "postgres"
	default:
		return driver
	}
}

// buildConnectionString 构建连接字符串
func (r *JDBCReader) buildConnectionString(config JDBCConfig) (string, error) {
	if config.ConnectionString != "" {
		return config.ConnectionString, nil
	}

	switch config.Driver {
	case "postgres", "postgresql":
		sslMode := config.SSLMode
		if sslMode == "" {
			sslMode = "disable"
		}
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			config.Host, config.Port, config.Username, config.Password, config.Database, sslMode), nil

	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			config.Username, config.Password, config.Host, config.Port, config.Database), nil

	default:
		return "", fmt.Errorf("unsupported driver: %s", config.Driver)
	}
}

func shouldPreserveBinary(dbType string) bool {
	switch dbType {
	case "BYTEA", "BLOB", "VARBINARY", "GEOMETRY", "GEOGRAPHY", "POINT", "POLYGON",
		"LINESTRING", "MULTIPOINT", "MULTIPOLYGON", "MULTILINESTRING", "GEOM", "GEOMCOLLECTION":
		return true
	default:
		return false
	}
}

// buildSelectQuery 构建 SELECT 查询
func (r *JDBCReader) buildSelectQuery(config JDBCConfig) string {
	// 处理 schema.table 格式
	schema, table := r.parseTableName(config.Table)
	var quotedTable string
	if schema != "" {
		// 分别引用 schema 和 table
		quotedTable = r.quoteIdentifier(schema) + "." + r.quoteIdentifier(table)
	} else {
		quotedTable = r.quoteIdentifier(table)
	}

	query := fmt.Sprintf("SELECT * FROM %s", quotedTable)

	if config.WhereClause != "" {
		query += " WHERE " + config.WhereClause
	}

	if config.OrderBy != "" {
		query += " ORDER BY " + config.OrderBy
	}

	return query
}

// quoteIdentifier 根据数据库类型引用标识符以保留大小写
func (r *JDBCReader) quoteIdentifier(identifier string) string {
	switch strings.ToLower(r.driver) {
	case "postgres", "postgresql":
		return pq.QuoteIdentifier(identifier)
	case "mysql":
		// MySQL 使用反引号
		return fmt.Sprintf("`%s`", strings.ReplaceAll(identifier, "`", "``"))
	default:
		// 未知数据库类型，保守策略：不加引号
		return identifier
	}
}

// buildPaginatedQuery 构建分页查询
// buildPaginatedQuery 构建分页查询
// 优化：使用游标寻址（WHERE id > last_id）替代 OFFSET 扫描
func (r *JDBCReader) buildPaginatedQuery() string {
	if r.useCursor && r.cursorColumn != "" {
		// 游标模式：使用 WHERE column > lastValue
		if r.lastCursorValue == nil {
			// 第一批：直接查询
			return fmt.Sprintf("%s ORDER BY %s LIMIT %d",
				r.baseQuery, r.quoteIdentifier(r.cursorColumn), r.batchSize)
		} else {
			// 后续批次：基于游标继续
			// 需要在基础查询中添加 WHERE 条件
			separator := " WHERE "
			if strings.Contains(strings.ToUpper(r.baseQuery), " WHERE ") {
				separator = " AND "
			}

			return fmt.Sprintf("%s%s%s > %s ORDER BY %s LIMIT %d",
				r.baseQuery,
				separator,
				r.quoteIdentifier(r.cursorColumn),
				r.formatCursorValue(r.lastCursorValue),
				r.quoteIdentifier(r.cursorColumn),
				r.batchSize)
		}
	}

	// 降级方案：传统 OFFSET 分页
	return fmt.Sprintf("%s LIMIT %d OFFSET %d", r.query, r.batchSize, r.offset)
}

// scanRow 扫描行数据
func (r *JDBCReader) scanRow(rows *sql.Rows) (map[string]interface{}, error) {
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	values := make([]interface{}, len(r.columns))
	valuePtrs := make([]interface{}, len(r.columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, err
	}

	row := make(map[string]interface{})
	for i, col := range r.columns {
		val := values[i]

		if val == nil {
			row[col] = nil
			continue
		}

		dbType := strings.ToUpper(columnTypes[i].DatabaseTypeName())

		// 处理 []byte 类型
		if b, ok := val.([]byte); ok {
			if shouldPreserveBinary(dbType) {
				copied := make([]byte, len(b))
				copy(copied, b)
				row[col] = copied
				continue
			}
			row[col] = string(b)
			continue
		}

		// 类型转换
		scanType := columnTypes[i].ScanType()
		if scanType != nil {
			switch scanType.Kind() {
			case 2, 3, 4, 5, 6: // int 类型
				if v, ok := val.(int64); ok {
					row[col] = v
				}
			case 13, 14: // float 类型
				if v, ok := val.(float64); ok {
					row[col] = v
				}
			case 1: // bool 类型
				if v, ok := val.(bool); ok {
					row[col] = v
				}
			}
		}

		row[col] = val
	}

	return row, nil
}

// inferSchema 推断数据 schema
func (r *JDBCReader) inferSchema(ctx context.Context) (*pipeline.Schema, error) {
	// 执行 LIMIT 1 查询获取列信息
	query := fmt.Sprintf("%s LIMIT 1", r.query)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	fields := make([]pipeline.Field, 0, len(columnTypes))
	for _, col := range columnTypes {
		field := pipeline.Field{
			Name:     col.Name(),
			Type:     r.mapDatabaseType(col.DatabaseTypeName()),
			Nullable: true,
		}

		if nullable, ok := col.Nullable(); ok {
			field.Nullable = nullable
		}

		fields = append(fields, field)
	}

	schema := &pipeline.Schema{
		Fields: fields,
		Metadata: map[string]interface{}{
			"source_type": "jdbc",
			"driver":      r.driver,
		},
	}

	// 如果有表名，检测主键信息
	if r.table != "" {
		schemaName, tableName := r.parseTableName(r.table)

		var primaryKeys []string
		var err error

		switch r.driver {
		case "postgres", "postgresql":
			primaryKeys, err = r.detectPostgresPrimaryKeyColumns(ctx, schemaName, tableName)
		case "mysql":
			primaryKeys, err = r.detectMySQLPrimaryKeyColumns(ctx, schemaName, tableName)
		}

		if err == nil && len(primaryKeys) > 0 {
			fmt.Printf("📋 JDBC Reader 检测到主键: %v (driver: %s, table: %s)\n", primaryKeys, r.driver, r.table)

			// 填充新格式
			schema.PrimaryKey = &pipeline.PrimaryKey{
				Columns: primaryKeys,
				Name:    "", // JDBC Reader 不提供约束名
			}

			// 同时填充旧格式（向后兼容）
			pkCols := make([]interface{}, len(primaryKeys))
			for i, col := range primaryKeys {
				pkCols[i] = col
			}
			schema.Metadata["table_metadata"] = map[string]interface{}{
				"has_primary_key":  true,
				"primary_key":      pkCols,
				"primary_key_name": "",
			}
		}
	}

	return schema, nil
}

// mapDatabaseType 映射数据库类型到统一类型
func (r *JDBCReader) mapDatabaseType(dbType string) string {
	switch dbType {
	case "VARCHAR", "TEXT", "CHAR", "STRING":
		return "string"
	case "INT", "INTEGER", "BIGINT", "SMALLINT":
		return "int"
	case "FLOAT", "DOUBLE", "DECIMAL", "NUMERIC":
		return "float"
	case "BOOL", "BOOLEAN":
		return "bool"
	case "DATE", "DATETIME", "TIMESTAMP", "TIMESTAMPTZ":
		return "datetime"
	case "JSON", "JSONB":
		return "json"
	// PostGIS 空间类型
	case "GEOMETRY", "GEOGRAPHY":
		return "geometry"
	case "POINT":
		return "geometry"
	case "LINESTRING":
		return "geometry"
	case "POLYGON":
		return "geometry"
	case "MULTIPOINT", "MULTILINESTRING", "MULTIPOLYGON":
		return "geometry"
	case "GEOMETRYCOLLECTION":
		return "geometry"
	// MySQL Spatial 类型
	case "GEOM", "GEOMCOLLECTION":
		return "geometry"
	default:
		return "string"
	}
}

// initializeCursor 初始化游标寻址优化
func (r *JDBCReader) initializeCursor(ctx context.Context, config JDBCConfig) error {
	// 如果用户显式禁用，跳过
	if config.UseCursor == false {
		r.useCursor = false
		return nil
	}

	// 保存基础查询（去掉 LIMIT/OFFSET）
	r.baseQuery = r.query

	// 1. 如果用户指定了游标列，直接使用
	if config.CursorColumn != "" {
		r.cursorColumn = config.CursorColumn
		r.useCursor = true
		fmt.Printf("INFO: Using user-specified cursor column: %s\n", r.cursorColumn)
		return nil
	}

	// 2. 自动检测主键（仅支持表查询，不支持复杂 SQL）
	if config.Table != "" {
		primaryKey, err := r.detectPrimaryKey(ctx, config.Table)
		if err != nil {
			return fmt.Errorf("failed to detect primary key: %w", err)
		}

		if primaryKey != "" {
			r.cursorColumn = primaryKey
			r.useCursor = true
			fmt.Printf("INFO: Auto-detected cursor column (primary key): %s\n", r.cursorColumn)
			return nil
		}
	}

	// 3. 如果有 OrderBy 配置，尝试使用第一个排序字段作为游标
	if config.OrderBy != "" {
		// 解析 OrderBy，提取第一个字段名（去掉 ASC/DESC）
		orderFields := strings.Split(config.OrderBy, ",")
		if len(orderFields) > 0 {
			firstField := strings.TrimSpace(orderFields[0])
			// 去掉 ASC/DESC
			firstField = strings.TrimSuffix(strings.TrimSuffix(firstField, " DESC"), " ASC")
			firstField = strings.TrimSpace(firstField)

			r.cursorColumn = firstField
			r.useCursor = true
			fmt.Printf("INFO: Using OrderBy field as cursor column: %s\n", r.cursorColumn)
			return nil
		}
	}

	// 4. 无法确定游标列，降级到 OFFSET 模式
	r.useCursor = false
	return fmt.Errorf("no suitable cursor column found (no primary key, no OrderBy)")
}

// detectPrimaryKey 检测表的主键
func (r *JDBCReader) detectPrimaryKey(ctx context.Context, tableName string) (string, error) {
	// 解析 schema.table
	schemaName, table := r.parseTableName(tableName)

	var primaryKey string
	var err error

	switch r.driver {
	case "postgres", "postgresql":
		primaryKey, err = r.detectPostgresPrimaryKey(ctx, schemaName, table)
	case "mysql":
		primaryKey, err = r.detectMySQLPrimaryKey(ctx, schemaName, table)
	default:
		return "", fmt.Errorf("unsupported driver for primary key detection: %s", r.driver)
	}

	return primaryKey, err
}

// detectPostgresPrimaryKey 检测 PostgreSQL 表的主键（单列，用于游标优化）
func (r *JDBCReader) detectPostgresPrimaryKey(ctx context.Context, schema, table string) (string, error) {
	keys, err := r.detectPostgresPrimaryKeyColumns(ctx, schema, table)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", nil
	}
	return keys[0], nil // 返回第一列主键用于游标
}

// detectPostgresPrimaryKeyColumns 检测 PostgreSQL 表的所有主键列（支持复合主键）
func (r *JDBCReader) detectPostgresPrimaryKeyColumns(ctx context.Context, schema, table string) ([]string, error) {
	if schema == "" {
		schema = "public"
	}

	query := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = $1::regclass
		  AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)
	`

	qualifiedTable := fmt.Sprintf("%s.%s", schema, table)
	rows, err := r.db.QueryContext(ctx, query, qualifiedTable)
	if err != nil {
		return nil, fmt.Errorf("failed to detect PostgreSQL primary key: %w", err)
	}
	defer rows.Close()

	var primaryKeys []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			return nil, err
		}
		primaryKeys = append(primaryKeys, colName)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return primaryKeys, nil
}

// detectMySQLPrimaryKey 检测 MySQL 表的主键（单列，用于游标优化）
func (r *JDBCReader) detectMySQLPrimaryKey(ctx context.Context, schema, table string) (string, error) {
	keys, err := r.detectMySQLPrimaryKeyColumns(ctx, schema, table)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", nil
	}
	return keys[0], nil // 返回第一列主键用于游标
}

// detectMySQLPrimaryKeyColumns 检测 MySQL 表的所有主键列（支持复合主键）
func (r *JDBCReader) detectMySQLPrimaryKeyColumns(ctx context.Context, schema, table string) ([]string, error) {
	if schema == "" {
		schema = r.database // 使用保存的数据库名
	}

	query := `
		SELECT COLUMN_NAME
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ?
		  AND TABLE_NAME = ?
		  AND COLUMN_KEY = 'PRI'
		ORDER BY ORDINAL_POSITION
	`

	rows, err := r.db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, fmt.Errorf("failed to detect MySQL primary key: %w", err)
	}
	defer rows.Close()

	var primaryKeys []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			return nil, err
		}
		primaryKeys = append(primaryKeys, colName)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return primaryKeys, nil
}

// parseTableName 解析表名为 schema 和 table
func (r *JDBCReader) parseTableName(tableName string) (string, string) {
	parts := strings.Split(tableName, ".")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}

// formatCursorValue 格式化游标值为 SQL 字面量
func (r *JDBCReader) formatCursorValue(value interface{}) string {
	if value == nil {
		return "NULL"
	}

	switch v := value.(type) {
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%f", v)
	case string:
		// 转义单引号
		escaped := strings.ReplaceAll(v, "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	case []byte:
		// 字节数组转为字符串
		escaped := strings.ReplaceAll(string(v), "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	case time.Time:
		// 时间类型格式化为 ISO 8601
		return fmt.Sprintf("'%s'", v.Format("2006-01-02 15:04:05"))
	default:
		// 其他类型转为字符串
		str := fmt.Sprintf("%v", v)
		escaped := strings.ReplaceAll(str, "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	}
}
