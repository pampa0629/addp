package writers

import (
	"github.com/addp/common/spatial"
	"github.com/addp/transfer/plugins/utils"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/twpayne/go-geom/encoding/wkb"
	"github.com/twpayne/go-geom/encoding/wkt"
)

type geometryColumnMeta struct {
	SRID        int
	SpatialType string
}

// primaryKeyInfo 主键信息
type primaryKeyInfo struct {
	Columns []string  // 主键字段列表
	Name    string    // 主键约束名
}

// spatialIndexInfo 空间索引信息
type spatialIndexInfo struct {
	ColumnName string
	IndexName  string
	SRID       int
}


// JDBCWriter JDBC 数据写入器
type JDBCWriter struct {
	db              *sql.DB
	table           string
	columns         []string
	buffer          []map[string]interface{}
	batchSize       int
	config          JDBCWriterConfig
	driver          string
	geometryColumns map[string]geometryColumnMeta
	schema          *pipeline.Schema
	tableEnsured    bool

	// 导入后优化任务相关
	postImportTasksExecuted bool              // 是否已执行导入后优化任务（主键、索引、ANALYZE）
	pendingPrimaryKey       *primaryKeyInfo   // 待添加的主键信息
}

// NewJDBCWriter 创建 JDBC Writer
func NewJDBCWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	var jdbcConfig JDBCWriterConfig
	if err := utils.MapToStruct(config.Config, &jdbcConfig); err != nil {
		return nil, fmt.Errorf("invalid jdbc config: %w", err)
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 10000 // 提高默认批次大小: 1000 → 5000 → 10000（性能优化）
	}

	return &JDBCWriter{
		batchSize: batchSize,
		buffer:    make([]map[string]interface{}, 0, batchSize),
		config:    jdbcConfig,
	}, nil
}

// JDBCWriterConfig JDBC Writer 配置
type JDBCWriterConfig struct {
	Driver           string   `json:"driver"`
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

	// 主键配置
	CreatePrimaryKey    bool     `json:"create_primary_key"`     // 是否创建主键，默认 true
	PrimaryKeyName      string   `json:"primary_key_name"`       // 主键约束名（可选，覆盖元数据）
	ForcePrimaryKey     []string `json:"force_primary_key"`      // 强制指定主键字段（覆盖元数据）

	// 空间索引配置
	CreateSpatialIndex  bool     `json:"create_spatial_index"`   // 是否创建空间索引，默认 true
	SpatialIndexName    string   `json:"spatial_index_name"`     // 空间索引名（可选）
}

