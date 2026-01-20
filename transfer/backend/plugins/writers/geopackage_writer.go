package writers

import (
	"github.com/addp/transfer/plugins/utils"
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
)

// GeoPackageWriter GeoPackage 数据写入器
type GeoPackageWriter struct {
	db            *sql.DB
	table         string
	geometryField string
	columns       []string
	buffer        []map[string]interface{}
	batchSize     int
	srid          int
}

// GeoPackageWriterConfig GeoPackage Writer 配置
type GeoPackageWriterConfig struct {
	FilePath      string `json:"file_path"`       // .gpkg 文件路径
	Table         string `json:"table"`           // 表名
	GeometryField string `json:"geometry_field"`  // 几何字段名
	SRID          int    `json:"srid"`            // 空间参考系统 ID
}

// NewGeoPackageWriter 创建 GeoPackage Writer
func NewGeoPackageWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	var writerConfig GeoPackageWriterConfig
	if err := utils.MapToStruct(config.Config, &writerConfig); err != nil {
		return nil, fmt.Errorf("invalid geopackage config: %w", err)
	}

	if writerConfig.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	if writerConfig.Table == "" {
		return nil, fmt.Errorf("table is required")
	}

	if writerConfig.GeometryField == "" {
		writerConfig.GeometryField = "geom"
	}

	if writerConfig.SRID == 0 {
		writerConfig.SRID = 4326 // 默认 WGS84
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	return &GeoPackageWriter{
		table:         writerConfig.Table,
		geometryField: writerConfig.GeometryField,
		buffer:        make([]map[string]interface{}, 0, batchSize),
		batchSize:     batchSize,
		srid:          writerConfig.SRID,
	}, nil
}

// Open 打开 GeoPackage 写入
func (w *GeoPackageWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var writerConfig GeoPackageWriterConfig
	if err := utils.MapToStruct(config.Config, &writerConfig); err != nil {
		return err
	}

	// 打开 SQLite 数据库
	db, err := sql.Open("sqlite3", writerConfig.FilePath)
	if err != nil {
		return fmt.Errorf("failed to open geopackage: %w", err)
	}

	// 启用 SpatiaLite 扩展（如果可用）
	_, _ = db.ExecContext(ctx, "SELECT load_extension('mod_spatialite')")

	w.db = db

	// 如果需要创建表，等待第一批数据
	// 实际表创建在第一次 Write 时进行

	return nil
}

// Write 写入数据批次
func (w *GeoPackageWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if batch.IsEmpty() {
		return nil
	}

	// 第一次写入时初始化列名和创建表
	if w.columns == nil {
		w.columns = make([]string, 0, len(batch.Rows[0]))
		for col := range batch.Rows[0] {
			w.columns = append(w.columns, col)
		}

		// 创建表（如果不存在）
		if err := w.createTableIfNotExists(ctx, batch); err != nil {
			return err
		}
	}

	// 添加到缓冲区
	w.buffer = append(w.buffer, batch.Rows...)

	// 缓冲区满时执行批量写入
	if len(w.buffer) >= w.batchSize {
		return w.flushBuffer(ctx)
	}

	return nil
}

// Flush 刷新缓冲区
func (w *GeoPackageWriter) Flush(ctx context.Context) error {
	if len(w.buffer) > 0 {
		return w.flushBuffer(ctx)
	}
	return nil
}

// Close 关闭连接
func (w *GeoPackageWriter) Close() error {
	// 刷新剩余数据
	if len(w.buffer) > 0 {
		if err := w.flushBuffer(context.Background()); err != nil {
			return err
		}
	}

	if w.db != nil {
		return w.db.Close()
	}
	return nil
}

// createTableIfNotExists 创建表（如果不存在）
func (w *GeoPackageWriter) createTableIfNotExists(ctx context.Context, batch *pipeline.DataBatch) error {
	// 构建 CREATE TABLE 语句
	var columns []string

	for _, col := range w.columns {
		if col == w.geometryField {
			// 几何字段
			columns = append(columns, fmt.Sprintf("%s BLOB", col))
		} else {
			// 普通字段，根据值类型推断
			value := batch.Rows[0][col]
			sqlType := inferSQLiteType(value)
			columns = append(columns, fmt.Sprintf("%s %s", col, sqlType))
		}
	}

	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", w.table, strings.Join(columns, ", "))

	_, err := w.db.ExecContext(ctx, createSQL)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// 注册到 gpkg_contents 表
	contentSQL := `
		INSERT OR IGNORE INTO gpkg_contents (table_name, data_type, identifier, srs_id)
		VALUES (?, 'features', ?, ?)
	`
	_, err = w.db.ExecContext(ctx, contentSQL, w.table, w.table, w.srid)
	if err != nil {
		// 忽略错误，可能是 GeoPackage 元数据表不存在
	}

	return nil
}

// flushBuffer 刷新缓冲区到数据库
func (w *GeoPackageWriter) flushBuffer(ctx context.Context) error {
	if len(w.buffer) == 0 {
		return nil
	}

	// 开启事务
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 构建 INSERT 语句
	placeholders := make([]string, len(w.columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		w.table,
		strings.Join(w.columns, ", "),
		strings.Join(placeholders, ", "),
	)

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// 批量插入
	for _, row := range w.buffer {
		values := make([]interface{}, len(w.columns))
		for i, col := range w.columns {
			values[i] = row[col]
		}

		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 清空缓冲区
	w.buffer = w.buffer[:0]

	return nil
}

// inferSQLiteType 推断 SQLite 类型
func inferSQLiteType(value interface{}) string {
	switch value.(type) {
	case string:
		return "TEXT"
	case int, int32, int64:
		return "INTEGER"
	case float32, float64:
		return "REAL"
	case bool:
		return "INTEGER"
	case []byte:
		return "BLOB"
	default:
		return "TEXT"
	}
}
