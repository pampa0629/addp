package writers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/utils"
)

// PostgresCOPYWriter 使用 PostgreSQL COPY 协议的高性能写入器
// 相比 INSERT 批量插入,COPY 性能提升 5-10 倍,适合千万级数据导入
type PostgresCOPYWriter struct {
	db              *sql.DB
	table           string
	columns         []string
	buffer          [][]interface{}
	batchSize       int
	config          PostgresCOPYConfig
	geometryColumns map[string]geometryColumnMeta
	schema          *pipeline.Schema
	tableEnsured    bool
	stmt            *sql.Stmt
	txn             *sql.Tx
	mu              sync.Mutex
	totalWritten    int64
}

// PostgresCOPYConfig COPY Writer 配置
type PostgresCOPYConfig struct {
	Host             string   `json:"host"`
	Port             int      `json:"port"`
	Database         string   `json:"database"`
	Username         string   `json:"username"`
	Password         string   `json:"password"`
	Table            string   `json:"table"`
	SSLMode          string   `json:"ssl_mode"`
	ConnectionString string   `json:"connection_string"`
	CreateTable      bool     `json:"create_table"`
	SRID             int      `json:"srid"`
	GeometryColumns  []string `json:"geometry_columns"`

	// 写入模式配置
	WriteMode string `json:"write_mode"` // insert, replace（COPY 不支持 upsert）

	// COPY 特定配置
	MaxConnections   int  `json:"max_connections"`   // 连接池大小（默认 4）
	UseCOPY          bool `json:"use_copy"`          // 是否使用 COPY（默认 true）
	PreallocateTable bool `json:"preallocate_table"` // 预分配表空间（VACUUM ANALYZE）
}

// NewPostgresCOPYWriter 创建 COPY Writer
func NewPostgresCOPYWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	var cfg PostgresCOPYConfig
	if err := utils.MapToStruct(config.Config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid postgres config: %w", err)
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 10000 // COPY 模式使用更大批次
	}

	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = 4
	}

	// 设置默认写入模式为 insert
	if cfg.WriteMode == "" {
		cfg.WriteMode = "insert"
	}

	// COPY 协议不支持 upsert，如果配置了 upsert 则返回错误
	if cfg.WriteMode == "upsert" {
		return nil, fmt.Errorf("PostgresCOPYWriter does not support upsert mode, please use JDBCWriter instead or change write_mode to 'insert' or 'replace'")
	}

	// 验证 WriteMode 有效性
	if cfg.WriteMode != "insert" && cfg.WriteMode != "replace" {
		return nil, fmt.Errorf("invalid write_mode '%s', must be 'insert' or 'replace'", cfg.WriteMode)
	}

	cfg.UseCOPY = true // 强制使用 COPY

	return &PostgresCOPYWriter{
		batchSize: batchSize,
		buffer:    make([][]interface{}, 0, batchSize),
		config:    cfg,
	}, nil
}

