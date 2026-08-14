package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pluginshared "github.com/addp/common/engine/plugins/shared"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
	go_ora "github.com/sijms/go-ora/v2"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkb"
)

const oracleTransferApplyLedgerTable = "_ADDP_TRANSFER_APPLY_POSITIONS"

type oracleApplyLedgerPosition struct {
	SourceIdentity  string
	TargetIdentity  string
	PositionType    string
	PositionVersion string
	NextOffset      int64
}

func (p *OraclePlugin) PreparePartitionedTableChangeApply(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.PartitionedTableChangeApplyOptions) error {
	keys, err := validateOraclePartitionedTableChangeApplyOptions(opts)
	if err != nil {
		return err
	}
	schema, table, err := oracleTablePathParts(path)
	if err != nil {
		return err
	}
	if strings.EqualFold(table, oracleTransferApplyLedgerTable) {
		return fmt.Errorf("oracle target table name %q is reserved for the transfer apply ledger", table)
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("build Oracle partitioned change apply DSN: %w", err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return fmt.Errorf("open Oracle partitioned change apply connection: %w", err)
	}
	defer db.Close()
	if err := validateOracleTargetSchema(ctx, db, schema); err != nil {
		return err
	}
	exists, err := oracleBaseTableExists(ctx, db, schema, table)
	if err != nil {
		return err
	}
	if opts.RequireTargetAbsent && exists {
		return fmt.Errorf("oracle replay target %s.%s already exists", schema, table)
	}
	if err := ensureOracleTransferApplyLedger(ctx, db, schema); err != nil {
		return fmt.Errorf("prepare Oracle transfer apply ledger: %w", err)
	}
	fields := oracleApplyFields(opts.Fields, keys, !exists)
	if !exists {
		if err := createOracleTable(ctx, db, schema, table, fields); err != nil {
			return err
		}
	} else if err := evolveOracleTable(ctx, db, schema, table, fields, opts.SpatialInfo); err != nil {
		return err
	}
	return validateOracleApplyTargetKeys(ctx, db, schema, table, keys)
}

func (p *OraclePlugin) ApplyPartitionedTableChanges(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, batch *plugin.PartitionedTableChangeBatch, opts plugin.PartitionedTableChangeApplyOptions) (*plugin.PartitionedTableChangeApplyResult, error) {
	keys, err := validateOraclePartitionedTableChangeApplyBatch(batch, opts)
	if err != nil {
		return nil, err
	}
	schema, table, err := oracleTablePathParts(path)
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
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return nil, fmt.Errorf("open Oracle partitioned change apply connection: %w", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin Oracle partitioned change apply: %w", err)
	}
	defer tx.Rollback()
	if err := insertOracleApplyLedgerStart(ctx, tx, schema, opts, targetIdentity, batch.Partition, startOffset); err != nil {
		return nil, err
	}
	ledger, err := lockOracleApplyLedger(ctx, tx, schema, opts.ApplyIdentity, batch.Partition)
	if err != nil {
		return nil, err
	}
	if err := validateOracleApplyLedgerIdentity(ledger, opts.SourceIdentity, targetIdentity); err != nil {
		return nil, err
	}
	if ledger.NextOffset < startOffset {
		return nil, fmt.Errorf("oracle target apply ledger gap for partition %q: ledger next_offset=%d, batch start=%d", batch.Partition, ledger.NextOffset, startOffset)
	}
	changes, skipped, err := pluginshared.FilterAndCoalesceTableChanges(batch, keys, startOffset, ledger.NextOffset, endOffset)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		switch change.Operation {
		case plugin.TableChangeOperationUpsert:
			if err := mergeOracleApplyRow(ctx, tx, schema, table, change.Row, opts.Fields, opts.SpatialInfo, keys); err != nil {
				return nil, err
			}
		case plugin.TableChangeOperationDelete:
			if err := deleteOracleApplyRow(ctx, tx, schema, table, change.Row, keys); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported coalesced table change operation %q", change.Operation)
		}
	}
	committedOffset := ledger.NextOffset
	if endOffset > committedOffset {
		if err := updateOracleApplyLedger(ctx, tx, schema, opts.ApplyIdentity, batch.Partition, endOffset); err != nil {
			return nil, err
		}
		committedOffset = endOffset
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Oracle partitioned change apply: %w", err)
	}
	return &plugin.PartitionedTableChangeApplyResult{
		AppliedRecords: len(changes),
		SkippedRecords: skipped,
		Position:       pluginshared.KafkaOffsetPosition(batch.Partition, committedOffset),
	}, nil
}

