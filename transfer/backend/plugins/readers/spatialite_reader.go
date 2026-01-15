package readers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3" // SQLite driver

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/plugins/utils"
)

// SpatiaLiteReader 读取基于 SpatiaLite 的 SQLite 数据库
// 将几何列通过 AsBinary() 导出为标准 WKB 字节,方便后续写入 PostGIS
type SpatiaLiteReader struct {
	db              *sql.DB
	baseQuery       string
	table           string
	where           string
	batchSize       int
	offset          int64
	columns         []string
	geomCols        map[string]geomMeta
	schema          *pipeline.Schema
	mode            pipeline.ReaderMode
	geomFunc        string
	extensionLoaded bool
	extensionPath   string
}

// SpatiaLiteConfig 配置
type SpatiaLiteConfig struct {
	FilePath       string   `json:"file_path"`       // .sqlite / .db 文件路径
	Table          string   `json:"table"`           // 表名（与 Query 二选一）
	Query          string   `json:"query"`           // 自定义查询（需自行确保几何列已 AsBinary）
	WhereClause    string   `json:"where_clause"`    // WHERE 条件
	GeometryFields []string `json:"geometry_fields"` // 指定几何列（不指定则自动探测）
	ExtensionPath  string   `json:"extension_path"`  // 指定 mod_spatialite 绝对路径
}

type geomMeta struct {
	SRID        int
	SpatialType string
}

// NewSpatiaLiteReader 工厂
func NewSpatiaLiteReader(config pipeline.ConnectorConfig) (pipeline.Reader, error) {
	var slc SpatiaLiteConfig
	if err := utils.MapToStruct(config.Config, &slc); err != nil {
		return nil, fmt.Errorf("invalid spatialite config: %w", err)
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	return &SpatiaLiteReader{
		batchSize: batchSize,
		table:     slc.Table,
		where:     slc.WhereClause,
		mode:      pipeline.ModeBatch,
	}, nil
}

// Open 打开 SpatiaLite 数据库
func (r *SpatiaLiteReader) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var slc SpatiaLiteConfig
	if err := utils.MapToStruct(config.Config, &slc); err != nil {
		return err
	}
	r.extensionPath = strings.TrimSpace(slc.ExtensionPath)
	r.extensionLoaded = false
	r.geomFunc = ""

	if slc.FilePath == "" {
		return fmt.Errorf("file_path is required")
	}

	db, err := sql.Open("sqlite3", slc.FilePath)
	if err != nil {
		return fmt.Errorf("failed to open spatialite db: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping spatialite db: %w", err)
	}

	r.db = db

	// 构建查询：优先使用自定义 Query
	if strings.TrimSpace(slc.Query) != "" {
		r.baseQuery = slc.Query
	} else if strings.TrimSpace(slc.Table) != "" {
		r.table = slc.Table

		// 探测几何元数据
		detectedGeom, _ := r.detectGeometryColumns(ctx, r.table)
		// 如果用户指定了 geometry_fields，则以用户为准（大小写不敏感匹配）
		if len(slc.GeometryFields) > 0 {
			filtered := make(map[string]geomMeta)
			for _, name := range slc.GeometryFields {
				for k, meta := range detectedGeom {
					if strings.EqualFold(k, name) {
						filtered[k] = meta
						break
					}
				}
			}
			r.geomCols = filtered
		} else {
			r.geomCols = detectedGeom
		}

		if len(r.geomCols) > 0 {
			if err := r.loadSpatiaLiteExtension(ctx); err != nil {
				db.Close()
				return err
			}
			if err := r.ensureGeometryFunc(ctx); err != nil {
				db.Close()
				return err
			}
		}

		r.baseQuery = r.buildSelectQueryForTable(r.table, r.where, r.geomCols)
	} else {
		return fmt.Errorf("either query or table must be specified")
	}

	// 推断 schema
	schema, err := r.inferSchema(ctx)
	if err != nil {
		return fmt.Errorf("failed to infer schema: %w", err)
	}
	r.schema = schema

	return nil
}