// Open 打开数据库连接
func (w *PostgresCOPYWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var cfg PostgresCOPYConfig
	if err := utils.MapToStruct(config.Config, &cfg); err != nil {
		return err
	}

	connStr, err := w.buildConnectionString(cfg)
	if err != nil {
		return err
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// 连接池优化
	db.SetMaxOpenConns(cfg.MaxConnections)
	db.SetMaxIdleConns(cfg.MaxConnections)

	if cfg.Table == "" {
		db.Close()
		return fmt.Errorf("target table cannot be empty")
	}

	w.db = db
	w.table = cfg.Table
	w.config = cfg

	return nil
}

// Write 写入数据批次
func (w *PostgresCOPYWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if batch == nil || batch.IsEmpty() {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// 初始化列信息
	if w.columns == nil {
		if err := w.initializeMetadata(batch); err != nil {
			return err
		}

		if err := w.ensureTable(ctx, batch); err != nil {
			return err
		}

		// 启动 COPY 事务
		if err := w.beginCOPY(ctx); err != nil {
			return err
		}
	}

	// 转换行为 COPY 格式
	for _, row := range batch.Rows {
		values := make([]interface{}, len(w.columns))
		for i, col := range w.columns {
			var original interface{}
			if row != nil {
				original = row[col]
			}
			prepared, err := w.prepareValueForCOPY(col, original)
			if err != nil {
				return err
			}
			values[i] = prepared
		}
		w.buffer = append(w.buffer, values)
	}

	// 达到批次大小时刷新
	if len(w.buffer) >= w.batchSize {
		return w.flushBuffer(ctx)
	}

	return nil
}

// beginCOPY 开启 COPY 事务
func (w *PostgresCOPYWriter) beginCOPY(ctx context.Context) error {
	txn, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	w.txn = txn

	// 准备 COPY 语句
	// 如果有空间字段，使用 TEXT 格式；否则使用 BINARY 格式以获得更好性能
	copyFormat := "BINARY"
	if len(w.geometryColumns) > 0 {
		copyFormat = "TEXT"
		fmt.Printf("INFO: Using TEXT format for COPY due to geometry columns\n")
	}

	copySQL := fmt.Sprintf("COPY %s (%s) FROM STDIN WITH (FORMAT %s)",
		w.qualifiedTableName(),
		strings.Join(w.quoteIdentifiers(w.columns), ", "),
		copyFormat)

	stmt, err := txn.PrepareContext(ctx, copySQL)
	if err != nil {
		txn.Rollback()
		return fmt.Errorf("failed to prepare COPY statement: %w", err)
	}

	w.stmt = stmt
	return nil
}

// flushBuffer 刷新缓冲区到 COPY
func (w *PostgresCOPYWriter) flushBuffer(ctx context.Context) error {
	if len(w.buffer) == 0 {
		return nil
	}

	// 使用 pq.CopyIn 批量插入
	for _, values := range w.buffer {
		if _, err := w.stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("failed to COPY row: %w", err)
		}
		w.totalWritten++
	}

	w.buffer = w.buffer[:0]
	return nil
}

// Flush 最终刷新并提交事务
func (w *PostgresCOPYWriter) Flush(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 刷新剩余缓冲区
	if err := w.flushBuffer(ctx); err != nil {
		if w.txn != nil {
			w.txn.Rollback()
		}
		return err
	}

	// 关闭 COPY 语句
	if w.stmt != nil {
		if _, err := w.stmt.ExecContext(ctx); err != nil {
			w.txn.Rollback()
			return fmt.Errorf("failed to finalize COPY: %w", err)
		}
		w.stmt.Close()
		w.stmt = nil
	}

	// 提交事务
	if w.txn != nil {
		if err := w.txn.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
		w.txn = nil
	}

	// 执行 ANALYZE 以更新统计信息
	if _, err := w.db.ExecContext(ctx, fmt.Sprintf("ANALYZE %s", w.qualifiedTableName())); err != nil {
		// ANALYZE 失败不阻断流程
		fmt.Printf("Warning: ANALYZE failed: %v\n", err)
	}

	return nil
}

// Close 关闭连接
func (w *PostgresCOPYWriter) Close() error {
	if w.stmt != nil {
		w.stmt.Close()
	}
	if w.txn != nil {
		w.txn.Rollback()
	}
	if w.db != nil {
		return w.db.Close()
	}
	return nil
}

// prepareValueForCOPY 为 COPY 准备值
func (w *PostgresCOPYWriter) prepareValueForCOPY(column string, value interface{}) (interface{}, error) {
	meta, isGeometry := w.geometryColumns[column]
	if isGeometry {
		if value == nil {
			return nil, nil
		}

		switch v := value.(type) {
		case []byte:
			// 检测并转换 GPKG WKB 格式为标准 WKB
			standardWKB, err := w.convertToStandardWKB(v)
			if err != nil {
				return nil, fmt.Errorf("failed to convert WKB for column %s: %w", column, err)
			}

			// WKB 格式转换为十六进制字符串（HEXEWKB）
			// PostGIS TEXT 格式的 COPY 支持 HEXEWKB (如: ST_GeomFromEWKB('\x01010000...'::geometry))
			// 添加 SRID 前缀（如果有）
			hexWKB := fmt.Sprintf("%X", standardWKB)
			if meta.SRID > 0 {
				// 为 WKB 添加 SRID 前缀（EWKB 格式）
				// EWKB 格式: SRID=4326;HEXWKB
				return fmt.Sprintf("SRID=%d;%s", meta.SRID, hexWKB), nil
			}
			return hexWKB, nil
		case string:
			// WKT 格式或已经是 HEXEWKB
			// 如果是 WKT 且配置了 SRID，添加 SRID 前缀
			if meta.SRID > 0 && !strings.HasPrefix(v, "SRID=") && !strings.Contains(v, ";") {
				// 可能是纯 WKT，添加 SRID
				return fmt.Sprintf("SRID=%d;%s", meta.SRID, v), nil
			}
			return v, nil
		default:
			return nil, fmt.Errorf("geometry column %s expects []byte or string, got %T", column, value)
		}
	}

	return value, nil
}

// convertToStandardWKB 将 GPKG WKB 转换为标准 ISO WKB
// GPKG WKB 格式: [GP 2字节magic][标志字节][envelope字节][标准WKB]
// 详见: http://www.geopackage.org/spec/#gpb_format
func (w *PostgresCOPYWriter) convertToStandardWKB(data []byte) ([]byte, error) {
	if len(data) < 8 {
		// 太短，可能不是有效的 WKB
		return data, nil
	}

	// 检测 GPKG WKB magic bytes: "GP" (0x47 0x50)
	if data[0] == 0x47 && data[1] == 0x50 {
		// GPKG WKB 格式检测
		// Byte 2: version (0x00)
		// Byte 3: flags (包含 envelope type 和 byte order)
		// Byte 4-7: SRID (little-endian)

		if len(data) < 8 {
			return nil, fmt.Errorf("GPKG WKB too short: %d bytes", len(data))
		}

		flags := data[3]
		envelopeType := (flags >> 1) & 0x07 // bits 1-3

		// 计算 envelope 大小
		envelopeSize := 0
		switch envelopeType {
		case 0: // 无 envelope
			envelopeSize = 0
		case 1: // XY envelope
			envelopeSize = 32 // 4 doubles
		case 2: // XYZ envelope
			envelopeSize = 48 // 6 doubles
		case 3: // XYM envelope
			envelopeSize = 48 // 6 doubles
		case 4: // XYZM envelope
			envelopeSize = 64 // 8 doubles
		default:
			return nil, fmt.Errorf("unknown GPKG envelope type: %d", envelopeType)
		}

		// GPKG header: 8 bytes + envelope
		gpkgHeaderSize := 8 + envelopeSize
		if len(data) < gpkgHeaderSize {
			return nil, fmt.Errorf("GPKG WKB incomplete: expected %d bytes, got %d", gpkgHeaderSize, len(data))
		}

		// 提取标准 WKB (跳过 GPKG header)
		standardWKB := data[gpkgHeaderSize:]

		fmt.Printf("DEBUG: Converted GPKG WKB to standard WKB (header size: %d, total: %d -> %d bytes)\n",
			gpkgHeaderSize, len(data), len(standardWKB))

		return standardWKB, nil
	}

	// 不是 GPKG WKB，直接返回（可能已经是标准 WKB）
	return data, nil
}

// initializeMetadata 初始化元数据
func (w *PostgresCOPYWriter) initializeMetadata(batch *pipeline.DataBatch) error {
	w.schema = batch.Schema

	if len(batch.Rows) == 0 {
		return fmt.Errorf("cannot infer columns from empty batch")
	}

	row := batch.Rows[0]
	if batch.Schema != nil && len(batch.Schema.Fields) > 0 {
		fieldSet := make(map[string]bool)
		w.columns = make([]string, 0, len(row))

		for _, field := range batch.Schema.Fields {
			if _, exists := row[field.Name]; exists {
				w.columns = append(w.columns, field.Name)
				fieldSet[field.Name] = true
			}
		}

		for col := range row {
			if !fieldSet[col] {
				w.columns = append(w.columns, col)
			}
		}
	} else {
		w.columns = make([]string, 0, len(row))
		for col := range row {
			w.columns = append(w.columns, col)
		}
	}

	w.detectGeometryColumns()
	return nil
}

// detectGeometryColumns 检测几何列
func (w *PostgresCOPYWriter) detectGeometryColumns() {
	candidates := make(map[string]geometryColumnMeta)

	// 优先从 Schema 中检测几何字段
	if w.schema != nil {
		for _, field := range w.schema.Fields {
			if strings.EqualFold(field.Type, "geometry") {
				meta := geometryColumnMeta{
					SRID:        field.SRID,
					SpatialType: field.SpatialType,
				}
				if meta.SRID == 0 {
					meta.SRID = w.config.SRID
				}
				candidates[field.Name] = meta
				fmt.Printf("DEBUG: Detected geometry field from schema: %s (type=%s, srid=%d)\n",
					field.Name, field.SpatialType, meta.SRID)
			}
		}
	}

	// 从配置中补充几何字段信息
	if len(w.config.GeometryColumns) > 0 {
		for _, name := range w.config.GeometryColumns {
			if _, exists := candidates[name]; !exists {
				candidates[name] = geometryColumnMeta{
					SRID: w.config.SRID,
				}
				fmt.Printf("DEBUG: Added geometry field from config: %s (srid=%d)\n", name, w.config.SRID)
			}
		}
	}

	if len(candidates) == 0 {
		fmt.Printf("DEBUG: No geometry columns detected (schema=%v, config_geom_cols=%v)\n",
			w.schema != nil, len(w.config.GeometryColumns))
		return
	}

	w.geometryColumns = make(map[string]geometryColumnMeta)
	for _, col := range w.columns {
		for candidate, meta := range candidates {
			if strings.EqualFold(candidate, col) {
				w.geometryColumns[col] = meta
				fmt.Printf("DEBUG: Matched geometry column: %s -> type=%s, srid=%d\n",
					col, meta.SpatialType, meta.SRID)
				break
			}
		}
	}
	fmt.Printf("DEBUG: Final geometry columns: %d detected\n", len(w.geometryColumns))
}

// ensureTable 确保表存在
func (w *PostgresCOPYWriter) ensureTable(ctx context.Context, batch *pipeline.DataBatch) error {
	if !w.config.CreateTable || w.tableEnsured {
		return nil
	}

	fieldMap := make(map[string]pipeline.Field)
	if batch.Schema != nil {
		for _, field := range batch.Schema.Fields {
			fieldMap[field.Name] = field
		}
	}

	columnDefs := make([]string, 0, len(w.columns))
	for _, col := range w.columns {
		field := fieldMap[col]
		meta, hasGeom := w.geometryColumns[col]
		sqlType := w.mapFieldToSQLType(field, hasGeom, meta)
		if sqlType == "" {
			sqlType = "TEXT"
		}

		definition := fmt.Sprintf("%s %s", w.quoteIdentifier(col), sqlType)
		if field.Name != "" && !field.Nullable {
			definition += " NOT NULL"
		}

		columnDefs = append(columnDefs, definition)
	}

	if len(columnDefs) == 0 {
		return fmt.Errorf("no columns available to create table %s", w.table)
	}

	schemaName, _ := splitTableName(w.table)
	if schemaName != "" {
		schemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", w.quoteIdentifier(schemaName))
		if _, err := w.db.ExecContext(ctx, schemaSQL); err != nil {
			return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
		}
	}

	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)",
		w.qualifiedTableName(), strings.Join(columnDefs, ", "))
	if _, err := w.db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create table %s: %w", w.table, err)
	}

	// 处理 write_mode: replace - 清空已存在的数据
	if w.config.WriteMode == "replace" {
		truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", w.qualifiedTableName())
		if _, err := w.db.ExecContext(ctx, truncateSQL); err != nil {
			return fmt.Errorf("failed to truncate table %s: %w", w.table, err)
		}
		fmt.Printf("INFO: Truncated table %s for replace mode\n", w.table)
	}

	// 禁用自动 VACUUM（导入时关闭以提升性能）
	if _, err := w.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s SET (autovacuum_enabled = false)", w.qualifiedTableName())); err != nil {
		fmt.Printf("Warning: failed to disable autovacuum: %v\n", err)
	}

	w.tableEnsured = true
	return nil
}