func validateOraclePartitionedTableChangeApplyOptions(opts plugin.PartitionedTableChangeApplyOptions) ([]string, error) {
	if _, err := uuid.Parse(strings.TrimSpace(opts.ApplyIdentity)); err != nil {
		return nil, fmt.Errorf("oracle partitioned change apply requires valid apply_identity UUID")
	}
	if strings.TrimSpace(opts.SourceIdentity) == "" {
		return nil, fmt.Errorf("oracle partitioned change apply requires source identity")
	}
	fields, err := validateOracleWriteFields(opts.Fields, opts.SpatialInfo)
	if err != nil {
		return nil, err
	}
	fieldSet := make(map[string]bool, len(fields))
	for _, field := range fields {
		fieldSet[field.Name] = true
	}
	keys := make([]string, 0, len(opts.Keys))
	seen := map[string]bool{}
	for _, key := range opts.Keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			return nil, fmt.Errorf("oracle partitioned change apply keys must be non-empty and unique")
		}
		if !fieldSet[key] {
			return nil, fmt.Errorf("oracle partitioned change apply key %q is not present in table fields", key)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("oracle partitioned change apply requires keys")
	}
	return keys, nil
}

func validateOraclePartitionedTableChangeApplyBatch(batch *plugin.PartitionedTableChangeBatch, opts plugin.PartitionedTableChangeApplyOptions) ([]string, error) {
	keys, err := validateOraclePartitionedTableChangeApplyOptions(opts)
	if err != nil {
		return nil, err
	}
	if batch == nil {
		return nil, fmt.Errorf("oracle partitioned change apply requires batch")
	}
	if strings.TrimSpace(batch.Partition) == "" {
		return nil, fmt.Errorf("oracle partitioned change apply requires partition")
	}
	return keys, nil
}

func validateOracleWriteFields(fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) ([]datatype.FieldInfo, error) {
	result := make([]datatype.FieldInfo, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		field.Name = strings.TrimSpace(field.Name)
		if field.Name == "" || seen[field.Name] {
			return nil, fmt.Errorf("oracle table write fields must have non-empty unique names")
		}
		if len([]byte(field.Name)) > 128 {
			return nil, fmt.Errorf("oracle field name %q exceeds the 128-byte identifier limit", field.Name)
		}
		if _, err := oracleSQLTypeForField(field); err != nil {
			return nil, err
		}
		if datatype.IsSpatialFieldType(field.Type) && oracleSpatialColumnForField(spatialInfo, field.Name) == nil {
			return nil, fmt.Errorf("oracle geometry field %q requires frozen spatial facts", field.Name)
		}
		if datatype.IsSpatialFieldType(field.Type) {
			column := oracleSpatialColumnForField(spatialInfo, field.Name)
			if column.Dimension == nil || *column.Dimension != 2 {
				return nil, fmt.Errorf("oracle target geometry field %q requires frozen XY dimension 2", field.Name)
			}
		}
		seen[field.Name] = true
		result = append(result, field)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("oracle table write requires fields")
	}
	return result, nil
}

func oracleApplyFields(fields []datatype.FieldInfo, keys []string, targetAbsent bool) []datatype.FieldInfo {
	result := append([]datatype.FieldInfo(nil), fields...)
	if !targetAbsent {
		return result
	}
	keySet := make(map[string]bool, len(keys))
	for _, key := range keys {
		keySet[key] = true
	}
	for i := range result {
		if keySet[result[i].Name] {
			result[i].PrimaryKey = true
			result[i].Nullable = false
		}
	}
	return result
}

func oracleTablePathParts(path plugin.CatalogPath) (string, string, error) {
	segments := plugin.CatalogPathWithoutRoot(path).Segments
	if len(segments) < 2 {
		return "", "", fmt.Errorf("oracle table operation requires schema/table catalog path")
	}
	schema := strings.TrimSpace(segments[len(segments)-2].Name)
	table := strings.TrimSpace(segments[len(segments)-1].Name)
	if schema == "" || table == "" {
		return "", "", fmt.Errorf("oracle table operation requires non-empty schema and table")
	}
	for role, value := range map[string]string{"schema": schema, "table": table} {
		if len([]byte(value)) > 128 {
			return "", "", fmt.Errorf("oracle %s name %q exceeds the 128-byte identifier limit", role, value)
		}
	}
	return schema, table, nil
}