// Open 打开数据库连接
func (w *JDBCWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var writerConfig JDBCWriterConfig
	if err := utils.MapToStruct(config.Config, &writerConfig); err != nil {
		return err
	}

	if writerConfig.Driver == "" {
		if config.Type != "" {
			writerConfig.Driver = config.Type
		} else {
			writerConfig.Driver = "postgresql"
		}
	}

	// 设置默认值：自动创建表（默认行为），默认也创建主键和空间索引
	// CreatePrimaryKey 默认为 true（除非用户显式设置为 false）
	if val, exists := config.Config["create_primary_key"]; !exists || val == nil {
		writerConfig.CreatePrimaryKey = true
	}

	// CreateSpatialIndex 同理
	if val, exists := config.Config["create_spatial_index"]; !exists || val == nil {
		writerConfig.CreateSpatialIndex = true
	}

	connStr, err := w.buildConnectionString(writerConfig)
	if err != nil {
		return err
	}

	driverName := w.normalizeDriverName(writerConfig.Driver)
	db, err := sql.Open(driverName, connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 优化连接池配置（性能优化）
	db.SetMaxOpenConns(20)       // 最大打开连接数（支持并行写入）
	db.SetMaxIdleConns(10)       // 最大空闲连接数
	db.SetConnMaxLifetime(0)     // 连接最大生命周期（0=无限制）
	db.SetConnMaxIdleTime(0)     // 连接最大空闲时间（0=无限制）

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	if writerConfig.Table == "" {
		db.Close()
		return fmt.Errorf("target table cannot be empty")
	}

	w.db = db
	w.table = writerConfig.Table
	w.driver = driverName
	w.config = writerConfig

	return nil
}

// Write 写入数据批次
func (w *JDBCWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if batch == nil || batch.IsEmpty() {
		return nil
	}

	// 初始化列信息
	if w.columns == nil {
		if err := w.initializeMetadata(batch); err != nil {
			return err
		}

		if err := w.ensureTable(ctx, batch); err != nil {
			return err
		}
	}

	w.buffer = append(w.buffer, batch.Rows...)

	if len(w.buffer) >= w.batchSize {
		return w.flushBuffer(ctx)
	}

	return nil
}

// Flush 刷新缓冲区
func (w *JDBCWriter) Flush(ctx context.Context) error {
	if len(w.buffer) == 0 {
		return nil
	}
	return w.flushBuffer(ctx)
}

// Close 关闭连接并执行导入后任务（主键、空间索引、ANALYZE）
func (w *JDBCWriter) Close() error {
	if f, err := os.OpenFile("/tmp/jdbc_writer_debug.log", os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintf(f, "[%s] Close() called, table=%s, postImportTasksExecuted=%v, pendingPrimaryKey=%+v\n",
			time.Now().Format("2006-01-02 15:04:05"), w.table, w.postImportTasksExecuted, w.pendingPrimaryKey)
		f.Close()
	}

	fmt.Printf("🚪 JDBC Writer Close() 被调用 (table: %s, postImportTasksExecuted: %v)\n", w.table, w.postImportTasksExecuted)

	// 执行导入后任务
	if !w.postImportTasksExecuted {
		fmt.Printf("🔄 准备执行导入后任务...\n")

		if f, err := os.OpenFile("/tmp/jdbc_writer_debug.log", os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			fmt.Fprintf(f, "[%s] Executing post-import tasks\n", time.Now().Format("2006-01-02 15:04:05"))
			f.Close()
		}

		w.executePostImportTasks(context.Background())
	} else {
		fmt.Printf("⏭️  导入后任务已执行，跳过\n")
	}

	// 关闭数据库连接
	if w.db != nil {
		fmt.Printf("🔌 关闭数据库连接\n")
		return w.db.Close()
	}
	return nil
}

func (w *JDBCWriter) flushBuffer(ctx context.Context) error {
	if len(w.buffer) == 0 {
		return nil
	}

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// PostgreSQL 参数限制：最多 65535 个参数
	// 计算每次可以插入的最大行数
	const maxParams = 65535
	maxRowsPerInsert := maxParams / len(w.columns)
	if maxRowsPerInsert == 0 {
		maxRowsPerInsert = 1
	}

	// 如果字段数不多，可以一次性插入所有数据
	if len(w.buffer) <= maxRowsPerInsert {
		insertSQL := w.buildBatchInsertSQL(len(w.buffer))
		allValues := make([]interface{}, 0, len(w.buffer)*len(w.columns))
		for _, row := range w.buffer {
			for _, col := range w.columns {
				var original interface{}
				if row != nil {
					original = row[col]
				}
				prepared, err := w.prepareValue(col, original)
				if err != nil {
					return err
				}
				allValues = append(allValues, prepared)
			}
		}

		if _, err := tx.ExecContext(ctx, insertSQL, allValues...); err != nil {
			// 批量插入失败，尝试逐条插入以定位问题记录
			return w.insertRowByRow(ctx, tx, w.buffer, err)
		}
	} else {
		// 分批插入，避免超过参数限制
		for i := 0; i < len(w.buffer); i += maxRowsPerInsert {
			end := i + maxRowsPerInsert
			if end > len(w.buffer) {
				end = len(w.buffer)
			}
			batchRows := w.buffer[i:end]
			rowCount := len(batchRows)

			insertSQL := w.buildBatchInsertSQL(rowCount)
			values := make([]interface{}, 0, rowCount*len(w.columns))
			for _, row := range batchRows {
				for _, col := range w.columns {
					var original interface{}
					if row != nil {
						original = row[col]
					}
					prepared, err := w.prepareValue(col, original)
					if err != nil {
						return err
					}
					values = append(values, prepared)
				}
			}

			if _, err := tx.ExecContext(ctx, insertSQL, values...); err != nil {
				// 分批插入失败，尝试逐条插入以定位问题记录
				log.Printf("⚠️  分批插入失败 (chunk %d-%d)，尝试逐条插入...\n", i, end)
				return w.insertRowByRow(ctx, tx, batchRows, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	w.buffer = w.buffer[:0]
	return nil
}

func (w *JDBCWriter) initializeMetadata(batch *pipeline.DataBatch) error {
	w.schema = batch.Schema

	log.Printf("DEBUG: initializeMetadata - schema非空: %v\n", w.schema != nil)
	if w.schema != nil {
		log.Printf("DEBUG: schema.Fields数量: %d\n", len(w.schema.Fields))
		for _, field := range w.schema.Fields {
			log.Printf("DEBUG: 字段 %s: Type=%s, SRID=%d, SpatialType=%s\n",
				field.Name, field.Type, field.SRID, field.SpatialType)
		}
	}

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

	log.Printf("DEBUG: 列名列表: %v\n", w.columns)
	w.detectGeometryColumns()
	log.Printf("DEBUG: geometryColumns数量: %d\n", len(w.geometryColumns))
	return nil
}

func (w *JDBCWriter) detectGeometryColumns() {
	candidates := make(map[string]geometryColumnMeta)

	if w.schema != nil {
		for _, field := range w.schema.Fields {
			if strings.EqualFold(field.Type, "geometry") {
				meta := geometryColumnMeta{
					SRID:        field.SRID,
					SpatialType: field.SpatialType,
				}
				log.Printf("DEBUG: 检测到几何字段 %s: SRID=%d (schema), SpatialType=%s\n",
					field.Name, field.SRID, field.SpatialType)
				if meta.SRID == 0 {
					meta.SRID = w.config.SRID
					log.Printf("DEBUG: SRID为0，使用配置中的SRID=%d\n", w.config.SRID)
				}
				candidates[field.Name] = meta
			}
		}
	}

	if len(candidates) == 0 && len(w.config.GeometryColumns) > 0 {
		for _, name := range w.config.GeometryColumns {
			candidates[name] = geometryColumnMeta{
				SRID: w.config.SRID,
			}
		}
	}

	if len(candidates) == 0 {
		return
	}

	w.geometryColumns = make(map[string]geometryColumnMeta)
	for _, col := range w.columns {
		for candidate, meta := range candidates {
			if strings.EqualFold(candidate, col) {
				w.geometryColumns[col] = meta
				log.Printf("DEBUG: 设置几何列 %s: SRID=%d, SpatialType=%s\n",
					col, meta.SRID, meta.SpatialType)
				break
			}
		}
	}
}

func (w *JDBCWriter) ensureTable(ctx context.Context, batch *pipeline.DataBatch) error {
	// 自动创建表（默认行为），仅支持 PostgreSQL
	if w.tableEnsured || !w.isPostgres() {
		return nil
	}

	// 覆盖模式：删除已存在的表，确保表结构与字段映射一致
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", w.qualifiedTableName())
	if _, err := w.db.ExecContext(ctx, dropSQL); err != nil {
		return fmt.Errorf("failed to drop table %s: %w", w.table, err)
	}
	fmt.Printf("✨ Dropped table %s (覆盖模式)\n", w.table)

	// 提取主键信息（用于后续创建和标记 NOT NULL）
	var primaryKey *primaryKeyInfo
	fmt.Printf("🔍 ensureTable: CreatePrimaryKey=%v\n", w.config.CreatePrimaryKey)
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
			if primaryKey != nil && w.config.PrimaryKeyName != "" {
				primaryKey.Name = w.config.PrimaryKeyName
			}
			if primaryKey != nil {
				fmt.Printf("📋 从元数据提取主键: %v (将在导入后创建)\n", primaryKey.Columns)
			} else {
				fmt.Printf("⚠️  extractPrimaryKeyFromMetadata 返回 nil\n")
			}
		}

		// 保存主键信息供 Close() 使用
		w.pendingPrimaryKey = primaryKey
		fmt.Printf("💾 设置 pendingPrimaryKey: %+v\n", primaryKey)
	} else {
		fmt.Printf("⏭️  CreatePrimaryKey=false, 跳过主键提取\n")
	}

	// 构建字段映射
	fieldMap := make(map[string]pipeline.Field)
	if batch.Schema != nil {
		for _, field := range batch.Schema.Fields {
			fieldMap[field.Name] = field
		}
	}

	// 构建列定义（不含主键约束）
	columnDefs := make([]string, 0, len(w.columns))
	for _, col := range w.columns {
		field := fieldMap[col]
		meta, hasGeom := w.geometryColumns[col]
		sqlType := w.mapFieldToSQLType(field, hasGeom, meta)
		if sqlType == "" {
			sqlType = "TEXT"
		}

		// 检查是否为主键字段
		isPrimaryKeyColumn := false
		if primaryKey != nil {
			for _, pkCol := range primaryKey.Columns {
				if strings.EqualFold(pkCol, col) {
					isPrimaryKeyColumn = true
					break
				}
			}
		}

		definition := fmt.Sprintf("%s %s", w.quoteIdentifier(col), sqlType)

		// NOT NULL 约束（主键字段必须 NOT NULL）
		if field.Name != "" && !field.Nullable {
			definition += " NOT NULL"
		} else if isPrimaryKeyColumn {
			definition += " NOT NULL"
		}

		columnDefs = append(columnDefs, definition)
	}

	if len(columnDefs) == 0 {
		return fmt.Errorf("no columns available to create table %s", w.table)
	}

	// 创建 Schema
	schemaName, _ := splitTableName(w.table)
	if schemaName != "" {
		schemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", w.quoteIdentifier(schemaName))
		if _, err := w.db.ExecContext(ctx, schemaSQL); err != nil {
			return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
		}
	}

	// 创建表（不含主键约束，性能最优）
	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)",
		w.qualifiedTableName(), strings.Join(columnDefs, ", "))

	if _, err := w.db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create table %s: %w", w.table, err)
	}

	log.Printf("✅ 表结构创建成功: %s (主键和空间索引将在导入后添加)\n", w.table)

	w.tableEnsured = true
	return nil
}