func (w *PostgresCOPYWriter) mapFieldToSQLType(field pipeline.Field, hasGeometry bool, meta geometryColumnMeta) string {
	if hasGeometry {
		spatialType := strings.ToUpper(meta.SpatialType)
		switch {
		case spatialType != "" && meta.SRID > 0:
			return fmt.Sprintf("geometry(%s, %d)", spatialType, meta.SRID)
		case spatialType != "":
			return fmt.Sprintf("geometry(%s)", spatialType)
		case meta.SRID > 0:
			return fmt.Sprintf("geometry(Geometry, %d)", meta.SRID)
		default:
			return "geometry"
		}
	}

	switch strings.ToLower(field.Type) {
	case "int":
		return "BIGINT"
	case "float":
		return "DOUBLE PRECISION"
	case "bool":
		return "BOOLEAN"
	case "datetime":
		return "TIMESTAMPTZ"
	case "json":
		return "JSONB"
	case "binary":
		return "BYTEA"
	case "":
		return ""
	default:
		return "TEXT"
	}
}

func (w *PostgresCOPYWriter) quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func (w *PostgresCOPYWriter) quoteIdentifiers(ids []string) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = w.quoteIdentifier(id)
	}
	return result
}

func (w *PostgresCOPYWriter) qualifiedTableName() string {
	schemaName, tableName := splitTableName(w.table)
	if schemaName == "" {
		return w.quoteIdentifier(tableName)
	}
	return fmt.Sprintf("%s.%s", w.quoteIdentifier(schemaName), w.quoteIdentifier(tableName))
}

func (w *PostgresCOPYWriter) buildConnectionString(config PostgresCOPYConfig) (string, error) {
	if config.ConnectionString != "" {
		return config.ConnectionString, nil
	}

	sslMode := config.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.Username, config.Password, config.Database, sslMode), nil
}
