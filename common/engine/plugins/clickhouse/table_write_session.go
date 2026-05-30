package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

const clickhouseDefaultInsertChunkSize = 1000
const clickhouseMaxBindParams = 65535

const clickhouseTableWriteSessionMarkerProvider = "clickhouse.table_write_session"
const clickhouseTableWriteSessionMarkerPositionUnit = "session_commit"

func (p *ClickHousePlugin) WriteBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, batch *plugin.BatchData, opts plugin.BatchWriteOptions) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{
		Method: opts.Method,
		Fields: clickhouseBatchFieldsForWrite(batch),
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

func (p *ClickHousePlugin) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, "clickhouse.table_write_session"); err != nil {
		return nil, err
	}
	if !shouldUseClickHouseInsertWriteMethod(opts.Method) {
		return nil, fmt.Errorf("clickhouse table write session only supports insert method")
	}

	database, table, err := clickhouseTablePathParts(path)
	if err != nil {
		return nil, err
	}
	columns := clickhouseFieldColumns(opts.Fields)
	if len(columns) == 0 {
		return nil, fmt.Errorf("clickhouse table write session requires fields")
	}
	if len(columns) > clickhouseMaxBindParams {
		return nil, fmt.Errorf("clickhouse table write session has %d columns, exceeding max bind parameters %d", len(columns), clickhouseMaxBindParams)
	}

	connStr, err := p.serverDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build clickhouse dsn: %w", err)
	}
	db, err := sql.Open("clickhouse", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open clickhouse connection: %w", err)
	}

	return &clickhouseTableWriteSession{
		db:        db,
		database:  database,
		table:     table,
		columns:   columns,
		chunkSize: effectiveClickHouseInsertChunkSize(len(columns), clickhouseDefaultInsertChunkSize),
	}, nil
}

func shouldUseClickHouseInsertWriteMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "insert", "clickhouse_insert", "copy":
		return true
	default:
		return false
	}
}

func clickhouseBatchFieldsForWrite(batch *plugin.BatchData) []datatype.FieldInfo {
	if batch == nil {
		return nil
	}
	if len(batch.Fields) > 0 {
		return batch.Fields
	}
	columns := clickhouseBatchColumns(batch)
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, datatype.FieldInfo{Name: column, Type: datatype.FieldTypeUnknown, Nullable: true})
	}
	return fields
}

func clickhouseFieldColumns(fields []datatype.FieldInfo) []string {
	seen := map[string]struct{}{}
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || field.Generated {
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

func clickhouseBatchColumns(batch *plugin.BatchData) []string {
	if batch == nil {
		return nil
	}
	if columns := clickhouseFieldColumns(batch.Fields); len(columns) > 0 {
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

func effectiveClickHouseInsertChunkSize(columnCount, requested int) int {
	if requested <= 0 {
		requested = clickhouseDefaultInsertChunkSize
	}
	if columnCount <= 0 {
		return requested
	}
	maxRowsByParams := clickhouseMaxBindParams / columnCount
	if requested > maxRowsByParams {
		return maxRowsByParams
	}
	return requested
}

func buildClickHouseInsertSQL(database, table string, columns []string, rows []map[string]interface{}) (string, []interface{}) {
	dialect := clickhouseDialect()
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
			group[i] = "?"
			args = append(args, row[column])
		}
		valueGroups = append(valueGroups, "("+strings.Join(group, ", ")+")")
	}
	sb.WriteString(strings.Join(valueGroups, ", "))
	return sb.String(), args
}

type clickhouseTableWriteSession struct {
	db             *sql.DB
	database       string
	table          string
	columns        []string
	chunkSize      int
	batchesWritten int64
	rowsWritten    int64
	commitMarker   *resume.Marker
	closed         bool
}

func (s *clickhouseTableWriteSession) WriteBatch(ctx context.Context, batch *plugin.BatchData) error {
	if s.closed {
		return fmt.Errorf("clickhouse table write session is closed")
	}
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	chunkSize := s.chunkSize
	if chunkSize <= 0 {
		chunkSize = effectiveClickHouseInsertChunkSize(len(s.columns), clickhouseDefaultInsertChunkSize)
	}
	if chunkSize <= 0 {
		return fmt.Errorf("clickhouse table write session has too many columns for insert bind parameters")
	}

	for start := 0; start < len(batch.Rows); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + chunkSize
		if end > len(batch.Rows) {
			end = len(batch.Rows)
		}
		insertSQL, args := buildClickHouseInsertSQL(s.database, s.table, s.columns, batch.Rows[start:end])
		if _, err := s.db.ExecContext(ctx, insertSQL, args...); err != nil {
			return fmt.Errorf("execute clickhouse table write session rows %d-%d: %w", start, end, err)
		}
	}
	s.batchesWritten++
	s.rowsWritten += int64(len(batch.Rows))
	return nil
}

func (s *clickhouseTableWriteSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.commitMarker = s.buildCommitMarker()
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close clickhouse table write session connection: %w", err)
	}
	return nil
}

func (s *clickhouseTableWriteSession) CommitMarker() *resume.Marker {
	if s == nil {
		return nil
	}
	return s.commitMarker.Clone()
}

func (s *clickhouseTableWriteSession) Abort(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.abort()
}

func (s *clickhouseTableWriteSession) abort() error {
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			return fmt.Errorf("close clickhouse table write session connection: %w", err)
		}
	}
	return nil
}

func (s *clickhouseTableWriteSession) buildCommitMarker() *resume.Marker {
	return &resume.Marker{
		Version:      resume.MarkerVersionV1,
		Provider:     clickhouseTableWriteSessionMarkerProvider,
		PositionUnit: clickhouseTableWriteSessionMarkerPositionUnit,
		CommitPosition: map[string]interface{}{
			"rows_committed":    s.rowsWritten,
			"batches_committed": s.batchesWritten,
		},
		Fingerprint: map[string]interface{}{
			"target":   strings.Trim(s.database+"/"+s.table, "/"),
			"database": s.database,
			"table":    s.table,
			"columns":  append([]string(nil), s.columns...),
			"method":   "clickhouse_insert",
		},
	}
}
