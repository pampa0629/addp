package writers

import (
	"github.com/addp/common/spatial"
	"github.com/addp/transfer/plugins/utils"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/twpayne/go-geom/encoding/wkb"
	"github.com/twpayne/go-geom/encoding/wkt"
)

type geometryColumnMeta struct {
	SRID        int
	SpatialType string
}

// JDBCWriter JDBC 数据写入器
type JDBCWriter struct {
	db              *sql.DB
	table           string
	columns         []string
	buffer          []map[string]interface{}
	batchSize       int
	writeMode       string
	conflictKey     string
	config          JDBCWriterConfig
	driver          string
	geometryColumns map[string]geometryColumnMeta
	schema          *pipeline.Schema
	tableEnsured    bool
}

// NewJDBCWriter 创建 JDBC Writer
func NewJDBCWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	var jdbcConfig JDBCWriterConfig
	if err := utils.MapToStruct(config.Config, &jdbcConfig); err != nil {
		return nil, fmt.Errorf("invalid jdbc config: %w", err)
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 5000 // 提高默认批次大小from 1000 → 5000 (性能优化)
	}

	writeMode := jdbcConfig.WriteMode
	if writeMode == "" {
		writeMode = "insert"
	}

	return &JDBCWriter{
		batchSize:   batchSize,
		buffer:      make([]map[string]interface{}, 0, batchSize),
		writeMode:   writeMode,
		conflictKey: jdbcConfig.ConflictKey,
		config:      jdbcConfig,
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
	WriteMode        string   `json:"write_mode"`   // insert, upsert, replace
	ConflictKey      string   `json:"conflict_key"` // upsert 时的唯一键
	SSLMode          string   `json:"ssl_mode"`
	ConnectionString string   `json:"connection_string"`
	CreateTable      bool     `json:"create_table"`
	SRID             int      `json:"srid"`
	GeometryColumns  []string `json:"geometry_columns"`
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

// Close 关闭连接
func (w *JDBCWriter) Close() error {
	if w.db != nil {
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
			return fmt.Errorf("failed to batch insert rows: %w", err)
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
				return fmt.Errorf("failed to batch insert rows (chunk %d-%d): %w", i, end, err)
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

func (w *JDBCWriter) detectGeometryColumns() {
	candidates := make(map[string]geometryColumnMeta)

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
				break
			}
		}
	}
}

func (w *JDBCWriter) ensureTable(ctx context.Context, batch *pipeline.DataBatch) error {
	if !w.config.CreateTable || w.tableEnsured || !w.isPostgres() {
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
	if w.writeMode == "replace" {
		truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", w.qualifiedTableName())
		if _, err := w.db.ExecContext(ctx, truncateSQL); err != nil {
			return fmt.Errorf("failed to truncate table %s: %w", w.table, err)
		}
	}

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

	switch w.writeMode {
	case "upsert":
		if w.conflictKey == "" {
			return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
				w.qualifiedTableName(), columnsStr, valuesStr)
		}

		updateClauses := make([]string, 0, len(w.columns))
		for _, col := range w.columns {
			if strings.EqualFold(col, w.conflictKey) {
				continue
			}
			quoted := w.quoteIdentifier(col)
			updateClauses = append(updateClauses,
				fmt.Sprintf("%s = EXCLUDED.%s", quoted, quoted))
		}

		return fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
			w.qualifiedTableName(),
			columnsStr,
			valuesStr,
			w.quoteIdentifier(w.conflictKey),
			strings.Join(updateClauses, ", "),
		)
	default:
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			w.qualifiedTableName(), columnsStr, valuesStr)
	}
}

func (w *JDBCWriter) buildGenericInsert() string {
	columns := strings.Join(w.columns, ", ")
	placeholders := make([]string, len(w.columns))
	for i := range placeholders {
		placeholders[i] = w.parameterPlaceholder(i)
	}
	valuesStr := strings.Join(placeholders, ", ")

	switch w.writeMode {
	case "upsert":
		updateClauses := make([]string, 0, len(w.columns))
		for _, col := range w.columns {
			if col == w.conflictKey {
				continue
			}
			updateClauses = append(updateClauses,
				fmt.Sprintf("%s = VALUES(%s)", col, col))
		}
		return fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s",
			w.table,
			columns,
			valuesStr,
			strings.Join(updateClauses, ", "),
		)
	case "replace":
		return fmt.Sprintf("REPLACE INTO %s (%s) VALUES (%s)",
			w.table, columns, valuesStr)
	default:
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			w.table, columns, valuesStr)
	}
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
			return standardWKB, nil
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
