package doris

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pluginshared "github.com/addp/common/engine/plugins/shared"
	"github.com/addp/common/resume"
)

const dorisDefaultInsertChunkSize = 1000
const dorisMaxBindParams = 65535

const dorisTableWriteSessionMarkerProvider = "doris.table_write_session"
const dorisTableWriteSessionMarkerPositionUnit = "session_commit"

func (p *DorisPlugin) WriteBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.BatchWriteOptions) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{
		Method: opts.Method,
		Fields: dorisBatchFieldsForWrite(batch),
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

func (p *DorisPlugin) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, "doris.table_write_session"); err != nil {
		return nil, err
	}
	if !shouldUseDorisInsertWriteMethod(opts.Method) {
		return nil, fmt.Errorf("doris table write session only supports insert method")
	}

	// INSERT may project a subset of an existing table. Validate the same field
	// rules, while key selection remains a full-table preparation constraint.
	fields, err := dorisWriteFields(opts.Fields)
	if err != nil {
		return nil, err
	}
	if opts.SpatialInfo != nil && (opts.SpatialInfo.IsSpatial() || opts.SpatialInfo.HasSpatialIndex != nil && *opts.SpatialInfo.HasSpatialIndex) {
		return nil, p.CheckTableWriteDefinition(plugin.TableWriteDefinitionFromOptions(plugin.TableWriteOptions{Fields: fields, SpatialInfo: opts.SpatialInfo})).UnsupportedError()
	}
	opts.Fields = fields

	database, table, err := dorisTablePathParts(path)
	if err != nil {
		return nil, err
	}
	columns := dorisFieldColumns(opts.Fields)
	if len(columns) == 0 {
		return nil, fmt.Errorf("doris table write session requires fields")
	}
	if len(columns) > dorisMaxBindParams {
		return nil, fmt.Errorf("doris table write session has %d columns, exceeding max bind parameters %d", len(columns), dorisMaxBindParams)
	}

	connStr, err := p.serverDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build doris dsn: %w", err)
	}
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open doris connection: %w", err)
	}

	return &dorisTableWriteSession{
		db:         db,
		database:   database,
		table:      table,
		columns:    columns,
		fieldTypes: pluginshared.TableWriteFieldTypes(opts.Fields),
		chunkSize:  effectiveDorisInsertChunkSize(len(columns), dorisDefaultInsertChunkSize),
	}, nil
}

func shouldUseDorisInsertWriteMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "insert", "doris_insert", "copy":
		return true
	default:
		return false
	}
}

func dorisBatchFieldsForWrite(batch *plugin.BatchData) []datatype.FieldInfo {
	if batch == nil {
		return nil
	}
	if len(batch.Fields) > 0 {
		return batch.Fields
	}
	columns := dorisBatchColumns(batch)
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, datatype.FieldInfo{Name: column, Type: datatype.FieldTypeUnknown, Nullable: true})
	}
	return fields
}