func (w *JDBCWriter) buildInsertSQL() string {
	if w.isPostgres() {
		return w.buildPostgresInsert()
	}
	return w.buildGenericInsert()
}

func (w *JDBCWriter) buildPostgresInsert() string {
	columns := w.quoteIdentifiers(w.columns)
	placeholders := make([]string, len(w.columns))

	for i, col := range w.columns {
		placeholder := w.parameterPlaceholder(i)
		if meta, ok := w.geometryColumns[col]; ok {
			// 使用 ST_GeomFromWKB 配合 ST_SetSRID 处理标准 WKB 格式
			// SpatiaLite ST_AsBinary() 返回标准 WKB (不含 SRID),需要手动设置 SRID
			if meta.SRID > 0 {
				placeholders[i] = fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE ST_SetSRID(ST_GeomFromWKB(%s), %d) END",
					placeholder, placeholder, meta.SRID)
			} else {
				placeholders[i] = fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE ST_GeomFromWKB(%s) END",
					placeholder, placeholder)
			}
		} else {
			placeholders[i] = placeholder
		}
	}

	columnsStr := strings.Join(columns, ", ")
	valuesStr := strings.Join(placeholders, ", ")

	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		w.qualifiedTableName(), columnsStr, valuesStr)
}

func (w *JDBCWriter) buildGenericInsert() string {
	columns := strings.Join(w.columns, ", ")
	placeholders := make([]string, len(w.columns))
	for i := range placeholders {
		placeholders[i] = w.parameterPlaceholder(i)
	}
	valuesStr := strings.Join(placeholders, ", ")

	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		w.table, columns, valuesStr)
}

