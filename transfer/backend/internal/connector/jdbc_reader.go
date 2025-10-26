package connector

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/addp/transfer/pkg/pipeline"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// JDBCReader JDBC 数据读取器
// 支持 PostgreSQL, MySQL 等关系型数据库
type JDBCReader struct {
	db           *sql.DB
	query        string
	batchSize    int
	offset       int64
	rows         *sql.Rows
	columns      []string
	schema       *pipeline.Schema
	mode         pipeline.ReaderMode
	lastReadAt   time.Time
	pollInterval time.Duration
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
}

// NewJDBCReader 创建 JDBC Reader
func NewJDBCReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	var jdbcConfig JDBCConfig
	if err := mapToStruct(config.Config, &jdbcConfig); err != nil {
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
		batchSize = 1000
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
	if err := mapToStruct(config.Config, &jdbcConfig); err != nil {
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

	return nil
}

// Read 读取一批数据
func (r *JDBCReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	// 首次读取，执行查询
	if r.rows == nil {
		query := r.buildPaginatedQuery()
		rows, err := r.db.QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("query failed: %w", err)
		}
		r.rows = rows

		// 获取列名
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		r.columns = columns
	}

	// 读取批次数据
	var batchRows []map[string]interface{}
	for i := 0; i < r.batchSize && r.rows.Next(); i++ {
		row, err := r.scanRow(r.rows)
		if err != nil {
			return nil, err
		}
		batchRows = append(batchRows, row)
		r.offset++
	}

	// 检查是否读完
	if len(batchRows) == 0 {
		r.rows.Close()
		r.rows = nil
		return nil, io.EOF
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
	if r.rows != nil {
		r.rows.Close()
		r.rows = nil
	}
	return nil
}

// Close 关闭连接
func (r *JDBCReader) Close() error {
	if r.rows != nil {
		r.rows.Close()
		r.rows = nil
	}
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
	query := fmt.Sprintf("SELECT * FROM %s", config.Table)

	if config.WhereClause != "" {
		query += " WHERE " + config.WhereClause
	}

	if config.OrderBy != "" {
		query += " ORDER BY " + config.OrderBy
	}

	return query
}

// buildPaginatedQuery 构建分页查询
func (r *JDBCReader) buildPaginatedQuery() string {
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

	return &pipeline.Schema{Fields: fields}, nil
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