// Read 读取一批
func (r *SpatiaLiteReader) Read(ctx context.Context) (*pipeline.DataBatch, error) {
	// 每次都执行新的分页查询
	q := r.buildPaginatedQuery()
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// 获取列名（第一次或列名未初始化时）
	if r.columns == nil {
		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		r.columns = cols
	}

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

	// 如果没有读取到数据，说明已经到达末尾
	if len(batchRows) == 0 {
		return nil, io.EOF
	}

	// 更新偏移量以便下次查询
	r.offset += int64(len(batchRows))

	return &pipeline.DataBatch{
		Rows:      batchRows,
		Schema:    r.schema,
		Offset:    r.offset,
		Timestamp: time.Now(),
	}, nil
}

// Schema 返回 schema
func (r *SpatiaLiteReader) Schema() (*pipeline.Schema, error) {
	if r.schema == nil {
		return nil, fmt.Errorf("schema not initialized, call Open first")
	}
	return r.schema, nil
}

// SeekTo 重置偏移
func (r *SpatiaLiteReader) SeekTo(offset int64) error {
	r.offset = offset
	return nil
}

// Close 关闭连接
func (r *SpatiaLiteReader) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

// Mode 返回模式
func (r *SpatiaLiteReader) Mode() pipeline.ReaderMode { return r.mode }

// --- helpers ---

func (r *SpatiaLiteReader) buildSelectQueryForTable(table, where string, geomCols map[string]geomMeta) string {
	// 读取表结构以拿到所有列
	cols, _ := r.fetchTableColumns(table)
	if len(cols) == 0 {
		// 回退到 SELECT *
		q := fmt.Sprintf("SELECT * FROM %s", table)
		if strings.TrimSpace(where) != "" {
			q += " WHERE " + where
		}
		return q
	}

	// 为几何列构建大小写不敏感的查找表，避免 geometry_columns 与实际列名大小写不一致
	lowerGeomCols := make(map[string]struct{}, len(geomCols))
	for name := range geomCols {
		lowerGeomCols[strings.ToLower(name)] = struct{}{}
	}

	// 对几何列使用 ST_AsBinary/AsBinary 函数
	projected := make([]string, 0, len(cols))
	for _, c := range cols {
		if _, isGeom := lowerGeomCols[strings.ToLower(c)]; isGeom {
			geomExpr := r.buildGeometryExpression(c)
			projected = append(projected, fmt.Sprintf("%s AS %s", geomExpr, quoteIdentSQLite(c)))
		} else {
			projected = append(projected, quoteIdentSQLite(c))
		}
	}

	selectList := strings.Join(projected, ", ")
	q := fmt.Sprintf("SELECT %s FROM %s", selectList, quoteIdentSQLite(table))
	if strings.TrimSpace(where) != "" {
		q += " WHERE " + where
	}
	return q
}

func (r *SpatiaLiteReader) buildPaginatedQuery() string {
	// SQLite 支持 LIMIT / OFFSET	re
	return fmt.Sprintf("%s LIMIT %d OFFSET %d", r.baseQuery, r.batchSize, r.offset)
}

func (r *SpatiaLiteReader) buildGeometryExpression(column string) string {
	fn := r.geomFunc
	if strings.TrimSpace(fn) == "" {
		fn = "ST_AsBinary"
	}
	return fmt.Sprintf("%s(%s)", fn, quoteIdentSQLite(column))
}

func (r *SpatiaLiteReader) scanRow(rows *sql.Rows) (map[string]interface{}, error) {
	values := make([]interface{}, len(r.columns))
	ptrs := make([]interface{}, len(r.columns))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}

	row := make(map[string]interface{}, len(r.columns))
	for i, col := range r.columns {
		v := values[i]
		if b, ok := v.([]byte); ok {
			// 包含几何（AsBinary -> WKB）或其它 BLOB
			row[col] = b
			continue
		}
		row[col] = v
	}
	return row, nil
}