func validateOracleTargetSchema(ctx context.Context, db *sql.DB, schema string) error {
	if _, system := oracleSystemSchemas[strings.ToUpper(strings.TrimSpace(schema))]; system {
		return fmt.Errorf("oracle system schema %q cannot be used as a table write target", schema)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM all_users WHERE username = :1", schema).Scan(&count); err != nil {
		return fmt.Errorf("check Oracle target schema %s: %w", schema, err)
	}
	if count != 1 {
		return fmt.Errorf("oracle target schema %s does not exist", schema)
	}
	var currentUser string
	if err := db.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&currentUser); err != nil {
		return fmt.Errorf("read Oracle target session user: %w", err)
	}
	if !strings.EqualFold(currentUser, schema) {
		return fmt.Errorf("oracle target schema %q must match the connected user %q", schema, currentUser)
	}
	return nil
}

func oracleBaseTableExists(ctx context.Context, db *sql.DB, schema, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM all_tables WHERE owner = :1 AND table_name = :2", schema, table).Scan(&count); err != nil {
		return false, fmt.Errorf("check Oracle target table %s.%s: %w", schema, table, err)
	}
	return count == 1, nil
}

func createOracleTable(ctx context.Context, db *sql.DB, schema, table string, fields []datatype.FieldInfo) error {
	dialect := commonquery.ForEngine("oracle")
	definitions := make([]string, 0, len(fields)+1)
	primaryKeys := make([]string, 0)
	for _, field := range fields {
		definition, err := oracleColumnDefinition(field)
		if err != nil {
			return err
		}
		definitions = append(definitions, definition)
		if field.PrimaryKey {
			primaryKeys = append(primaryKeys, dialect.QuoteIdentifier(field.Name))
		}
	}
	if len(primaryKeys) > 0 {
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(primaryKeys, ", ")+")")
	}
	statement := "CREATE TABLE " + dialect.QualifiedTable(schema, table) + " (" + strings.Join(definitions, ", ") + ")"
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create Oracle target table %s.%s: %w", schema, table, err)
	}
	return nil
}

func evolveOracleTable(ctx context.Context, db *sql.DB, schema, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) error {
	existing, err := (&OraclePlugin{}).listColumnsWithSQL(ctx, db, schema, table)
	if err != nil {
		return err
	}
	byName := make(map[string]datatype.FieldInfo, len(existing))
	for _, field := range existing {
		byName[field.Name] = field
	}
	dialect := commonquery.ForEngine("oracle")
	for _, field := range fields {
		current, ok := byName[field.Name]
		if ok {
			if !oracleColumnCompatible(current, field, spatialInfo) {
				return fmt.Errorf("oracle target column %q has type %q, expected %q", field.Name, current.NativeType, oracleExpectedType(field))
			}
			continue
		}
		if field.PrimaryKey {
			return fmt.Errorf("oracle schema evolution cannot add primary key column %q to existing table", field.Name)
		}
		if !field.Nullable {
			return fmt.Errorf("oracle schema evolution cannot add non-null column %q", field.Name)
		}
		definition, err := oracleColumnDefinition(field)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, "ALTER TABLE "+dialect.QualifiedTable(schema, table)+" ADD ("+definition+")"); err != nil {
			return fmt.Errorf("evolve Oracle target table %s.%s: %w", schema, table, err)
		}
	}
	return nil
}

func oracleColumnDefinition(field datatype.FieldInfo) (string, error) {
	sqlType, err := oracleSQLTypeForField(field)
	if err != nil {
		return "", err
	}
	definition := commonquery.ForEngine("oracle").QuoteIdentifier(field.Name) + " " + sqlType
	if !field.Nullable {
		definition += " NOT NULL"
	}
	return definition, nil
}

