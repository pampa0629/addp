package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	pluginshared "github.com/addp/common/engine/plugins/shared"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
)

const postgresTransferLedgerSchema = "addp_transfer"
const postgresTransferLedgerTable = "apply_positions"

type postgresApplyLedgerPosition struct {
	SourceIdentity  string
	TargetIdentity  string
	PositionType    string
	PositionVersion string
	NextOffset      int64
}

func (p *PostgreSQLPlugin) PreparePartitionedTableChangeApply(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.PartitionedTableChangeApplyOptions) error {
	if err := validatePartitionedTableChangeApplyOptions(opts); err != nil {
		return err
	}
	if err := p.prepareTableUpsert(ctx, connInfo, path, plugin.TableUpsertOptions{
		Fields: opts.Fields, SpatialInfo: opts.SpatialInfo, Keys: opts.Keys,
	}, opts.RequireTargetAbsent); err != nil {
		return err
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensurePostgresTransferApplyLedger(ctx, db); err != nil {
		return fmt.Errorf("prepare postgresql transfer apply ledger: %w", err)
	}
	return nil
}

func (p *PostgreSQLPlugin) ApplyPartitionedTableChanges(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, batch *plugin.PartitionedTableChangeBatch, opts plugin.PartitionedTableChangeApplyOptions) (*plugin.PartitionedTableChangeApplyResult, error) {
	keys, err := validatePartitionedTableChangeApplyBatch(batch, opts)
	if err != nil {
		return nil, err
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return nil, err
	}
	targetIdentity := schema + "." + table
	startOffset, err := pluginshared.KafkaNextOffset(batch.StartPosition, batch.Partition)
	if err != nil {
		return nil, fmt.Errorf("invalid start position: %w", err)
	}
	endOffset, err := pluginshared.KafkaNextOffset(batch.EndPosition, batch.Partition)
	if err != nil {
		return nil, fmt.Errorf("invalid end position: %w", err)
	}
	if endOffset < startOffset {
		return nil, fmt.Errorf("partitioned table change batch end offset %d is before start offset %d", endOffset, startOffset)
	}

	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin postgresql partitioned change apply: %w", err)
	}
	defer tx.Rollback()

	if err := insertPostgresApplyLedgerStart(ctx, tx, opts, targetIdentity, batch.Partition, startOffset); err != nil {
		return nil, err
	}
	ledger, err := lockPostgresApplyLedger(ctx, tx, opts.ApplyIdentity, batch.Partition)
	if err != nil {
		return nil, err
	}
	if err := validatePostgresApplyLedgerIdentity(ledger, opts.SourceIdentity, targetIdentity); err != nil {
		return nil, err
	}
	if ledger.NextOffset < startOffset {
		return nil, fmt.Errorf("postgresql target apply ledger gap for partition %q: ledger next_offset=%d, batch start=%d", batch.Partition, ledger.NextOffset, startOffset)
	}

	changes, skipped, err := pluginshared.FilterAndCoalesceTableChanges(batch, keys, startOffset, ledger.NextOffset, endOffset)
	if err != nil {
		return nil, err
	}
	if len(changes) > 0 {
		upsertRows := make([]map[string]interface{}, 0, len(changes))
		deleteRows := make([]map[string]interface{}, 0, len(changes))
		for _, change := range changes {
			switch change.Operation {
			case plugin.TableChangeOperationUpsert:
				upsertRows = append(upsertRows, change.Row)
			case plugin.TableChangeOperationDelete:
				deleteRows = append(deleteRows, change.Row)
			default:
				return nil, fmt.Errorf("unsupported coalesced table change operation %q", change.Operation)
			}
		}
		if len(upsertRows) > 0 {
			data := &plugin.BatchData{Fields: opts.Fields, Spatial: opts.SpatialInfo, Rows: upsertRows}
			if err := upsertPostgresRowsTx(ctx, tx, schema, table, data, keys); err != nil {
				return nil, err
			}
		}
		if len(deleteRows) > 0 {
			if err := deletePostgresRowsTx(ctx, tx, schema, table, deleteRows, keys); err != nil {
				return nil, err
			}
		}
	}
	committedOffset := ledger.NextOffset
	if endOffset > committedOffset {
		if err := updatePostgresApplyLedger(ctx, tx, opts.ApplyIdentity, batch.Partition, endOffset); err != nil {
			return nil, err
		}
		committedOffset = endOffset
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit postgresql partitioned change apply: %w", err)
	}
	return &plugin.PartitionedTableChangeApplyResult{
		AppliedRecords: len(changes),
		SkippedRecords: skipped,
		Position:       pluginshared.KafkaOffsetPosition(batch.Partition, committedOffset),
	}, nil
}

func validatePartitionedTableChangeApplyOptions(opts plugin.PartitionedTableChangeApplyOptions) error {
	if _, err := uuid.Parse(strings.TrimSpace(opts.ApplyIdentity)); err != nil {
		return fmt.Errorf("postgresql partitioned change apply requires valid apply_identity UUID")
	}
	if strings.TrimSpace(opts.SourceIdentity) == "" {
		return fmt.Errorf("postgresql partitioned change apply requires source identity")
	}
	_, err := validatePostgresUpsertOptions(plugin.TableUpsertOptions{Fields: opts.Fields, SpatialInfo: opts.SpatialInfo, Keys: opts.Keys})
	return err
}

