package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	pluginshared "github.com/addp/common/engine/plugins/shared"
	"github.com/google/uuid"
)

const mysqlTransferApplyLedgerTable = "_addp_transfer_apply_positions"

type mysqlApplyLedgerPosition struct {
	SourceIdentity  string
	TargetIdentity  string
	PositionType    string
	PositionVersion string
	NextOffset      int64
}

func (p *MySQLPlugin) PreparePartitionedTableChangeApply(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.PartitionedTableChangeApplyOptions) error {
	if err := validateMySQLPartitionedTableChangeApplyOptions(opts); err != nil {
		return err
	}
	database, table, err := mysqlTablePathParts(path)
	if err != nil {
		return err
	}
	if table == mysqlTransferApplyLedgerTable {
		return fmt.Errorf("mysql target table name %q is reserved for the transfer apply ledger", table)
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build mysql dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open mysql transfer apply ledger: %w", err)
	}
	defer db.Close()
	exists, err := mysqlBaseTableExists(ctx, db, database, table)
	if err != nil {
		return err
	}
	if opts.RequireTargetAbsent && exists {
		return fmt.Errorf("mysql replay target %s.%s already exists", database, table)
	}
	if err := ensureMySQLTransferApplyLedger(ctx, db, database); err != nil {
		return fmt.Errorf("prepare mysql transfer apply ledger: %w", err)
	}
	if err := p.prepareTableUpsert(ctx, connInfo, path, plugin.TableUpsertOptions{
		Fields: opts.Fields, SpatialInfo: opts.SpatialInfo, Keys: opts.Keys,
	}, opts.RequireTargetAbsent); err != nil {
		return err
	}
	return nil
}

func (p *MySQLPlugin) ApplyPartitionedTableChanges(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.PartitionedTableChangeBatch, opts plugin.PartitionedTableChangeApplyOptions) (*plugin.PartitionedTableChangeApplyResult, error) {
	keys, err := validateMySQLPartitionedTableChangeApplyBatch(batch, opts)
	if err != nil {
		return nil, err
	}
	database, table, err := mysqlTablePathParts(path)
	if err != nil {
		return nil, err
	}
	if table == mysqlTransferApplyLedgerTable {
		return nil, fmt.Errorf("mysql target table name %q is reserved for the transfer apply ledger", table)
	}
	targetIdentity := database + "." + table
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

	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build mysql dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql partitioned change apply: %w", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin mysql partitioned change apply: %w", err)
	}
	defer tx.Rollback()

	if err := insertMySQLApplyLedgerStart(ctx, tx, database, opts, targetIdentity, batch.Partition, startOffset); err != nil {
		return nil, err
	}
	ledger, err := lockMySQLApplyLedger(ctx, tx, database, opts.ApplyIdentity, batch.Partition)
	if err != nil {
		return nil, err
	}
	if err := validateMySQLApplyLedgerIdentity(ledger, opts.SourceIdentity, targetIdentity); err != nil {
		return nil, err
	}
	if ledger.NextOffset < startOffset {
		return nil, fmt.Errorf("mysql target apply ledger gap for partition %q: ledger next_offset=%d, batch start=%d", batch.Partition, ledger.NextOffset, startOffset)
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
			if err := upsertMySQLRowsTx(ctx, tx, database, table, data, plugin.TableUpsertOptions{
				Fields: opts.Fields, SpatialInfo: opts.SpatialInfo, Keys: keys,
			}, keys); err != nil {
				return nil, err
			}
		}
		if len(deleteRows) > 0 {
			if err := deleteMySQLRowsTx(ctx, tx, database, table, deleteRows, keys); err != nil {
				return nil, err
			}
		}
	}
	committedOffset := ledger.NextOffset
	if endOffset > committedOffset {
		if err := updateMySQLApplyLedger(ctx, tx, database, opts.ApplyIdentity, batch.Partition, endOffset); err != nil {
			return nil, err
		}
		committedOffset = endOffset
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit mysql partitioned change apply: %w", err)
	}
	return &plugin.PartitionedTableChangeApplyResult{
		AppliedRecords: len(changes),
		SkippedRecords: skipped,
		Position:       pluginshared.KafkaOffsetPosition(batch.Partition, committedOffset),
	}, nil
}