func oracleSQLTypeForField(field datatype.FieldInfo) (string, error) {
	switch datatype.ParseFieldType(string(field.Type)) {
	case datatype.FieldTypeString:
		return "VARCHAR2(4000 CHAR)", nil
	case datatype.FieldTypeInt:
		return "NUMBER(9,0)", nil
	case datatype.FieldTypeBigInt:
		return "NUMBER(18,0)", nil
	case datatype.FieldTypeFloat:
		return "BINARY_FLOAT", nil
	case datatype.FieldTypeDouble:
		return "BINARY_DOUBLE", nil
	case datatype.FieldTypeDecimal:
		if field.Precision == 0 && field.Scale == 0 {
			return "NUMBER", nil
		}
		if field.Precision < 1 || field.Precision > 38 || field.Scale < 0 || field.Scale > field.Precision {
			return "", fmt.Errorf("oracle decimal field %q requires precision 1..38 and scale 0..precision", field.Name)
		}
		return fmt.Sprintf("NUMBER(%d,%d)", field.Precision, field.Scale), nil
	case datatype.FieldTypeBool:
		return "BOOLEAN", nil
	case datatype.FieldTypeDate:
		return "DATE", nil
	case datatype.FieldTypeTimestamp:
		return "TIMESTAMP(3)", nil
	case datatype.FieldTypeJSON:
		return "JSON", nil
	case datatype.FieldTypeUUID:
		return "VARCHAR2(36 CHAR)", nil
	case datatype.FieldTypeBytes:
		return "BLOB", nil
	case datatype.FieldTypeGeometry:
		return "MDSYS.SDO_GEOMETRY", nil
	case datatype.FieldTypeTime:
		return "", fmt.Errorf("oracle target does not support time-only field %q", field.Name)
	default:
		return "", fmt.Errorf("oracle target does not support field %q type %q", field.Name, field.Type)
	}
}

func oracleExpectedType(field datatype.FieldInfo) string {
	value, _ := oracleSQLTypeForField(field)
	return value
}

func oracleColumnCompatible(current, expected datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) bool {
	if datatype.IsSpatialFieldType(expected.Type) {
		return datatype.IsSpatialFieldType(current.Type) && oracleSpatialColumnForField(spatialInfo, expected.Name) != nil
	}
	if expected.Type == datatype.FieldTypeDecimal && current.Type == datatype.FieldTypeDecimal {
		return expected.Precision == 0 || (current.Precision == expected.Precision && current.Scale == expected.Scale)
	}
	if expected.Type == datatype.FieldTypeDate {
		return current.Type == datatype.FieldTypeTimestamp && strings.EqualFold(current.NativeType, "DATE")
	}
	if expected.Type == datatype.FieldTypeUUID {
		return current.Type == datatype.FieldTypeString && current.Size == 36
	}
	return current.Type == expected.Type
}

func validateOracleApplyTargetKeys(ctx context.Context, db *sql.DB, schema, table string, keys []string) error {
	query := "SELECT c.constraint_name, cc.column_name, cc.position " +
		"FROM all_constraints c JOIN all_cons_columns cc " +
		"ON cc.owner = c.owner AND cc.constraint_name = c.constraint_name AND cc.table_name = c.table_name " +
		"WHERE c.owner = :1 AND c.table_name = :2 AND c.constraint_type IN ('P', 'U') " +
		"ORDER BY c.constraint_name, cc.position"
	rows, err := db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return fmt.Errorf("query Oracle target unique keys: %w", err)
	}
	defer rows.Close()
	byConstraint := map[string][]string{}
	for rows.Next() {
		var name, column string
		var position int
		if err := rows.Scan(&name, &column, &position); err != nil {
			return fmt.Errorf("scan Oracle target unique keys: %w", err)
		}
		byConstraint[name] = append(byConstraint[name], column)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate Oracle target unique keys: %w", err)
	}
	for _, columns := range byConstraint {
		if len(columns) != len(keys) {
			continue
		}
		matches := true
		for i := range keys {
			if columns[i] != keys[i] {
				matches = false
				break
			}
		}
		if matches {
			return nil
		}
	}
	return fmt.Errorf("oracle partitioned change apply keys %v must match a primary or unique constraint", keys)
}

