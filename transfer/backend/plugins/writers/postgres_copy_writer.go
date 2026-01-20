package writers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/addp/common/spatial"
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

	// 主键和索引管理（导入后添加）
	pendingPrimaryKey       *primaryKeyInfo
	postImportTasksExecuted bool
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
	SRID             int      `json:"srid"`
	GeometryColumns  []string `json:"geometry_columns"`

	// 写入模式配置
	WriteMode string `json:"write_mode"` // insert, replace（COPY 不支持 upsert）

	// 主键和索引配置
	CreatePrimaryKey   bool     `json:"create_primary_key"`   // 是否创建主键（默认 true）
	CreateSpatialIndex bool     `json:"create_spatial_index"` // 是否创建空间索引（默认 true）
	ForcePrimaryKey    []string `json:"force_primary_key"`    // 强制指定主键字段（覆盖自动检测）
	PrimaryKeyName     string   `json:"primary_key_name"`     // 主键约束名（可选）

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

	// 设置默认值：自动创建表（默认行为），默认也创建主键和空间索引
	// CreatePrimaryKey 默认为 true（除非用户显式设置为 false）
	if val, exists := config.Config["create_primary_key"]; !exists || val == nil {
		cfg.CreatePrimaryKey = true
	}

	// CreateSpatialIndex 同理
	if val, exists := config.Config["create_spatial_index"]; !exists || val == nil {
		cfg.CreateSpatialIndex = true
	}

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
	// 使用 TEXT 格式（BINARY 格式在 PrepareContext 模式下不被 pq 驱动支持）
	copyFormat := "TEXT"

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
	fmt.Printf("🔒 PostgresCOPYWriter Close() called, postImportTasksExecuted=%v\n", w.postImportTasksExecuted)

	// 执行导入后任务（主键、索引等）
	if !w.postImportTasksExecuted && w.db != nil {
		ctx := context.Background()
		if err := w.executePostImportTasks(ctx); err != nil {
			// 导入后任务失败，记录错误但不阻断关闭流程
			fmt.Printf("❌ PostgresCOPYWriter: 导入后任务执行失败: %v\n", err)
		}
	}

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
			// 检测并转换 GPKG WKB 格式为标准 WKB（使用 common/spatial 共享函数）
			standardWKB, err := spatial.ConvertToStandardWKB(v)
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
	// 处理 write_mode: replace - 无论表是否需要创建，都先清空数据
	// 这个操作要在 CreateTable 检查之前，确保 replace 模式始终生效
	if w.config.WriteMode == "replace" && !w.tableEnsured {
		truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", w.qualifiedTableName())
		if _, err := w.db.ExecContext(ctx, truncateSQL); err != nil {
			// 如果表不存在，忽略错误（后续会创建表）
			if !strings.Contains(err.Error(), "does not exist") {
				return fmt.Errorf("failed to truncate table %s: %w", w.table, err)
			}
			fmt.Printf("INFO: Table %s does not exist, will create it\n", w.table)
		} else {
			fmt.Printf("✨ Truncated table %s for replace mode\n", w.table)
		}
	}

	// 自动创建表（默认行为）
	if w.tableEnsured {
		return nil
	}

	fieldMap := make(map[string]pipeline.Field)
	if batch.Schema != nil {
		for _, field := range batch.Schema.Fields {
			fieldMap[field.Name] = field
		}
	}

	// 提取主键信息（用于导入后创建约束）
	var primaryKey *primaryKeyInfo
	fmt.Printf("🔍 PostgresCOPYWriter ensureTable: CreatePrimaryKey=%v\n", w.config.CreatePrimaryKey)
	if w.config.CreatePrimaryKey {
		if len(w.config.ForcePrimaryKey) > 0 {
			primaryKey = &primaryKeyInfo{
				Columns: w.config.ForcePrimaryKey,
				Name:    w.config.PrimaryKeyName,
			}
			fmt.Printf("📋 使用配置中的强制主键: %v (将在导入后创建)\n", primaryKey.Columns)
		} else {
			fmt.Printf("🔎 调用 extractPrimaryKeyFromMetadata...\n")
			primaryKey = w.extractPrimaryKeyFromMetadata(batch)
			if primaryKey != nil {
				fmt.Printf("✅ 检测到主键: %v\n", primaryKey.Columns)
			} else {
				fmt.Printf("⚠️ 未检测到主键\n")
			}
		}
		w.pendingPrimaryKey = primaryKey
		fmt.Printf("💾 设置 pendingPrimaryKey: %+v\n", primaryKey)
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
		// 单精度浮点数 (float32, 4字节)
		return "REAL"
	case "double":
		// 双精度浮点数 (float64, 8字节)
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

// extractPrimaryKeyFromMetadata 从元数据中提取主键信息（支持四级降级）
func (w *PostgresCOPYWriter) extractPrimaryKeyFromMetadata(batch *pipeline.DataBatch) *primaryKeyInfo {
	if batch.Schema == nil {
		return nil
	}

	// Level 0: 从 Schema.PrimaryKey 提取（新格式，最高优先级）
	if batch.Schema.PrimaryKey != nil && len(batch.Schema.PrimaryKey.Columns) > 0 {
		fmt.Printf("📋 PostgresCOPYWriter: 从 Schema.PrimaryKey 提取主键: %v (约束名: %s)\n",
			batch.Schema.PrimaryKey.Columns, batch.Schema.PrimaryKey.Name)
		return &primaryKeyInfo{
			Columns: batch.Schema.PrimaryKey.Columns,
			Name:    batch.Schema.PrimaryKey.Name,
		}
	}

	// 以下是向后兼容逻辑
	if batch.Schema.Metadata == nil {
		return nil
	}

	metadata := batch.Schema.Metadata

	// Level 1: 从 table_metadata 提取（旧格式，向后兼容）
	if tableMeta, ok := metadata["table_metadata"].(map[string]interface{}); ok {
		if hasPK, _ := tableMeta["has_primary_key"].(bool); hasPK {
			columns := []string{}

			// 提取主键列
			if pkCols, ok := tableMeta["primary_key"].([]interface{}); ok {
				for _, col := range pkCols {
					if colName, ok := col.(string); ok {
						columns = append(columns, colName)
					}
				}
			}

			if len(columns) > 0 {
				pkName, _ := tableMeta["primary_key_name"].(string)
				fmt.Printf("📋 PostgresCOPYWriter: 从 table_metadata 提取主键: %v (约束名: %s)\n", columns, pkName)
				return &primaryKeyInfo{
					Columns: columns,
					Name:    pkName,
				}
			}
		}
	}

	// Level 2: 从 fields 推断（降级方案）
	columns := []string{}
	if fields, ok := metadata["fields"].([]interface{}); ok {
		for _, f := range fields {
			if field, ok := f.(map[string]interface{}); ok {
				if isPK, _ := field["is_primary_key"].(bool); isPK {
					if name, ok := field["name"].(string); ok {
						columns = append(columns, name)
					}
				}
			}
		}
	}

	if len(columns) > 0 {
		fmt.Printf("📋 PostgresCOPYWriter: 从 fields 推断主键: %v\n", columns)
		return &primaryKeyInfo{
			Columns: columns,
			Name:    "", // 未记录约束名
		}
	}

	fmt.Printf("⚠️  PostgresCOPYWriter: 未找到主键信息\n")
	return nil
}

// executePostImportTasks 执行导入后的任务（主键、索引等）
func (w *PostgresCOPYWriter) executePostImportTasks(ctx context.Context) error {
	fmt.Printf("🔧 PostgresCOPYWriter: 执行导入后任务...\n")

	// 创建主键约束
	if w.pendingPrimaryKey != nil && len(w.pendingPrimaryKey.Columns) > 0 {
		if err := w.addPrimaryKeyConstraint(ctx, w.pendingPrimaryKey); err != nil {
			fmt.Printf("❌ PostgresCOPYWriter: 主键约束创建失败: %v\n", err)
			return err
		}
	} else {
		fmt.Printf("ℹ️  PostgresCOPYWriter: 无待创建的主键\n")
	}

	w.postImportTasksExecuted = true
	return nil
}

// addPrimaryKeyConstraint 添加主键约束（导入后）
func (w *PostgresCOPYWriter) addPrimaryKeyConstraint(ctx context.Context, pk *primaryKeyInfo) error {
	if pk == nil || len(pk.Columns) == 0 {
		return nil
	}

	// 生成约束名
	_, tableName := splitTableName(w.table)
	constraintName := pk.Name
	if constraintName == "" {
		constraintName = fmt.Sprintf("%s_pkey", tableName)
	}

	// 引用列名
	quotedColumns := w.quoteIdentifiers(pk.Columns)

	// ALTER TABLE ADD PRIMARY KEY
	alterSQL := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY (%s)",
		w.qualifiedTableName(),
		w.quoteIdentifier(constraintName),
		strings.Join(quotedColumns, ", "))

	fmt.Printf("🔑 PostgresCOPYWriter: 添加主键约束: %s\n", constraintName)

	if _, err := w.db.ExecContext(ctx, alterSQL); err != nil {
		// 容错：主键已存在或数据有重复
		if strings.Contains(err.Error(), "already exists") ||
			strings.Contains(err.Error(), "multiple primary keys") {
			fmt.Printf("⚠️  PostgresCOPYWriter: 主键约束已存在: %s\n", constraintName)
			return nil
		} else if strings.Contains(err.Error(), "duplicate") {
			return fmt.Errorf("数据包含重复值，无法创建主键: %w", err)
		}
		return err
	}

	fmt.Printf("✅ PostgresCOPYWriter: 主键约束创建成功: %v\n", pk.Columns)
	return nil
}