func dorisFieldColumns(fields []datatype.FieldInfo) []string {
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

func dorisBatchColumns(batch *plugin.BatchData) []string {
	if batch == nil {
		return nil
	}
	if columns := dorisFieldColumns(batch.Fields); len(columns) > 0 {
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

func effectiveDorisInsertChunkSize(columnCount, requested int) int {
	if requested <= 0 {
		requested = dorisDefaultInsertChunkSize
	}
	if columnCount <= 0 {
		return requested
	}
	maxRowsByParams := dorisMaxBindParams / columnCount
	if requested > maxRowsByParams {
		return maxRowsByParams
	}
	return requested
}

func buildDorisInsertSQL(database, table string, columns []string, fieldTypes map[string]datatype.FieldType, rowCount int) string {
	dialect := dorisDialect()
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

	valueGroups := make([]string, 0, rowCount)
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		group := make([]string, len(columns))
		for i, column := range columns {
			group[i] = "?"
			if fieldTypes[column] == datatype.FieldTypeBytes {
				group[i] = "UNHEX(?)"
			}
		}
		valueGroups = append(valueGroups, "("+strings.Join(group, ", ")+")")
	}
	sb.WriteString(strings.Join(valueGroups, ", "))
	return sb.String()
}

func dorisTableWriteArgs(ctx context.Context, columns []string, fieldTypes map[string]datatype.FieldType, rows []map[string]interface{}) ([]interface{}, error) {
	args := make([]interface{}, 0, len(rows)*len(columns))
	for rowIndex, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, column := range columns {
			value, err := pluginshared.EncodeTableWriteValue(row[column], fieldTypes[column])
			if err != nil {
				return nil, fmt.Errorf("encode doris row %d field %q: %w", rowIndex, column, err)
			}
			// Doris rejects the MySQL driver's _binary literal. Bind ASCII hex
			// and let UNHEX restore bytes without any character conversion.
			if fieldTypes[column] == datatype.FieldTypeBytes && value != nil {
				binary, ok := value.([]byte)
				if !ok {
					return nil, fmt.Errorf("encode doris row %d field %q: bytes requires []byte, got %T", rowIndex, column, value)
				}
				if binary == nil {
					value = nil
				} else {
					value = hex.EncodeToString(binary)
				}
			}
			args = append(args, value)
		}
	}
	return args, nil
}

type dorisTableWriteSession struct {
	db             *sql.DB
	database       string
	table          string
	columns        []string
	fieldTypes     map[string]datatype.FieldType
	chunkSize      int
	batchesWritten int64
	rowsWritten    int64
	commitMarker   *resume.Marker
	closed         bool
}

func (s *dorisTableWriteSession) WriteBatch(ctx context.Context, batch *plugin.BatchData) error {
	if s.closed {
		return fmt.Errorf("doris table write session is closed")
	}
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	chunkSize := s.chunkSize
	if chunkSize <= 0 {
		chunkSize = effectiveDorisInsertChunkSize(len(s.columns), dorisDefaultInsertChunkSize)
	}
	if chunkSize <= 0 {
		return fmt.Errorf("doris table write session has too many columns for insert bind parameters")
	}
	// Doris INSERT chunks commit independently. Validate the entire batch before
	// issuing SQL so an encoding error cannot leave earlier chunks committed.
	args, err := dorisTableWriteArgs(ctx, s.columns, s.fieldTypes, batch.Rows)
	if err != nil {
		return err
	}

	for start := 0; start < len(batch.Rows); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + chunkSize
		if end > len(batch.Rows) {
			end = len(batch.Rows)
		}
		insertSQL := buildDorisInsertSQL(s.database, s.table, s.columns, s.fieldTypes, end-start)
		if _, err := s.db.ExecContext(ctx, insertSQL, args[start*len(s.columns):end*len(s.columns)]...); err != nil {
			return fmt.Errorf("execute doris table write session rows %d-%d: %w", start, end, err)
		}
	}
	s.batchesWritten++
	s.rowsWritten += int64(len(batch.Rows))
	return nil
}

func (s *dorisTableWriteSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.commitMarker = s.buildCommitMarker()
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close doris table write session connection: %w", err)
	}
	return nil
}

func (s *dorisTableWriteSession) CommitMarker() *resume.Marker {
	if s == nil {
		return nil
	}
	return s.commitMarker.Clone()
}

func (s *dorisTableWriteSession) Abort(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.abort()
}

func (s *dorisTableWriteSession) abort() error {
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			return fmt.Errorf("close doris table write session connection: %w", err)
		}
	}
	return nil
}

func (s *dorisTableWriteSession) buildCommitMarker() *resume.Marker {
	return &resume.Marker{
		Version:      resume.MarkerVersionV1,
		Provider:     dorisTableWriteSessionMarkerProvider,
		PositionUnit: dorisTableWriteSessionMarkerPositionUnit,
		CommitPosition: map[string]interface{}{
			"rows_committed":    s.rowsWritten,
			"batches_committed": s.batchesWritten,
		},
		Fingerprint: map[string]interface{}{
			"target":   strings.Trim(s.database+"/"+s.table, "/"),
			"database": s.database,
			"table":    s.table,
			"columns":  append([]string(nil), s.columns...),
			"method":   "doris_insert",
		},
	}
}