func ensureOracleTransferApplyLedger(ctx context.Context, db *sql.DB, schema string) error {
	exists, err := oracleBaseTableExists(ctx, db, schema, oracleTransferApplyLedgerTable)
	if err != nil {
		return err
	}
	dialect := commonquery.ForEngine("oracle")
	qualified := dialect.QualifiedTable(schema, oracleTransferApplyLedgerTable)
	if !exists {
		statement := "CREATE TABLE " + qualified + " (" +
			"apply_identity VARCHAR2(36 CHAR) NOT NULL, " +
			"source_identity VARCHAR2(4000 CHAR) NOT NULL, " +
			"target_identity VARCHAR2(4000 CHAR) NOT NULL, " +
			"partition_key VARCHAR2(255 CHAR) NOT NULL, " +
			"position_type VARCHAR2(50 CHAR) NOT NULL, " +
			"position_version VARCHAR2(20 CHAR) NOT NULL, " +
			"next_offset NUMBER(19,0) NOT NULL CHECK (next_offset >= 0), " +
			"created_at TIMESTAMP(6) DEFAULT SYSTIMESTAMP NOT NULL, " +
			"updated_at TIMESTAMP(6) DEFAULT SYSTIMESTAMP NOT NULL, " +
			"PRIMARY KEY (apply_identity, partition_key))"
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
		comment := "COMMENT ON TABLE " + qualified + " IS '" + InternalTransferApplyTableComment + "'"
		if _, err := db.ExecContext(ctx, comment); err != nil {
			return fmt.Errorf("mark Oracle transfer apply ledger ownership: %w", err)
		}
	}
	return validateOracleTransferApplyLedger(ctx, db, schema)
}

func validateOracleTransferApplyLedger(ctx context.Context, db *sql.DB, schema string) error {
	fields, err := (&OraclePlugin{}).listColumnsWithSQL(ctx, db, schema, oracleTransferApplyLedgerTable)
	if err != nil {
		return err
	}
	expected := []struct {
		name      string
		fieldType datatype.FieldType
	}{
		{"APPLY_IDENTITY", datatype.FieldTypeString},
		{"SOURCE_IDENTITY", datatype.FieldTypeString},
		{"TARGET_IDENTITY", datatype.FieldTypeString},
		{"PARTITION_KEY", datatype.FieldTypeString},
		{"POSITION_TYPE", datatype.FieldTypeString},
		{"POSITION_VERSION", datatype.FieldTypeString},
		{"NEXT_OFFSET", datatype.FieldTypeDecimal},
		{"CREATED_AT", datatype.FieldTypeTimestamp},
		{"UPDATED_AT", datatype.FieldTypeTimestamp},
	}
	if len(fields) != len(expected) {
		return fmt.Errorf("oracle transfer apply ledger has %d columns, expected %d", len(fields), len(expected))
	}
	for i := range fields {
		if fields[i].Name != expected[i].name || fields[i].Type != expected[i].fieldType || fields[i].Nullable {
			return fmt.Errorf("oracle transfer apply ledger column %d is %q %q nullable=%t, expected %q %q nullable=false", i+1, fields[i].Name, fields[i].NativeType, fields[i].Nullable, expected[i].name, expected[i].fieldType)
		}
	}
	var comment sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT comments FROM all_tab_comments WHERE owner = :1 AND table_name = :2", schema, oracleTransferApplyLedgerTable).Scan(&comment); err != nil {
		return fmt.Errorf("read Oracle transfer apply ledger ownership: %w", err)
	}
	if !comment.Valid || comment.String != InternalTransferApplyTableComment {
		return fmt.Errorf("oracle transfer apply ledger ownership marker is invalid")
	}
	return validateOracleApplyTargetKeys(ctx, db, schema, oracleTransferApplyLedgerTable, []string{"APPLY_IDENTITY", "PARTITION_KEY"})
}

func insertOracleApplyLedgerStart(ctx context.Context, tx *sql.Tx, schema string, opts plugin.PartitionedTableChangeApplyOptions, targetIdentity, partition string, startOffset int64) error {
	qualified := commonquery.ForEngine("oracle").QualifiedTable(schema, oracleTransferApplyLedgerTable)
	statement := "MERGE INTO " + qualified + " target " +
		"USING (SELECT :1 apply_identity, :2 source_identity, :3 target_identity, :4 partition_key, " +
		":5 position_type, :6 position_version, :7 next_offset FROM DUAL) source " +
		"ON (target.apply_identity = source.apply_identity AND target.partition_key = source.partition_key) " +
		"WHEN NOT MATCHED THEN INSERT " +
		"(apply_identity, source_identity, target_identity, partition_key, position_type, position_version, next_offset) " +
		"VALUES (source.apply_identity, source.source_identity, source.target_identity, source.partition_key, source.position_type, source.position_version, source.next_offset)"
	if _, err := tx.ExecContext(ctx, statement, opts.ApplyIdentity, opts.SourceIdentity, targetIdentity, partition,
		plugin.ChangeStreamPositionTypeKafkaOffset, plugin.ChangeStreamPositionVersionV1, startOffset); err != nil {
		return fmt.Errorf("initialize Oracle target apply ledger: %w", err)
	}
	return nil
}

