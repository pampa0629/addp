package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	"github.com/addp/common/resume"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkb"
)

const mysqlDefaultInsertChunkSize = 1000
const mysqlMaxBindParams = 65535

const mysqlTableWriteSessionMarkerProvider = "mysql.table_write_session"
const mysqlTableWriteSessionMarkerPositionUnit = "session_commit"

func (p *MySQLPlugin) WriteBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.BatchWriteOptions) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{
		Method:      opts.Method,
		Fields:      mysqlBatchFieldsForWrite(batch),
		SpatialInfo: mysqlBatchSpatialInfo(batch),
	})
	if err != nil {
		return err
	}
	if err := session.WriteBatch(ctx, batch); err != nil {
		_ = session.Abort(ctx)
		return err
	}
	return session.Close(ctx)
}

func (p *MySQLPlugin) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if !shared.HasSpatialTableWrite(opts.Fields, opts.SpatialInfo) {
		return p.nonSpatialTableWriter().OpenTableWriteSession(ctx, connInfo, path, opts)
	}
	if err := resume.RejectUnsupported(opts.ResumeMarker, "mysql.table_write_session"); err != nil {
		return nil, err
	}
	if !shouldUseMySQLInsertWriteMethod(opts.Method) {
		return nil, fmt.Errorf("mysql table write session only supports insert method")
	}

	database, table, err := mysqlTablePathParts(path)
	if err != nil {
		return nil, err
	}
	columns := mysqlFieldColumns(opts.Fields)
	if len(columns) == 0 {
		return nil, fmt.Errorf("mysql table write session requires fields")
	}
	if len(columns) > mysqlMaxBindParams {
		return nil, fmt.Errorf("mysql table write session has %d columns, exceeding max bind parameters %d", len(columns), mysqlMaxBindParams)
	}

	connStr, err := p.serverDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build mysql dsn: %w", err)
	}
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql connection: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin mysql table write session: %w", err)
	}

	return &mysqlTableWriteSession{
		db:            db,
		tx:            tx,
		database:      database,
		table:         table,
		columns:       columns,
		geometrySRIDs: mysqlGeometrySRIDs(opts.Fields, opts.SpatialInfo),
		chunkSize:     effectiveMySQLInsertChunkSize(len(columns), mysqlDefaultInsertChunkSize),
	}, nil
}

func shouldUseMySQLInsertWriteMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "insert", "mysql_insert", "copy":
		return true
	default:
		return false
	}
}

func mysqlBatchFieldsForWrite(batch *plugin.BatchData) []datatype.FieldInfo {
	if batch == nil {
		return nil
	}
	if len(batch.Fields) > 0 {
		return batch.Fields
	}
	columns := mysqlBatchColumns(batch)
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, datatype.FieldInfo{Name: column, Type: datatype.FieldTypeUnknown, Nullable: true})
	}
	return fields
}

func mysqlBatchSpatialInfo(batch *plugin.BatchData) *datatype.SpatialInfo {
	if batch == nil {
		return nil
	}
	return batch.Spatial
}

func mysqlFieldColumns(fields []datatype.FieldInfo) []string {
	seen := map[string]struct{}{}
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		columns = append(columns, name)
	}
	return columns
}

func mysqlBatchColumns(batch *plugin.BatchData) []string {
	if batch == nil {
		return nil
	}
	if columns := mysqlFieldColumns(batch.Fields); len(columns) > 0 {
		return columns
	}
	seen := map[string]struct{}{}
	columns := make([]string, 0)
	for _, row := range batch.Rows {
		for name := range row {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			columns = append(columns, name)
		}
	}
	sort.Strings(columns)
	return columns
}

func effectiveMySQLInsertChunkSize(columnCount, requested int) int {
	if requested <= 0 {
		requested = mysqlDefaultInsertChunkSize
	}
	if columnCount <= 0 {
		return requested
	}
	maxRowsByParams := mysqlMaxBindParams / columnCount
	if requested > maxRowsByParams {
		return maxRowsByParams
	}
	return requested
}

func buildMySQLInsertSQL(database, table string, columns []string, rows []map[string]interface{}, geometrySRIDs map[string]int) (string, []interface{}, error) {
	dialect := mysqlDialect()
	quotedColumns := make([]string, 0, len(columns))
	for _, column := range columns {
		quotedColumns = append(quotedColumns, dialect.QuoteIdentifier(column))
	}

	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(dialect.QualifiedTable(database, table))
	sb.WriteString(" (")
	sb.WriteString(strings.Join(quotedColumns, ", "))
	sb.WriteString(") VALUES ")

	args := make([]interface{}, 0, len(rows)*len(columns))
	valueGroups := make([]string, 0, len(rows))
	for _, row := range rows {
		group := make([]string, len(columns))
		for i, column := range columns {
			value := row[column]
			expectedSRID, isGeometry := geometrySRIDs[column]
			if !isGeometry || value == nil {
				group[i] = "?"
				args = append(args, value)
				continue
			}
			wkbValue, srid, err := mysqlGeometryWriteValue(value, expectedSRID)
			if err != nil {
				return "", nil, fmt.Errorf("encode mysql geometry column %q: %w", column, err)
			}
			group[i] = "ST_GeomFromWKB(?)"
			if srid > 0 {
				group[i] = "ST_GeomFromWKB(?, " + strconv.Itoa(srid) + ")"
			}
			args = append(args, wkbValue)
		}
		valueGroups = append(valueGroups, "("+strings.Join(group, ", ")+")")
	}
	sb.WriteString(strings.Join(valueGroups, ", "))
	return sb.String(), args, nil
}