func (w *JDBCWriter) parameterPlaceholder(index int) string {
	if w.isPostgres() {
		return fmt.Sprintf("$%d", index+1)
	}
	return "?"
}

// buildBatchInsertSQL 构建批量插入 SQL（性能优化）
// INSERT INTO table (col1, col2) VALUES ($1, $2), ($3, $4), ...
func (w *JDBCWriter) buildBatchInsertSQL(rowCount int) string {
	if w.isPostgres() {
		return w.buildPostgresBatchInsert(rowCount)
	}
	return w.buildGenericBatchInsert(rowCount)
}

func (w *JDBCWriter) buildPostgresBatchInsert(rowCount int) string {
	columns := w.quoteIdentifiers(w.columns)

	// 构建多行 VALUES 子句
	valuesClauses := make([]string, rowCount)
	paramIndex := 0

	for row := 0; row < rowCount; row++ {
		placeholders := make([]string, len(w.columns))
		for i, col := range w.columns {
			placeholder := fmt.Sprintf("$%d", paramIndex+1)
			paramIndex++

			if meta, ok := w.geometryColumns[col]; ok {
				if meta.SRID > 0 {
					placeholders[i] = fmt.Sprintf("ST_GeomFromWKB(%s, %d)", placeholder, meta.SRID)
				} else {
					placeholders[i] = fmt.Sprintf("ST_GeomFromWKB(%s)", placeholder)
				}
			} else {
				placeholders[i] = placeholder
			}
		}
		valuesClauses[row] = fmt.Sprintf("(%s)", strings.Join(placeholders, ", "))
	}

	columnsStr := strings.Join(columns, ", ")
	valuesStr := strings.Join(valuesClauses, ", ")

	return fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
		w.qualifiedTableName(), columnsStr, valuesStr)
}