func lockOracleApplyLedger(ctx context.Context, tx *sql.Tx, schema, applyIdentity, partition string) (*oracleApplyLedgerPosition, error) {
	qualified := commonquery.ForEngine("oracle").QualifiedTable(schema, oracleTransferApplyLedgerTable)
	query := "SELECT source_identity, target_identity, position_type, position_version, next_offset " +
		"FROM " + qualified + " WHERE apply_identity = :1 AND partition_key = :2 FOR UPDATE NOWAIT"
	for {
		var ledger oracleApplyLedgerPosition
		err := tx.QueryRowContext(ctx, query, applyIdentity, partition).Scan(
			&ledger.SourceIdentity, &ledger.TargetIdentity, &ledger.PositionType, &ledger.PositionVersion, &ledger.NextOffset,
		)
		if err == nil {
			return &ledger, nil
		}
		if !strings.Contains(strings.ToUpper(err.Error()), "ORA-00054") {
			return nil, fmt.Errorf("lock Oracle target apply ledger: %w", err)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("lock Oracle target apply ledger: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func validateOracleApplyLedgerIdentity(ledger *oracleApplyLedgerPosition, sourceIdentity, targetIdentity string) error {
	if ledger.SourceIdentity != sourceIdentity || ledger.TargetIdentity != targetIdentity {
		return fmt.Errorf("oracle target apply identity drift: ledger source=%q target=%q, batch source=%q target=%q", ledger.SourceIdentity, ledger.TargetIdentity, sourceIdentity, targetIdentity)
	}
	if ledger.PositionType != plugin.ChangeStreamPositionTypeKafkaOffset || ledger.PositionVersion != plugin.ChangeStreamPositionVersionV1 {
		return fmt.Errorf("unsupported Oracle target apply ledger position %s/%s", ledger.PositionType, ledger.PositionVersion)
	}
	return nil
}

func updateOracleApplyLedger(ctx context.Context, tx *sql.Tx, schema, applyIdentity, partition string, nextOffset int64) error {
	qualified := commonquery.ForEngine("oracle").QualifiedTable(schema, oracleTransferApplyLedgerTable)
	statement := "UPDATE " + qualified + " SET next_offset = :1, updated_at = SYSTIMESTAMP " +
		"WHERE apply_identity = :2 AND partition_key = :3 AND next_offset <= :4"
	result, err := tx.ExecContext(ctx, statement, nextOffset, applyIdentity, partition, nextOffset)
	if err != nil {
		return fmt.Errorf("advance Oracle target apply ledger: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Oracle target apply ledger update result: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("oracle target apply ledger was not advanced")
	}
	return nil
}

func mergeOracleApplyRow(ctx context.Context, tx *sql.Tx, schema, table string, row map[string]interface{}, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo, keys []string) error {
	dialect := commonquery.ForEngine("oracle")
	selects := make([]string, 0, len(fields))
	args := make([]interface{}, 0, len(fields))
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
		value, ok := row[field.Name]
		if !ok {
			return fmt.Errorf("oracle upsert row is missing field %q", field.Name)
		}
		quoted := dialect.QuoteIdentifier(field.Name)
		expression, valueArgs, err := oracleApplyValueExpression(field, value, spatialInfo, len(args)+1)
		if err != nil {
			return err
		}
		selects = append(selects, expression+" "+quoted)
		args = append(args, valueArgs...)
		columns = append(columns, field.Name)
	}
	keySet := make(map[string]bool, len(keys))
	on := make([]string, 0, len(keys))
	for _, key := range keys {
		keySet[key] = true
		quoted := dialect.QuoteIdentifier(key)
		on = append(on, "target."+quoted+" = source."+quoted)
	}
	updates := make([]string, 0, len(columns))
	quotedColumns := make([]string, 0, len(columns))
	insertValues := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted := dialect.QuoteIdentifier(column)
		quotedColumns = append(quotedColumns, quoted)
		insertValues = append(insertValues, "source."+quoted)
		if !keySet[column] {
			updates = append(updates, "target."+quoted+" = source."+quoted)
		}
	}
	statement := "MERGE INTO " + dialect.QualifiedTable(schema, table) +
		" target USING (SELECT " + strings.Join(selects, ", ") + " FROM DUAL) source ON (" + strings.Join(on, " AND ") + ") "
	if len(updates) > 0 {
		statement += "WHEN MATCHED THEN UPDATE SET " + strings.Join(updates, ", ") + " "
	}
	statement += "WHEN NOT MATCHED THEN INSERT (" + strings.Join(quotedColumns, ", ") + ") VALUES (" + strings.Join(insertValues, ", ") + ")"
	if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
		return fmt.Errorf("execute Oracle CDC target MERGE: %w", err)
	}
	return nil
}

func oracleApplyValueExpression(field datatype.FieldInfo, value interface{}, spatialInfo *datatype.SpatialInfo, bindIndex int) (string, []interface{}, error) {
	if value == nil {
		if !field.Nullable {
			return "", nil, fmt.Errorf("oracle upsert field %q is NOT NULL", field.Name)
		}
		return "CAST(NULL AS " + oracleExpectedType(field) + ")", nil, nil
	}
	if datatype.IsSpatialFieldType(field.Type) {
		column := oracleSpatialColumnForField(spatialInfo, field.Name)
		encoded, err := oracleGeometryWKB(value, column)
		if err != nil {
			return "", nil, fmt.Errorf("convert Oracle geometry field %q: %w", field.Name, err)
		}
		srid := "NULL"
		if column.SRID != nil && *column.SRID > 0 {
			srid = strconv.Itoa(*column.SRID)
		}
		gtype := oracleSDOGTypeExpression(column)
		bind := fmt.Sprintf(":%d", bindIndex)
		expression := "(SELECT MDSYS.SDO_GEOMETRY(" + gtype + ", " + srid +
			", decoded.raw_geom.SDO_POINT, decoded.raw_geom.SDO_ELEM_INFO, decoded.raw_geom.SDO_ORDINATES) " +
			"FROM (SELECT SDO_UTIL.FROM_WKBGEOMETRY(" + bind + ") raw_geom FROM DUAL) decoded)"
		return expression, []interface{}{go_ora.Blob{Data: encoded}}, nil
	}
	converted, err := oracleScalarWriteValue(field, value)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf(":%d", bindIndex), []interface{}{converted}, nil
}

func oracleScalarWriteValue(field datatype.FieldInfo, value interface{}) (interface{}, error) {
	switch field.Type {
	case datatype.FieldTypeString:
		text, ok := value.(string)
		if !ok || utf8.RuneCountInString(text) > 4000 {
			return nil, fmt.Errorf("oracle string field %q requires at most 4000 characters", field.Name)
		}
		return text, nil
	case datatype.FieldTypeUUID:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("oracle UUID field %q requires string", field.Name)
		}
		if _, err := uuid.Parse(text); err != nil {
			return nil, fmt.Errorf("oracle UUID field %q is invalid", field.Name)
		}
		return text, nil
	case datatype.FieldTypeDate:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("oracle date field %q requires YYYY-MM-DD text", field.Name)
		}
		parsed, err := time.Parse("2006-01-02", text)
		if err != nil {
			return nil, fmt.Errorf("oracle date field %q is invalid", field.Name)
		}
		return parsed, nil
	case datatype.FieldTypeTimestamp:
		if valueTime, ok := value.(time.Time); ok {
			return valueTime, nil
		}
		return nil, fmt.Errorf("oracle timestamp field %q requires time.Time", field.Name)
	case datatype.FieldTypeDecimal:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("oracle decimal field %q requires canonical decimal text", field.Name)
		}
		number, err := go_ora.NewNumberFromString(text)
		if err != nil {
			return nil, fmt.Errorf("oracle decimal field %q is invalid: %w", field.Name, err)
		}
		return *number, nil
	case datatype.FieldTypeJSON:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("oracle JSON field %q requires JSON text", field.Name)
		}
		return text, nil
	case datatype.FieldTypeBytes:
		bytes, ok := value.([]byte)
		if !ok {
			return nil, fmt.Errorf("oracle bytes field %q requires []byte", field.Name)
		}
		return go_ora.Blob{Data: bytes}, nil
	default:
		return value, nil
	}
}

