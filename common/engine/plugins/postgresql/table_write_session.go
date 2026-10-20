package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
	"github.com/lib/pq"
)

const postgresTableWriteSessionMarkerProvider = "postgresql.table_write_session"
const postgresTableWriteSessionMarkerPositionUnit = "session_commit"

func (p *PostgreSQLPlugin) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if err := validatePostgresArrayFields(opts.Fields); err != nil {
		return nil, err
	}
	if err := resume.RejectUnsupported(opts.ResumeMarker, "postgresql.table_write_session"); err != nil {
		return nil, err
	}
	if !shouldUseCopyWriteMethod(opts.Method) {
		return nil, fmt.Errorf("postgresql table write session only supports copy method")
	}

	schema, table, err := tablePathParts(path)
	if err != nil {
		return nil, err
	}
	columns := fieldColumns(opts.Fields)
	if len(columns) == 0 {
		return nil, fmt.Errorf("postgresql table write session requires fields")
	}

	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build postgresql dsn: %w", err)
	}
	db, err := sql.Open(p.sqlDriverName(), connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgresql connection: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin postgresql table write session: %w", err)
	}
	creationMatched := false
	var creationWitness *postgresCreationWitness
	if p.Type() == "postgresql" && opts.TargetPreparation.Disposition == plugin.TableTargetCreated {
		if expected := decodePostgresCreationWitness(opts.TargetPreparation.CreationWitness); expected != nil && expected.EngineID == uint64(path.EngineID) && expected.Schema == schema && expected.Table == table {
			current := observePostgresCreationWitness(ctx, tx, schema, table, true)
			if current != nil {
				current.EngineID = uint64(path.EngineID)
			}
			creationMatched = samePostgresCreatedTable(expected, current)
			if creationMatched {
				creationWitness = expected
			}
		}
	}
	if opts.Replace {
		// Keep deletion in the COPY transaction: abort, preparation and encoding
		// failures must restore every previous row without replacing the table.
		target := commonquery.ForDialect(p.SQLDialect()).QualifiedTable(schema, table)
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+target); err != nil {
			tx.Rollback()
			db.Close()
			return nil, fmt.Errorf("replace postgresql table rows: %w", err)
		}
	}
	stmt, err := tx.PrepareContext(ctx, pq.CopyInSchema(schema, table, columns...))
	if err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("prepare postgresql copy session: %w", err)
	}

	return &postgresTableWriteSession{
		db:              db,
		tx:              tx,
		stmt:            stmt,
		schema:          schema,
		table:           table,
		columns:         columns,
		fieldTypes:      shared.TableWriteFieldTypes(opts.Fields),
		creationMatched: creationMatched,
		creationWitness: creationWitness,
	}, nil
}

func shouldUseCopyWriteMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "copy", "postgres_copy":
		return true
	default:
		return false
	}
}

func fieldColumns(fields []datatype.FieldInfo) []string {
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

type postgresTableWriteSession struct {
	db               *sql.DB
	tx               *sql.Tx
	stmt             *sql.Stmt
	schema           string
	table            string
	columns          []string
	fieldTypes       map[string]datatype.FieldType
	batchesWritten   int64
	rowsWritten      int64
	commitMarker     *resume.Marker
	closed           bool
	writeErr         error
	creationMatched  bool
	creationWitness  *postgresCreationWitness
	createdCommitted bool
}

func (s *postgresTableWriteSession) WriteBatch(ctx context.Context, batch *plugin.BatchData) (err error) {
	if s.closed {
		return fmt.Errorf("postgresql table write session is closed")
	}
	if s.writeErr != nil {
		return s.writeErr
	}
	// Client-side encoding errors do not invalidate the SQL transaction. Retain
	// them so Close cannot commit a partial batch or a preceding replacement.
	defer func() {
		if err != nil {
			err = plugin.NewTableWriteError(plugin.TableWriteFailed, err)
			s.writeErr = err
		}
	}()
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	for rowIndex, row := range batch.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		values, err := postgresWriteRow(row, s.columns, s.fieldTypes)
		if err != nil {
			return fmt.Errorf("copy postgresql row %d: %w", rowIndex, err)
		}
		if _, err := s.stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("copy postgresql row: %w", err)
		}
	}
	s.batchesWritten++
	s.rowsWritten += int64(len(batch.Rows))
	return nil
}

func (s *postgresTableWriteSession) Close(ctx context.Context) (err error) {
	defer func() {
		err = plugin.NewTableWriteError(plugin.TableWriteFailed, err)
		if err != nil {
			s.writeErr = err
		}
	}()
	if s.closed {
		return s.writeErr
	}
	s.closed = true
	if s.writeErr != nil {
		_ = s.abort()
		return s.writeErr
	}

	if _, err := s.stmt.ExecContext(ctx); err != nil {
		_ = s.abort()
		return fmt.Errorf("finalize postgresql copy session: %w", err)
	}
	if err := s.stmt.Close(); err != nil {
		_ = s.abort()
		return fmt.Errorf("close postgresql copy session statement: %w", err)
	}
	// Detect observed path changes without changing the write strategy. Schema
	// maintenance must not overlap tasks; this is not a namespace DDL lock or
	// an atomic guarantee against arbitrary external DDL after this observation.
	if s.creationMatched {
		current := observePostgresCreationWitness(ctx, s.tx, s.schema, s.table, false)
		if current != nil {
			current.EngineID = s.creationWitness.EngineID
		}
		s.creationMatched = samePostgresCreatedTable(s.creationWitness, current)
	}
	if err := s.tx.Commit(); err != nil {
		_ = s.db.Close()
		return fmt.Errorf("commit postgresql copy session: %w", err)
	}
	s.commitMarker = s.buildCommitMarker()
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close postgresql copy session connection: %w", err)
	}
	s.createdCommitted = s.creationMatched
	return nil
}

func (s *postgresTableWriteSession) CreatedTableCommitted() bool {
	return s != nil && s.createdCommitted && s.writeErr == nil
}

func (s *postgresTableWriteSession) CommitMarker() *resume.Marker {
	if s == nil {
		return nil
	}
	return s.commitMarker.Clone()
}

func (s *postgresTableWriteSession) Abort(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.abort()
}

func (s *postgresTableWriteSession) abort() error {
	var abortErr error
	if s.stmt != nil {
		if err := s.stmt.Close(); err != nil && abortErr == nil {
			abortErr = fmt.Errorf("close postgresql copy session statement: %w", err)
		}
	}
	if s.tx != nil {
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && abortErr == nil {
			abortErr = fmt.Errorf("rollback postgresql copy session: %w", err)
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && abortErr == nil {
			abortErr = fmt.Errorf("close postgresql copy session connection: %w", err)
		}
	}
	return abortErr
}

func (s *postgresTableWriteSession) buildCommitMarker() *resume.Marker {
	return &resume.Marker{
		Version:      resume.MarkerVersionV1,
		Provider:     postgresTableWriteSessionMarkerProvider,
		PositionUnit: postgresTableWriteSessionMarkerPositionUnit,
		CommitPosition: map[string]interface{}{
			"rows_committed":    s.rowsWritten,
			"batches_committed": s.batchesWritten,
		},
		Fingerprint: map[string]interface{}{
			"target":  strings.Trim(s.schema+"/"+s.table, "/"),
			"schema":  s.schema,
			"table":   s.table,
			"columns": append([]string(nil), s.columns...),
			"method":  "postgres_copy",
		},
	}
}