func validateMySQLPartitionedTableChangeApplyOptions(opts plugin.PartitionedTableChangeApplyOptions) error {
	if _, err := uuid.Parse(strings.TrimSpace(opts.ApplyIdentity)); err != nil {
		return fmt.Errorf("mysql partitioned change apply requires valid apply_identity UUID")
	}
	if strings.TrimSpace(opts.SourceIdentity) == "" {
		return fmt.Errorf("mysql partitioned change apply requires source identity")
	}
	_, err := validateMySQLUpsertOptions(plugin.TableUpsertOptions{Fields: opts.Fields, SpatialInfo: opts.SpatialInfo, Keys: opts.Keys})
	return err
}

func validateMySQLPartitionedTableChangeApplyBatch(batch *plugin.PartitionedTableChangeBatch, opts plugin.PartitionedTableChangeApplyOptions) ([]string, error) {
	if err := validateMySQLPartitionedTableChangeApplyOptions(opts); err != nil {
		return nil, err
	}
	if batch == nil {
		return nil, fmt.Errorf("mysql partitioned change apply requires batch")
	}
	if strings.TrimSpace(batch.Partition) == "" {
		return nil, fmt.Errorf("mysql partitioned change apply requires partition")
	}
	return validateMySQLUpsertOptions(plugin.TableUpsertOptions{Fields: opts.Fields, SpatialInfo: opts.SpatialInfo, Keys: opts.Keys})
}

func ensureMySQLTransferApplyLedger(ctx context.Context, db *sql.DB, database string) error {
	dialect := mysqlDialect()
	qualified := dialect.QualifiedTable(database, mysqlTransferApplyLedgerTable)
	statement := `CREATE TABLE IF NOT EXISTS ` + qualified + ` (
		apply_identity CHAR(36) NOT NULL,
		source_identity TEXT NOT NULL,
		target_identity TEXT NOT NULL,
		partition_key VARCHAR(255) NOT NULL,
		position_type VARCHAR(50) NOT NULL,
		position_version VARCHAR(20) NOT NULL,
		next_offset BIGINT UNSIGNED NOT NULL,
		created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
		PRIMARY KEY (apply_identity, partition_key)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return err
	}
	return validateMySQLTransferApplyLedger(ctx, db, database)
}

func validateMySQLTransferApplyLedger(ctx context.Context, db *sql.DB, database string) error {
	var engine sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT engine FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ? AND table_type = 'BASE TABLE'
	`, database, mysqlTransferApplyLedgerTable).Scan(&engine); err != nil {
		return err
	}
	if !engine.Valid || !strings.EqualFold(strings.TrimSpace(engine.String), "InnoDB") {
		return fmt.Errorf("mysql transfer apply ledger must use InnoDB, got %q", engine.String)
	}
	columns, err := mysqlTableColumns(ctx, db, database, mysqlTransferApplyLedgerTable)
	if err != nil {
		return err
	}
	expected := []struct {
		name       string
		nativeType string
	}{
		{name: "apply_identity", nativeType: "char(36)"},
		{name: "source_identity", nativeType: "text"},
		{name: "target_identity", nativeType: "text"},
		{name: "partition_key", nativeType: "varchar(255)"},
		{name: "position_type", nativeType: "varchar(50)"},
		{name: "position_version", nativeType: "varchar(20)"},
		{name: "next_offset", nativeType: "bigint unsigned"},
		{name: "created_at", nativeType: "datetime(6)"},
		{name: "updated_at", nativeType: "datetime(6)"},
	}
	if len(columns) != len(expected) {
		return fmt.Errorf("mysql transfer apply ledger has %d columns, expected %d", len(columns), len(expected))
	}
	for i, column := range columns {
		if column.Name != expected[i].name || !strings.EqualFold(mysqlColumnNativeType(column), expected[i].nativeType) || column.Nullable {
			return fmt.Errorf("mysql transfer apply ledger column %d is %q %q nullable=%t, expected %q %q nullable=false", i+1, column.Name, mysqlColumnNativeType(column), column.Nullable, expected[i].name, expected[i].nativeType)
		}
	}
	indexes, err := mysqlUniqueIndexes(ctx, db, database, mysqlTransferApplyLedgerTable)
	if err != nil {
		return err
	}
	if !mysqlUniqueIndexesCompatible([]string{"apply_identity", "partition_key"}, indexes) {
		return fmt.Errorf("mysql transfer apply ledger primary key must be (apply_identity, partition_key)")
	}
	return nil
}