func oracleSpatialColumnForField(spatialInfo *datatype.SpatialInfo, fieldName string) *datatype.GeometryColumnInfo {
	if spatialInfo == nil {
		return nil
	}
	for i := range spatialInfo.GeometryColumns {
		if strings.EqualFold(spatialInfo.GeometryColumns[i].Name, fieldName) {
			return &spatialInfo.GeometryColumns[i]
		}
	}
	return nil
}

func oracleGeometryWKB(value interface{}, column *datatype.GeometryColumnInfo) ([]byte, error) {
	if column == nil {
		return nil, fmt.Errorf("frozen spatial facts are required")
	}
	encoded, ok := value.([]byte)
	if !ok {
		return nil, fmt.Errorf("geometry requires EWKB []byte, got %T", value)
	}
	geometry, err := ewkb.Unmarshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode EWKB: %w", err)
	}
	if column.SRID != nil && *column.SRID > 0 && geometry.SRID() != *column.SRID {
		return nil, fmt.Errorf("geometry SRID %d does not match frozen SRID %d", geometry.SRID(), *column.SRID)
	}
	expectedType := datatype.ParseGeometryType(column.GeometryType)
	if expectedType != datatype.GeometryTypeUnknown && expectedType != datatype.GeometryTypeGeometry {
		actualType := datatype.ParseGeometryType(oracleApplyGeometryTypeName(geometry))
		if actualType != expectedType {
			return nil, fmt.Errorf("geometry type %s does not match frozen type %s", actualType, expectedType)
		}
	}
	if column.Dimension != nil && *column.Dimension > 0 && geometry.Layout().Stride() != *column.Dimension {
		return nil, fmt.Errorf("geometry dimension %d does not match frozen dimension %d", geometry.Layout().Stride(), *column.Dimension)
	}
	standardWKB, err := wkb.Marshal(geometry, wkb.NDR)
	if err != nil {
		return nil, fmt.Errorf("encode standard WKB: %w", err)
	}
	return standardWKB, nil
}