func (w *JDBCWriter) buildGenericBatchInsert(rowCount int) string {
	columns := strings.Join(w.columns, ", ")

	// 构建多行 VALUES 子句
	valuesClauses := make([]string, rowCount)
	for row := 0; row < rowCount; row++ {
		placeholders := make([]string, len(w.columns))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		valuesClauses[row] = fmt.Sprintf("(%s)", strings.Join(placeholders, ", "))
	}

	valuesStr := strings.Join(valuesClauses, ", ")

	return fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
		w.table, columns, valuesStr)
}


func (w *JDBCWriter) prepareValue(column string, value interface{}) (interface{}, error) {
	if _, ok := w.geometryColumns[column]; ok {
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

			// 修复只有3个点的环（SpatiaLite允许3点环，但PostGIS要求至少4点）
			fixedWKB, err := spatial.FixInvalidRings(standardWKB)
			if err != nil {
				return nil, fmt.Errorf("failed to fix invalid rings for column %s: %w", column, err)
			}

			return fixedWKB, nil
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				return nil, nil
			}

			// 尝试解析为十六进制 WKB
			if len(trimmed)%2 == 0 {
				if data, err := hex.DecodeString(trimmed); err == nil {
					return data, nil
				}
			}

			geometry, err := wkt.Unmarshal(trimmed)
			if err != nil {
				return nil, fmt.Errorf("geometry column %s expects WKB bytes or WKT string: %w", column, err)
			}

			data, err := wkb.Marshal(geometry, wkb.NDR)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal geometry for column %s: %w", column, err)
			}
			return data, nil
		default:
			return nil, fmt.Errorf("geometry column %s expects []byte or string, got %T", column, value)
		}
	}

	// 清理字符串中的无效字符（PostgreSQL UTF8 编码要求严格）
	if str, ok := value.(string); ok {
		return cleanStringForPostgres(str), nil
	}

	// 处理 []byte 类型的文本数据（SQLite 有时会返回 []byte 而不是 string）
	// 注意：几何列已在上面处理，这里只处理文本类型的 []byte
	if b, ok := value.([]byte); ok {
		str := string(b)
		return cleanStringForPostgres(str), nil
	}

	return value, nil
}