func insertMySQLApplyLedgerStart(ctx context.Context, tx *sql.Tx, database string, opts plugin.PartitionedTableChangeApplyOptions, targetIdentity, partition string, startOffset int64) error {
	qualified := mysqlDialect().QualifiedTable(database, mysqlTransferApplyLedgerTable)
	_, err := tx.ExecContext(ctx, `INSERT INTO `+qualified+`
		(apply_identity, source_identity, target_identity, partition_key, position_type, position_version, next_offset)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE apply_identity = apply_identity`,
		opts.ApplyIdentity, opts.SourceIdentity, targetIdentity, partition,
		plugin.ChangeStreamPositionTypeKafkaOffset, plugin.ChangeStreamPositionVersionV1, startOffset,
	)
	if err != nil {
		return fmt.Errorf("initialize mysql target apply ledger: %w", err)
	}
	return nil
}

func lockMySQLApplyLedger(ctx context.Context, tx *sql.Tx, database, applyIdentity, partition string) (*mysqlApplyLedgerPosition, error) {
	qualified := mysqlDialect().QualifiedTable(database, mysqlTransferApplyLedgerTable)
	var ledger mysqlApplyLedgerPosition
	err := tx.QueryRowContext(ctx, `SELECT source_identity, target_identity, position_type, position_version, next_offset
		FROM `+qualified+` WHERE apply_identity = ? AND partition_key = ? FOR UPDATE`, applyIdentity, partition).Scan(
		&ledger.SourceIdentity, &ledger.TargetIdentity, &ledger.PositionType, &ledger.PositionVersion, &ledger.NextOffset,
	)
	if err != nil {
		return nil, fmt.Errorf("lock mysql target apply ledger: %w", err)
	}
	return &ledger, nil
}

func validateMySQLApplyLedgerIdentity(ledger *mysqlApplyLedgerPosition, sourceIdentity, targetIdentity string) error {
	if ledger.SourceIdentity != sourceIdentity || ledger.TargetIdentity != targetIdentity {
		return fmt.Errorf("mysql target apply identity drift: ledger source=%q target=%q, batch source=%q target=%q", ledger.SourceIdentity, ledger.TargetIdentity, sourceIdentity, targetIdentity)
	}
	if ledger.PositionType != plugin.ChangeStreamPositionTypeKafkaOffset || ledger.PositionVersion != plugin.ChangeStreamPositionVersionV1 {
		return fmt.Errorf("unsupported mysql target apply ledger position %s/%s", ledger.PositionType, ledger.PositionVersion)
	}
	return nil
}

func updateMySQLApplyLedger(ctx context.Context, tx *sql.Tx, database, applyIdentity, partition string, nextOffset int64) error {
	qualified := mysqlDialect().QualifiedTable(database, mysqlTransferApplyLedgerTable)
	result, err := tx.ExecContext(ctx, `UPDATE `+qualified+`
		SET next_offset = ?, updated_at = CURRENT_TIMESTAMP(6)
		WHERE apply_identity = ? AND partition_key = ? AND next_offset <= ?`, nextOffset, applyIdentity, partition, nextOffset)
	if err != nil {
		return fmt.Errorf("advance mysql target apply ledger: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read mysql target apply ledger update result: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("mysql target apply ledger was not advanced")
	}
	return nil
}

func deleteMySQLRowsTx(ctx context.Context, tx *sql.Tx, database, table string, rows []map[string]interface{}, keys []string) error {
	if len(rows) == 0 {
		return nil
	}
	dialect := mysqlDialect()
	quotedKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		quotedKeys = append(quotedKeys, dialect.QuoteIdentifier(key))
	}
	chunkSize := effectiveMySQLInsertChunkSize(len(keys), mysqlDefaultInsertChunkSize)
	if chunkSize <= 0 {
		return fmt.Errorf("mysql delete has too many key columns for bind parameters")
	}
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
					return fmt.Errorf("mysql delete row is missing non-null key field %q", key)
				}
				args = append(args, value)
				placeholders = append(placeholders, "?")
			}
			tuples = append(tuples, "("+strings.Join(placeholders, ", ")+")")
		}
		statement := "DELETE FROM " + dialect.QualifiedTable(database, table) +
			" WHERE (" + strings.Join(quotedKeys, ", ") + ") IN (" + strings.Join(tuples, ", ") + ")"
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("execute mysql delete rows %d-%d: %w", start, end, err)
		}
	}
	return nil
}