func oracleSDOGTypeExpression(column *datatype.GeometryColumnInfo) string {
	if column == nil {
		return "decoded.raw_geom.SDO_GTYPE"
	}
	typeCode := 0
	switch datatype.ParseGeometryType(column.GeometryType) {
	case datatype.GeometryTypePoint:
		typeCode = 1
	case datatype.GeometryTypeLineString:
		typeCode = 2
	case datatype.GeometryTypePolygon:
		typeCode = 3
	case datatype.GeometryTypeGeometryCollection:
		typeCode = 4
	case datatype.GeometryTypeMultiPoint:
		typeCode = 5
	case datatype.GeometryTypeMultiLineString:
		typeCode = 6
	case datatype.GeometryTypeMultiPolygon:
		typeCode = 7
	default:
		return "decoded.raw_geom.SDO_GTYPE"
	}
	dimension := 2
	if column.Dimension != nil && *column.Dimension > 0 {
		dimension = *column.Dimension
	}
	return strconv.Itoa(dimension*1000 + typeCode)
}

func oracleApplyGeometryTypeName(geometry geom.T) string {
	switch geometry.(type) {
	case *geom.Point:
		return string(datatype.GeometryTypePoint)
	case *geom.LineString:
		return string(datatype.GeometryTypeLineString)
	case *geom.Polygon:
		return string(datatype.GeometryTypePolygon)
	case *geom.MultiPoint:
		return string(datatype.GeometryTypeMultiPoint)
	case *geom.MultiLineString:
		return string(datatype.GeometryTypeMultiLineString)
	case *geom.MultiPolygon:
		return string(datatype.GeometryTypeMultiPolygon)
	case *geom.GeometryCollection:
		return string(datatype.GeometryTypeGeometryCollection)
	default:
		return ""
	}
}

func deleteOracleApplyRow(ctx context.Context, tx *sql.Tx, schema, table string, row map[string]interface{}, keys []string) error {
	dialect := commonquery.ForEngine("oracle")
	predicates := make([]string, 0, len(keys))
	args := make([]interface{}, 0, len(keys))
	for index, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			return fmt.Errorf("oracle delete row is missing non-null key field %q", key)
		}
		predicates = append(predicates, dialect.QuoteIdentifier(key)+fmt.Sprintf(" = :%d", index+1))
		args = append(args, value)
	}
	statement := "DELETE FROM " + dialect.QualifiedTable(schema, table) + " WHERE " + strings.Join(predicates, " AND ")
	if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
		return fmt.Errorf("execute Oracle CDC target delete: %w", err)
	}
	return nil
}