// cleanStringForPostgres 清理字符串使其符合 PostgreSQL UTF8 编码要求
func cleanStringForPostgres(s string) string {
	// 移除 null 字节
	s = strings.ReplaceAll(s, "\x00", "")

	// 移除所有无效的 UTF8 字符
	if !utf8.ValidString(s) {
		// 逐个字符检查，只保留有效的 UTF8 字符
		var buf strings.Builder
		buf.Grow(len(s))
		for _, r := range s {
			if r != utf8.RuneError {
				buf.WriteRune(r)
			}
		}
		return buf.String()
	}

	return s
}

func (w *JDBCWriter) mapFieldToSQLType(field pipeline.Field, hasGeometry bool, meta geometryColumnMeta) string {
	if hasGeometry && w.isPostgres() {
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
		if w.isPostgres() {
			return "REAL"
		}
		return "FLOAT"
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

func (w *JDBCWriter) quoteIdentifier(identifier string) string {
	if w.isPostgres() {
		return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func (w *JDBCWriter) quoteIdentifiers(ids []string) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = w.quoteIdentifier(id)
	}
	return result
}

func (w *JDBCWriter) qualifiedTableName() string {
	schemaName, tableName := splitTableName(w.table)
	if schemaName == "" {
		return w.quoteIdentifier(tableName)
	}
	return fmt.Sprintf("%s.%s", w.quoteIdentifier(schemaName), w.quoteIdentifier(tableName))
}

func splitTableName(name string) (string, string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ""
	}

	parts := strings.Split(trimmed, ".")
	switch len(parts) {
	case 1:
		return "", strings.Trim(parts[0], `"`)
	default:
		return strings.Trim(parts[0], `"`), strings.Trim(parts[1], `"`)
	}
}

func (w *JDBCWriter) isPostgres() bool {
	return w.driver == "postgres"
}

func (w *JDBCWriter) normalizeDriverName(driver string) string {
	switch driver {
	case "", "postgresql":
		return "postgres"
	default:
		return driver
	}
}

func (w *JDBCWriter) buildConnectionString(config JDBCWriterConfig) (string, error) {
	if config.ConnectionString != "" {
		return config.ConnectionString, nil
	}

	switch config.Driver {
	case "postgres", "postgresql", "":
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

// insertRowByRow 批量插入失败时，逐条插入以定位问题记录
func (w *JDBCWriter) insertRowByRow(ctx context.Context, tx *sql.Tx, rows []map[string]interface{}, batchErr error) error {
	log.Printf("⚠️  批量插入失败，开始逐条插入以定位问题记录...\n")
	log.Printf("原始错误: %v\n", batchErr)
	log.Printf("待插入记录数: %d\n\n", len(rows))

	// 构建单行插入SQL
	singleInsertSQL := w.buildBatchInsertSQL(1)

	failedRows := []map[string]interface{}{}
	successCount := 0
	firstRealError := batchErr

	for idx, row := range rows {
		// 为每条记录创建一个保存点，这样即使插入失败也不会中止整个事务
		savepointName := fmt.Sprintf("sp_%d", idx)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("SAVEPOINT %s", savepointName)); err != nil {
			log.Printf("⚠️  创建保存点失败: %v\n", err)
			continue
		}

		// 准备单行数据
		values := make([]interface{}, 0, len(w.columns))
		for _, col := range w.columns {
			var original interface{}
			if row != nil {
				original = row[col]
			}
			prepared, err := w.prepareValue(col, original)
			if err != nil {
				return fmt.Errorf("记录 #%d 准备数据失败 (字段 %s): %w", idx, col, err)
			}
			values = append(values, prepared)
		}

		// 尝试插入单行
		if _, err := tx.ExecContext(ctx, singleInsertSQL, values...); err != nil {
			// 回滚到保存点，避免事务中止
			tx.ExecContext(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", savepointName))

			log.Printf("❌ 记录 #%d 插入失败:\n", idx)

			// 输出关键字段信息帮助定位
			keyFields := []string{"SmID", "id", "ID", "rowid", "ROWID", "fid", "FID", "ogc_fid"}
			for _, keyField := range keyFields {
				if val, ok := row[keyField]; ok {
					log.Printf("   %s: %v\n", keyField, val)
				}
			}

			// 输出几何字段信息
			for col, meta := range w.geometryColumns {
				if geomVal, ok := row[col]; ok {
					if geomBytes, ok := geomVal.([]byte); ok {
						log.Printf("   几何字段 %s: WKB长度=%d bytes, SRID=%d, SpatialType=%s\n",
							col, len(geomBytes), meta.SRID, meta.SpatialType)
						log.Printf("   WKB前32字节: %x\n", geomBytes[:min(32, len(geomBytes))])
					}
				}
			}

			log.Printf("   错误: %v\n\n", err)

			// 记录第一个真实错误（不是"transaction is aborted"）
			if firstRealError == batchErr {
				firstRealError = err
			}

			failedRows = append(failedRows, row)
		} else {
			// 成功插入，释放保存点
			tx.ExecContext(ctx, fmt.Sprintf("RELEASE SAVEPOINT %s", savepointName))
			successCount++
		}
	}

	if len(failedRows) > 0 {
		return fmt.Errorf("批量插入失败: 成功 %d 条，失败 %d 条。首个失败原因: %v",
			successCount, len(failedRows), firstRealError)
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
// executePostImportTasks 执行数据导入后的优化任务
func (w *JDBCWriter) executePostImportTasks(ctx context.Context) {
	if w.postImportTasksExecuted {
		return
	}
	defer func() {
		w.postImportTasksExecuted = true
	}()

	if !w.isPostgres() {
		return
	}

	log.Printf("\n🔧 开始执行导入后优化任务...\n")

	// 任务 1: 创建主键约束
	if w.config.CreatePrimaryKey && w.pendingPrimaryKey != nil {
		if err := w.addPrimaryKeyConstraint(ctx); err != nil {
			log.Printf("❌ 主键约束创建失败: %v\n", err)
		} else {
			log.Printf("✅ 主键约束创建成功: %v\n", w.pendingPrimaryKey.Columns)
		}
	}

	// 任务 2: 创建空间索引
	if w.config.CreateSpatialIndex {
		if err := w.createSpatialIndexesPostImport(ctx); err != nil {
			log.Printf("❌ 空间索引创建失败: %v\n", err)
		}
	}

	// 任务 3: 更新统计信息
	analyzeSQL := fmt.Sprintf("ANALYZE %s", w.qualifiedTableName())
	if _, err := w.db.ExecContext(ctx, analyzeSQL); err != nil {
		log.Printf("⚠️  ANALYZE 执行失败: %v\n", err)
	} else {
		log.Printf("✅ 统计信息更新成功\n")
	}

	log.Printf("🎉 导入后优化任务完成\n\n")
}

// addPrimaryKeyConstraint 添加主键约束（导入后）
func (w *JDBCWriter) addPrimaryKeyConstraint(ctx context.Context) error {
	if w.pendingPrimaryKey == nil || len(w.pendingPrimaryKey.Columns) == 0 {
		return nil
	}

	pk := w.pendingPrimaryKey

	// 引用列名
	quotedColumns := make([]string, len(pk.Columns))
	for i, col := range pk.Columns {
		quotedColumns[i] = w.quoteIdentifier(col)
	}

	// 确定约束名
	constraintName := pk.Name
	if constraintName == "" {
		_, tableName := splitTableName(w.table)
		constraintName = fmt.Sprintf("%s_pkey", tableName)
	}

	// ALTER TABLE ADD PRIMARY KEY
	alterSQL := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY (%s)",
		w.qualifiedTableName(),
		w.quoteIdentifier(constraintName),
		strings.Join(quotedColumns, ", "))

	log.Printf("🔑 添加主键约束: %s\n", constraintName)

	if _, err := w.db.ExecContext(ctx, alterSQL); err != nil {
		// 容错：主键已存在或数据有重复
		if strings.Contains(err.Error(), "already exists") {
			log.Printf("⚠️  主键约束已存在: %s\n", constraintName)
			return nil
		} else if strings.Contains(err.Error(), "duplicate") {
			return fmt.Errorf("数据包含重复值，无法创建主键: %w", err)
		}
		return err
	}

	return nil
}

// createSpatialIndexesPostImport 创建空间索引（导入后）
func (w *JDBCWriter) createSpatialIndexesPostImport(ctx context.Context) error {
	// 提取空间索引信息
	indexes := w.extractSpatialIndexInfoFromCache()
	if len(indexes) == 0 {
		log.Printf("ℹ️  未检测到空间列，跳过空间索引创建\n")
		return nil
	}

	// 逐个创建空间索引
	for _, idx := range indexes {
		// 验证几何列是否存在
		found := false
		for _, col := range w.columns {
			if strings.EqualFold(col, idx.ColumnName) {
				found = true
				break
			}
		}

		if !found {
			log.Printf("⚠️  几何列 %s 不存在于表中，跳过索引创建\n", idx.ColumnName)
			continue
		}

		indexSQL := w.buildSpatialIndexSQL(idx)

		log.Printf("🗺️  创建空间索引: %s (列: %s, SRID: %d)\n",
			idx.IndexName, idx.ColumnName, idx.SRID)

		if _, err := w.db.ExecContext(ctx, indexSQL); err != nil {
			if strings.Contains(err.Error(), "already exists") {
				log.Printf("⚠️  空间索引 %s 已存在，跳过\n", idx.IndexName)
			} else {
				log.Printf("❌ 空间索引创建失败: %v\n", err)
			}
		} else {
			log.Printf("✅ 空间索引创建成功: %s\n", idx.IndexName)
		}
	}

	return nil
}

// extractPrimaryKeyFromMetadata 从元数据中提取主键信息（支持四级降级）
func (w *JDBCWriter) extractPrimaryKeyFromMetadata(batch *pipeline.DataBatch) *primaryKeyInfo {
	if batch.Schema == nil {
		return nil
	}

	// Level 0: 从 Schema.PrimaryKey 提取（新格式，最高优先级）
	if batch.Schema.PrimaryKey != nil && len(batch.Schema.PrimaryKey.Columns) > 0 {
		fmt.Printf("📋 从 Schema.PrimaryKey 提取主键: %v (约束名: %s)\n",
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
				log.Printf("📋 从 table_metadata 提取主键: %v (约束名: %s)\n", columns, pkName)
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
		log.Printf("📋 从 fields 推断主键: %v\n", columns)
		return &primaryKeyInfo{
			Columns: columns,
			Name:    "", // 未记录约束名
		}
	}

	log.Printf("⚠️  未找到主键信息\n")
	return nil
}

// extractSpatialIndexInfoFromCache 从缓存的几何列信息提取空间索引信息
func (w *JDBCWriter) extractSpatialIndexInfoFromCache() []spatialIndexInfo {
	indexes := []spatialIndexInfo{}

	if len(w.geometryColumns) == 0 {
		return indexes
	}

	for col, meta := range w.geometryColumns {
		_, tableName := splitTableName(w.table)
		indexName := fmt.Sprintf("idx_%s_%s", tableName, col)

		if w.config.SpatialIndexName != "" {
			indexName = w.config.SpatialIndexName
		}

		indexes = append(indexes, spatialIndexInfo{
			ColumnName: col,
			IndexName:  indexName,
			SRID:       meta.SRID,
		})

		log.Printf("📍 检测到几何列: %s，将创建空间索引\n", col)
	}

	return indexes
}

// buildSpatialIndexSQL 构建空间索引 SQL
func (w *JDBCWriter) buildSpatialIndexSQL(idx spatialIndexInfo) string {
	return fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s USING GIST (%s)",
		w.quoteIdentifier(idx.IndexName),
		w.qualifiedTableName(),
		w.quoteIdentifier(idx.ColumnName))
}