func mysqlGeometrySRIDs(fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) map[string]int {
	result := make(map[string]int)
	for _, field := range fields {
		if !datatype.IsSpatialFieldType(field.Type) {
			continue
		}
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		srid := 0
		if column := mysqlSpatialColumnForField(spatialInfo, name); column != nil && column.SRID != nil {
			srid = *column.SRID
		}
		result[name] = srid
	}
	return result
}

func mysqlGeometryWriteValue(value interface{}, expectedSRID int) ([]byte, int, error) {
	data, ok := value.([]byte)
	if !ok {
		return nil, 0, fmt.Errorf("EWKB geometry value must be []byte, got %T", value)
	}
	geometry, err := ewkb.Unmarshal(data)
	if err != nil {
		return nil, 0, fmt.Errorf("decode EWKB geometry: %w", err)
	}
	srid := geometry.SRID()
	if expectedSRID > 0 {
		if srid > 0 && srid != expectedSRID {
			return nil, 0, fmt.Errorf("EWKB SRID %d does not match target SRID %d", srid, expectedSRID)
		}
		srid = expectedSRID
	}
	data, err = wkb.Marshal(geometry, wkb.NDR)
	if err != nil {
		return nil, 0, fmt.Errorf("encode standard WKB geometry: %w", err)
	}
	return data, srid, nil
}

type mysqlTableWriteSession struct {
	db             *sql.DB
	tx             *sql.Tx
	database       string
	table          string
	columns        []string
	geometrySRIDs  map[string]int
	chunkSize      int
	batchesWritten int64
	rowsWritten    int64
	commitMarker   *resume.Marker
	closed         bool
}

func (s *mysqlTableWriteSession) WriteBatch(ctx context.Context, batch *plugin.BatchData) error {
	if s.closed {
		return fmt.Errorf("mysql table write session is closed")
	}
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	chunkSize := s.chunkSize
	if chunkSize <= 0 {
		chunkSize = effectiveMySQLInsertChunkSize(len(s.columns), mysqlDefaultInsertChunkSize)
	}
	if chunkSize <= 0 {
		return fmt.Errorf("mysql table write session has too many columns for insert bind parameters")
	}

	for start := 0; start < len(batch.Rows); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + chunkSize
		if end > len(batch.Rows) {
			end = len(batch.Rows)
		}
		insertSQL, args, err := buildMySQLInsertSQL(s.database, s.table, s.columns, batch.Rows[start:end], s.geometrySRIDs)
		if err != nil {
			return fmt.Errorf("build mysql table write session rows %d-%d: %w", start, end, err)
		}
		if _, err := s.tx.ExecContext(ctx, insertSQL, args...); err != nil {
			return fmt.Errorf("execute mysql table write session rows %d-%d: %w", start, end, err)
		}
	}
	s.batchesWritten++
	s.rowsWritten += int64(len(batch.Rows))
	return nil
}

func (s *mysqlTableWriteSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.tx.Commit(); err != nil {
		_ = s.db.Close()
		return fmt.Errorf("commit mysql table write session: %w", err)
	}
	s.commitMarker = s.buildCommitMarker()
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close mysql table write session connection: %w", err)
	}
	return nil
}

func (s *mysqlTableWriteSession) CommitMarker() *resume.Marker {
	if s == nil {
		return nil
	}
	return s.commitMarker.Clone()
}

func (s *mysqlTableWriteSession) Abort(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.abort()
}

func (s *mysqlTableWriteSession) abort() error {
	var abortErr error
	if s.tx != nil {
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && abortErr == nil {
			abortErr = fmt.Errorf("rollback mysql table write session: %w", err)
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && abortErr == nil {
			abortErr = fmt.Errorf("close mysql table write session connection: %w", err)
		}
	}
	return abortErr
}

func (s *mysqlTableWriteSession) buildCommitMarker() *resume.Marker {
	return &resume.Marker{
		Version:      resume.MarkerVersionV1,
		Provider:     mysqlTableWriteSessionMarkerProvider,
		PositionUnit: mysqlTableWriteSessionMarkerPositionUnit,
		CommitPosition: map[string]interface{}{
			"rows_committed":    s.rowsWritten,
			"batches_committed": s.batchesWritten,
		},
		Fingerprint: map[string]interface{}{
			"target":   strings.Trim(s.database+"/"+s.table, "/"),
			"database": s.database,
			"table":    s.table,
			"columns":  append([]string(nil), s.columns...),
			"method":   "mysql_insert",
		},
	}
}