func (r *SpatiaLiteReader) inferSchema(ctx context.Context) (*pipeline.Schema, error) {
	// 使用 LIMIT 1 取列名与类型；并融合 geometry_columns 元数据
	// 先拿几何列元信息
	geom := r.geomCols
	if geom == nil && r.table != "" {
		detected, _ := r.detectGeometryColumns(ctx, r.table)
		geom = detected
	}

	// 为防止自定义 Query 下推断失败，只尝试用该查询 LIMIT 1 获取列名
	q := fmt.Sprintf("%s LIMIT 1", r.baseQuery)
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		// 回退：如果失败，尝试从 PRAGMA table_info 获取
		if r.table != "" {
			return r.inferSchemaFromTable(ctx, r.table, geom)
		}
		return nil, err
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	fields := make([]pipeline.Field, 0, len(colTypes))
	for _, ct := range colTypes {
		name := ct.Name()
		upperDBType := strings.ToUpper(ct.DatabaseTypeName())

		f := pipeline.Field{
			Name:     name,
			Type:     mapSQLiteType(upperDBType),
			Nullable: true,
		}
		if meta, ok := geom[name]; ok {
			f.Type = "geometry"
			f.SpatialType = meta.SpatialType
			f.SRID = meta.SRID
		}
		fields = append(fields, f)
	}

	return &pipeline.Schema{
		Fields: fields,
		Metadata: map[string]interface{}{
			"source_type": "spatialite",
			"table":       r.table,
		},
	}, nil
}

func (r *SpatiaLiteReader) inferSchemaFromTable(ctx context.Context, table string, geom map[string]geomMeta) (*pipeline.Schema, error) {
	cols, err := r.fetchTableInfo(ctx, table)
	if err != nil {
		return nil, err
	}
	fields := make([]pipeline.Field, 0, len(cols))
	for _, c := range cols {
		f := pipeline.Field{
			Name:     c.name,
			Type:     mapSQLiteType(strings.ToUpper(c.dataType)),
			Nullable: !c.notNull,
		}
		if meta, ok := geom[c.name]; ok {
			f.Type = "geometry"
			f.SpatialType = meta.SpatialType
			f.SRID = meta.SRID
		}
		fields = append(fields, f)
	}
	return &pipeline.Schema{
		Fields: fields,
		Metadata: map[string]interface{}{
			"source_type": "spatialite",
			"table":       table,
		},
	}, nil
}

// --- metadata helpers ---

func (r *SpatiaLiteReader) detectGeometryColumns(ctx context.Context, table string) (map[string]geomMeta, error) {
	// geometry_columns: f_table_name, f_geometry_column, geometry_type, coord_dimension, srid, spatial_index_enabled
	q := `SELECT f_geometry_column, srid, geometry_type FROM geometry_columns WHERE LOWER(f_table_name) = LOWER(?)`
	rows, err := r.db.QueryContext(ctx, q, table)
	if err != nil {
		return map[string]geomMeta{}, err
	}
	defer rows.Close()

	result := make(map[string]geomMeta)
	for rows.Next() {
		var col string
		var srid int
		var gtype int
		if err := rows.Scan(&col, &srid, &gtype); err != nil {
			return result, err
		}
		result[col] = geomMeta{SRID: srid, SpatialType: mapSpatiaLiteGeomType(gtype)}
	}
	return result, nil
}

func (r *SpatiaLiteReader) ensureGeometryFunc(ctx context.Context) error {
	if strings.TrimSpace(r.geomFunc) != "" {
		return nil
	}

	candidates := []string{"ST_AsBinary", "AsBinary"}
	var lastErr error
	for _, fn := range candidates {
		if err := r.probeGeometryFunc(ctx, fn); err == nil {
			r.geomFunc = fn
			fmt.Printf("DEBUG: SpatiaLite Reader - using geometry function %s\n", fn)
			return nil
		} else {
			lastErr = err
		}
	}

	return fmt.Errorf("spatialite extension loaded but no supported geometry-to-WKB function found (tried %v): %v", candidates, lastErr)
}