func validatePartitionedTableChangeApplyBatch(batch *plugin.PartitionedTableChangeBatch, opts plugin.PartitionedTableChangeApplyOptions) ([]string, error) {
	if err := validatePartitionedTableChangeApplyOptions(opts); err != nil {
		return nil, err
	}
	if batch == nil {
		return nil, fmt.Errorf("postgresql partitioned change apply requires batch")
	}
	if strings.TrimSpace(batch.Partition) == "" {
		return nil, fmt.Errorf("postgresql partitioned change apply requires partition")
	}
	keys, err := validatePostgresUpsertOptions(plugin.TableUpsertOptions{Fields: opts.Fields, SpatialInfo: opts.SpatialInfo, Keys: opts.Keys})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func ensurePostgresTransferApplyLedger(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE SCHEMA IF NOT EXISTS addp_transfer`,
		`CREATE TABLE IF NOT EXISTS addp_transfer.apply_positions (
			apply_identity uuid NOT NULL,
			source_identity text NOT NULL,
			target_identity text NOT NULL,
			partition text NOT NULL,
			position_type varchar(50) NOT NULL,
			position_version varchar(20) NOT NULL,
			next_offset bigint NOT NULL CHECK (next_offset >= 0),
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (apply_identity, partition)
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func insertPostgresApplyLedgerStart(ctx context.Context, tx *sql.Tx, opts plugin.PartitionedTableChangeApplyOptions, targetIdentity, partition string, startOffset int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO addp_transfer.apply_positions
			(apply_identity, source_identity, target_identity, partition, position_type, position_version, next_offset)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (apply_identity, partition) DO NOTHING`,
		opts.ApplyIdentity, opts.SourceIdentity, targetIdentity, partition,
		plugin.ChangeStreamPositionTypeKafkaOffset, plugin.ChangeStreamPositionVersionV1, startOffset,
	)
	if err != nil {
		return fmt.Errorf("initialize postgresql target apply ledger: %w", err)
	}
	return nil
}

func lockPostgresApplyLedger(ctx context.Context, tx *sql.Tx, applyIdentity, partition string) (*postgresApplyLedgerPosition, error) {
	var ledger postgresApplyLedgerPosition
	err := tx.QueryRowContext(ctx, `
		SELECT source_identity, target_identity, position_type, position_version, next_offset
		FROM addp_transfer.apply_positions
		WHERE apply_identity = $1::uuid AND partition = $2
		FOR UPDATE`, applyIdentity, partition).Scan(
		&ledger.SourceIdentity, &ledger.TargetIdentity, &ledger.PositionType, &ledger.PositionVersion, &ledger.NextOffset,
	)
	if err != nil {
		return nil, fmt.Errorf("lock postgresql target apply ledger: %w", err)
	}
	return &ledger, nil
}

func validatePostgresApplyLedgerIdentity(ledger *postgresApplyLedgerPosition, sourceIdentity, targetIdentity string) error {
	if ledger.SourceIdentity != sourceIdentity || ledger.TargetIdentity != targetIdentity {
		return fmt.Errorf("postgresql target apply identity drift: ledger source=%q target=%q, batch source=%q target=%q", ledger.SourceIdentity, ledger.TargetIdentity, sourceIdentity, targetIdentity)
	}
	if ledger.PositionType != plugin.ChangeStreamPositionTypeKafkaOffset || ledger.PositionVersion != plugin.ChangeStreamPositionVersionV1 {
		return fmt.Errorf("unsupported postgresql target apply ledger position %s/%s", ledger.PositionType, ledger.PositionVersion)
	}
	return nil
}

func updatePostgresApplyLedger(ctx context.Context, tx *sql.Tx, applyIdentity, partition string, nextOffset int64) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE addp_transfer.apply_positions
		SET next_offset = $3, updated_at = now()
		WHERE apply_identity = $1::uuid AND partition = $2 AND next_offset <= $3`, applyIdentity, partition, nextOffset)
	if err != nil {
		return fmt.Errorf("advance postgresql target apply ledger: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read postgresql target apply ledger update result: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("postgresql target apply ledger was not advanced")
	}
	return nil
}

func deletePostgresRowsTx(ctx context.Context, tx *sql.Tx, schema, table string, rows []map[string]interface{}, keys []string) error {
	if len(rows) == 0 {
		return nil
	}
	dialect := commonquery.ForEngine("postgresql")
	quotedKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		quotedKeys = append(quotedKeys, dialect.QuoteIdentifier(key))
	}
	chunkSize := effectivePostgresInsertChunkSize(len(keys), postgresDefaultInsertChunkSize)
	for start := 0; start < len(rows); start += chunkSize {
		end := start + chunkSize
		if end > len(rows) {
			end = len(rows)
		}
		args := make([]interface{}, 0, (end-start)*len(keys))
		tuples := make([]string, 0, end-start)
		for _, row := range rows[start:end] {
			placeholders := make([]string, 0, len(keys))
			for _, key := range keys {
				value, ok := row[key]
				if !ok || value == nil {
					return fmt.Errorf("postgresql delete row is missing non-null key field %q", key)
				}
				args = append(args, value)
				placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
			}
			tuples = append(tuples, "("+strings.Join(placeholders, ", ")+")")
		}
		statement := "DELETE FROM " + dialect.QuoteIdentifier(schema) + "." + dialect.QuoteIdentifier(table) +
			" WHERE (" + strings.Join(quotedKeys, ", ") + ") IN (" + strings.Join(tuples, ", ") + ")"
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("execute postgresql delete rows %d-%d: %w", start, end, err)
		}
	}
	return nil
}
