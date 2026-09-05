package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
	go_ora "github.com/sijms/go-ora/v2"
)

const oracleTableWriteSessionMarkerProvider = "oracle.table_write_session"
const oracleTableWriteSessionMarkerPositionUnit = "session_commit"

func (p *OraclePlugin) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, oracleTableWriteSessionMarkerProvider); err != nil {
		return nil, err
	}
	if !shouldUseOracleInsertWriteMethod(opts.Method) {
		return nil, fmt.Errorf("oracle table write session only supports insert method")
	}
	schema, table, err := oracleTablePathParts(path)
	if err != nil {
		return nil, err
	}
	fields, err := validateOracleWriteFields(opts.Fields, opts.SpatialInfo)
	if err != nil {
		return nil, err
	}
	insertSQL, err := buildOracleInsertSQL(schema, table, fields, opts.SpatialInfo)
	if err != nil {
		return nil, err
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build Oracle table write session DSN: %w", err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return nil, fmt.Errorf("open Oracle table write session connection: %w", err)
	}
	if err := validateOracleTargetSchema(ctx, db, schema); err != nil {
		db.Close()
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin Oracle table write session: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("prepare Oracle table write session: %w", err)
	}
	return &oracleTableWriteSession{
		db:          db,
		tx:          tx,
		stmt:        stmt,
		schema:      schema,
		table:       table,
		fields:      fields,
		spatialInfo: opts.SpatialInfo.Clone(),
	}, nil
}

func shouldUseOracleInsertWriteMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "copy", "insert", "oracle_insert":
		return true
	default:
		return false
	}
}

func buildOracleInsertSQL(schema, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) (string, error) {
	if len(fields) == 0 {
		return "", fmt.Errorf("oracle table write session requires fields")
	}
	dialect := commonquery.ForDialect("oracle")
	columns := make([]string, 0, len(fields))
	values := make([]string, 0, len(fields))
	for index, field := range fields {
		columns = append(columns, dialect.QuoteIdentifier(field.Name))
		expression, err := oracleInsertValueExpression(field, spatialInfo, index+1)
		if err != nil {
			return "", err
		}
		values = append(values, expression)
	}
	return "INSERT INTO " + dialect.QualifiedTable(schema, table) + " (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(values, ", ") + ")", nil
}

func oracleInsertValueExpression(field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo, bindIndex int) (string, error) {
	bind := fmt.Sprintf(":%d", bindIndex)
	if !datatype.IsSpatialFieldType(field.Type) {
		return bind, nil
	}
	column := oracleSpatialColumnForField(spatialInfo, field.Name)
	if column == nil {
		return "", fmt.Errorf("oracle geometry field %q requires frozen spatial facts", field.Name)
	}
	srid := "NULL"
	if column.SRID != nil && *column.SRID > 0 {
		srid = strconv.Itoa(*column.SRID)
	}
	gtype := oracleSDOGTypeExpression(column)
	return "(SELECT CASE WHEN decoded.raw_geom IS NULL THEN NULL ELSE " +
		"MDSYS.SDO_GEOMETRY(" + gtype + ", " + srid +
		", decoded.raw_geom.SDO_POINT, decoded.raw_geom.SDO_ELEM_INFO, decoded.raw_geom.SDO_ORDINATES) END " +
		"FROM (SELECT SDO_UTIL.FROM_WKBGEOMETRY(" + bind + ") raw_geom FROM DUAL) decoded)", nil
}

func oracleTableWriteValue(field datatype.FieldInfo, value interface{}, spatialInfo *datatype.SpatialInfo) (interface{}, error) {
	if value == nil {
		if !field.Nullable {
			return nil, fmt.Errorf("oracle insert field %q is NOT NULL", field.Name)
		}
		return nil, nil
	}
	if datatype.IsSpatialFieldType(field.Type) {
		column := oracleSpatialColumnForField(spatialInfo, field.Name)
		encoded, err := oracleGeometryWKB(value, column)
		if err != nil {
			return nil, fmt.Errorf("convert Oracle geometry field %q: %w", field.Name, err)
		}
		return go_ora.Blob{Data: encoded}, nil
	}
	return oracleScalarWriteValue(field, value)
}

type oracleTableWriteSession struct {
	db             *sql.DB
	tx             *sql.Tx
	stmt           *sql.Stmt
	schema         string
	table          string
	fields         []datatype.FieldInfo
	spatialInfo    *datatype.SpatialInfo
	batchesWritten int64
	rowsWritten    int64
	commitMarker   *resume.Marker
	closed         bool
}

func (s *oracleTableWriteSession) WriteBatch(ctx context.Context, batch *plugin.BatchData) error {
	if s.closed {
		return fmt.Errorf("oracle table write session is closed")
	}
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	values := make([]interface{}, len(s.fields))
	for rowIndex, row := range batch.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		for fieldIndex, field := range s.fields {
			value, ok := row[field.Name]
			if !ok {
				return fmt.Errorf("oracle insert row %d is missing field %q", rowIndex, field.Name)
			}
			converted, err := oracleTableWriteValue(field, value, s.spatialInfo)
			if err != nil {
				return fmt.Errorf("oracle insert row %d: %w", rowIndex, err)
			}
			values[fieldIndex] = converted
		}
		if _, err := s.stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("insert Oracle row %d: %w", rowIndex, err)
		}
	}
	s.batchesWritten++
	s.rowsWritten += int64(len(batch.Rows))
	return nil
}

func (s *oracleTableWriteSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.stmt.Close(); err != nil {
		_ = s.abort()
		return fmt.Errorf("close Oracle table write statement: %w", err)
	}
	if err := s.tx.Commit(); err != nil {
		_ = s.db.Close()
		return fmt.Errorf("commit Oracle table write session: %w", err)
	}
	s.commitMarker = s.buildCommitMarker()
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close Oracle table write connection: %w", err)
	}
	return nil
}

func (s *oracleTableWriteSession) CommitMarker() *resume.Marker {
	if s == nil {
		return nil
	}
	return s.commitMarker.Clone()
}

func (s *oracleTableWriteSession) Abort(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.abort()
}

func (s *oracleTableWriteSession) abort() error {
	var abortErr error
	if s.stmt != nil {
		if err := s.stmt.Close(); err != nil && abortErr == nil {
			abortErr = fmt.Errorf("close Oracle table write statement: %w", err)
		}
	}
	if s.tx != nil {
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && abortErr == nil {
			abortErr = fmt.Errorf("rollback Oracle table write session: %w", err)
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && abortErr == nil {
			abortErr = fmt.Errorf("close Oracle table write connection: %w", err)
		}
	}
	return abortErr
}

func (s *oracleTableWriteSession) buildCommitMarker() *resume.Marker {
	columns := make([]string, 0, len(s.fields))
	for _, field := range s.fields {
		columns = append(columns, field.Name)
	}
	return &resume.Marker{
		Version:      resume.MarkerVersionV1,
		Provider:     oracleTableWriteSessionMarkerProvider,
		PositionUnit: oracleTableWriteSessionMarkerPositionUnit,
		CommitPosition: map[string]interface{}{
			"rows_committed":    s.rowsWritten,
			"batches_committed": s.batchesWritten,
		},
		Fingerprint: map[string]interface{}{
			"target":  strings.Trim(s.schema+"/"+s.table, "/"),
			"schema":  s.schema,
			"table":   s.table,
			"columns": columns,
			"method":  "oracle_insert",
		},
	}
}