func (r *SpatiaLiteReader) probeGeometryFunc(ctx context.Context, fn string) error {
	queries := []string{
		fmt.Sprintf("SELECT %s(GeomFromText('POINT(0 0)', 4326))", fn),
		fmt.Sprintf("SELECT %s(ST_GeomFromText('POINT(0 0)', 4326))", fn),
	}

	var lastErr error
	for _, q := range queries {
		row := r.db.QueryRowContext(ctx, q)
		var buf []byte
		if err := row.Scan(&buf); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func (r *SpatiaLiteReader) loadSpatiaLiteExtension(ctx context.Context) error {
	if r.extensionLoaded {
		return nil
	}
	if r.db == nil {
		return fmt.Errorf("sqlite connection not initialized")
	}

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire sqlite connection: %w", err)
	}
	defer conn.Close()

	candidates := make([]string, 0, 10)

	// 1. 优先使用用户配置的扩展路径
	if r.extensionPath != "" {
		candidates = append(candidates, r.extensionPath)
	}

	// 2. 尝试从环境变量读取
	if envPath := os.Getenv("SPATIALITE_EXTENSION_PATH"); envPath != "" {
		candidates = append(candidates, envPath)
	}

	// 3. 兜底：尝试几个最常见的路径
	candidates = append(candidates,
		"/opt/homebrew/lib/mod_spatialite.dylib",  // macOS ARM (M1/M2/M3)
		"/usr/local/lib/mod_spatialite.dylib",     // macOS Intel
		"mod_spatialite",                          // 系统动态库搜索路径
	)

	// 尝试加载 SpatiaLite 扩展
	var lastErr error
	for _, extPath := range candidates {
		err := conn.Raw(func(driverConn interface{}) error {
			sqliteConn, ok := driverConn.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("failed to cast to SQLiteConn")
			}
			return sqliteConn.LoadExtension(extPath, "sqlite3_modspatialite_init")
		})

		if err == nil {
			fmt.Printf("INFO: SpatiaLite extension loaded successfully from %s\n", extPath)
			r.extensionLoaded = true
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("failed to load SpatiaLite extension (tried %v): %w", candidates, lastErr)
}

func mapSpatiaLiteGeomType(code int) string {
	switch code {
	case 1:
		return "POINT"
	case 2:
		return "LINESTRING"
	case 3:
		return "POLYGON"
	case 4:
		return "MULTIPOINT"
	case 5:
		return "MULTILINESTRING"
	case 6:
		return "MULTIPOLYGON"
	case 7:
		return "GEOMETRYCOLLECTION"
	default:
		return "Geometry"
	}
}

type tableCol struct {
	name     string
	dataType string
	notNull  bool
}

func (r *SpatiaLiteReader) fetchTableInfo(ctx context.Context, table string) ([]tableCol, error) {
	q := fmt.Sprintf("PRAGMA table_info(%s)", quoteIdentSQLite(table))
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []tableCol
	for rows.Next() {
		var cid int
		var name, dtype string
		var notNull int
		var dflt interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &dtype, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		out = append(out, tableCol{name: name, dataType: dtype, notNull: notNull != 0})
	}
	return out, nil
}

func (r *SpatiaLiteReader) fetchTableColumns(table string) ([]string, error) {
	infos, err := r.fetchTableInfo(context.Background(), table)
	if err != nil {
		return nil, err
	}
	cols := make([]string, 0, len(infos))
	for _, c := range infos {
		cols = append(cols, c.name)
	}
	return cols, nil
}

func mapSQLiteType(sqliteType string) string {
	switch sqliteType {
	case "TEXT":
		return "string"
	case "INTEGER":
		return "int"
	case "REAL", "NUMERIC", "DOUBLE", "FLOAT":
		return "float"
	case "BLOB":
		return "binary"
	case "GEOMETRY":
		return "geometry"
	default:
		return "string"
	}
}

func quoteIdentSQLite(id string) string {
	// Use double-quotes for SQLite identifiers
	return `"` + strings.ReplaceAll(id, `"`, `""`) + `"`
}
