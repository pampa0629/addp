package readers

import (
	"github.com/addp/transfer/plugins/utils"
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/addp/transfer/pkg/pipeline"
	_ "github.com/mattn/go-sqlite3"
)

// GeoPackageReader GeoPackage 数据读取器
// GeoPackage 基于 SQLite + 空间扩展
type GeoPackageReader struct {
	db        *sql.DB
	query     string
	table     string
	batchSize int
	offset    int64
	rows      *sql.Rows
	columns   []string
	schema    *pipeline.Schema
	mode      pipeline.ReaderMode
}

// GeoPackageConfig GeoPackage 配置
type GeoPackageConfig struct {
	FilePath      string `json:"file_path"`       // .gpkg 文件路径
	Table         string `json:"table"`           // 表名
	Query         string `json:"query"`           // SQL 查询（可选）
	WhereClause   string `json:"where_clause"`    // WHERE 条件
	GeometryField string `json:"geometry_field"`  // 几何字段名（默认 "geom"）
}

// NewGeoPackageReader 创建 GeoPackage Reader
func NewGeoPackageReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	var gpkgConfig GeoPackageConfig
	if err := utils.MapToStruct(config.Config, &gpkgConfig); err != nil {
		return nil, fmt.Errorf("invalid geopackage config: %w", err)
	}

	if gpkgConfig.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	return &GeoPackageReader{
		batchSize: batchSize,
		table:     gpkgConfig.Table,
		query:     gpkgConfig.Query,
		mode:      pipeline.ModeBatch,
	}, nil
}

// Open 打开 GeoPackage
func (r *GeoPackageReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var gpkgConfig GeoPackageConfig
	if err := utils.MapToStruct(config.Config, &gpkgConfig); err != nil {
		return err
	}

	// 打开 SQLite 数据库
	db, err := sql.Open("sqlite3", gpkgConfig.FilePath)
	if err != nil {
		return fmt.Errorf("failed to open geopackage: %w", err)
	}

	// 测试连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping geopackage: %w", err)
	}

	r.db = db

	// 构建查询语句
	if gpkgConfig.Query != "" {
		r.query = gpkgConfig.Query
	} else if gpkgConfig.Table != "" {
		r.query = r.buildSelectQuery(gpkgConfig)
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
func (r *GeoPackageReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
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

	return &pipeline.DataBatch{
		Rows:      batchRows,
		Schema:    r.schema,
		Offset:    r.offset,
		Timestamp: time.Now(),
	}, nil
}

// Schema 返回数据 schema
func (r *GeoPackageReader) Schema() (*pipeline.Schema, error) {
	if r.schema == nil {
		return nil, fmt.Errorf("schema not initialized, call Open first")
	}
	return r.schema, nil
}

// SeekTo 跳转到指定偏移量
func (r *GeoPackageReader) SeekTo(offset int64) error {
	r.offset = offset
	if r.rows != nil {
		r.rows.Close()
		r.rows = nil
	}
	return nil
}

// Close 关闭连接
func (r *GeoPackageReader) Close() error {
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
func (r *GeoPackageReader) Mode() pipeline.ReaderMode {
	return r.mode
}

// buildSelectQuery 构建 SELECT 查询
func (r *GeoPackageReader) buildSelectQuery(config GeoPackageConfig) string {
	query := fmt.Sprintf("SELECT * FROM %s", config.Table)

	if config.WhereClause != "" {
		query += " WHERE " + config.WhereClause
	}

	return query
}

// buildPaginatedQuery 构建分页查询
func (r *GeoPackageReader) buildPaginatedQuery() string {
	return fmt.Sprintf("%s LIMIT %d OFFSET %d", r.query, r.batchSize, r.offset)
}

// scanRow 扫描行数据
func (r *GeoPackageReader) scanRow(rows *sql.Rows) (map[string]interface{}, error) {
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

		// 处理 []byte 类型（几何数据通常是 BLOB）
		if b, ok := val.([]byte); ok {
			// GeoPackage 使用 WKB 格式存储几何
			row[col] = b
		} else if val == nil {
			row[col] = nil
		} else {
			row[col] = val
		}
	}

	return row, nil
}

// inferSchema 推断数据 schema
func (r *GeoPackageReader) inferSchema(ctx context.Context) (*pipeline.Schema, error) {
	// 查询表结构
	query := fmt.Sprintf("PRAGMA table_info(%s)", r.table)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := make([]pipeline.Field, 0)

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var dfltValue interface{}
		var pk int

		if err := rows.Scan(&cid, &name, &dataType, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}

		field := pipeline.Field{
			Name:     name,
			Type:     mapSQLiteType(dataType),
			Nullable: notNull == 0,
		}

		// 检查是否是几何字段
		if dataType == "BLOB" || dataType == "GEOMETRY" {
			field.Type = "geometry"
			field.SpatialType = r.inferGeometryType(ctx, name)
		}

		fields = append(fields, field)
	}

	return &pipeline.Schema{
		Fields: fields,
		Metadata: map[string]interface{}{
			"source_type": "geopackage",
			"table":       r.table,
		},
	}, nil
}

// inferGeometryType 推断几何类型
func (r *GeoPackageReader) inferGeometryType(ctx context.Context, columnName string) string {
	// 查询 gpkg_geometry_columns 表获取几何类型
	query := `SELECT geometry_type_name FROM gpkg_geometry_columns WHERE column_name = ?`
	var geomType string
	err := r.db.QueryRowContext(ctx, query, columnName).Scan(&geomType)
	if err != nil {
		return "Geometry" // 默认类型
	}
	return geomType
}

// mapSQLiteType 映射 SQLite 类型到统一类型
func mapSQLiteType(sqliteType string) string {
	switch sqliteType {
	case "TEXT":
		return "string"
	case "INTEGER":
		return "int"
	case "REAL", "NUMERIC":
		return "float"
	case "BLOB":
		return "binary"
	case "GEOMETRY":
		return "geometry"
	default:
		return "string"
	}
}
