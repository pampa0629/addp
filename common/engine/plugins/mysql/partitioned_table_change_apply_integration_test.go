package mysql

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pluginshared "github.com/addp/common/engine/plugins/shared"
	"github.com/google/uuid"
)

func TestIntegrationMySQLPartitionedTableChangeApplyIsMonotonic(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := context.Background()
	path := mysqlIntegrationTablePath(database, "orders")
	opts := mysqlPartitionedApplyOptions()
	if err := mysqlPlugin.PreparePartitionedTableChangeApply(ctx, connInfo, path, opts); err != nil {
		t.Fatalf("PreparePartitionedTableChangeApply failed: %v", err)
	}

	first := mysqlPartitionedApplyBatch("0", 0, 3,
		mysqlPositionedChange(1, 1, "first"),
		mysqlPositionedChange(2, 1, "latest-in-batch"),
		mysqlPositionedChange(3, 2, "second-key"),
	)
	result, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, first, opts)
	if err != nil {
		t.Fatalf("first ApplyPartitionedTableChanges failed: %v", err)
	}
	if result.AppliedRecords != 2 || result.SkippedRecords != 1 {
		t.Fatalf("first result=%#v, want applied=2 skipped=1", result)
	}
	assertMySQLPartitionedApplyRow(t, ctx, db, database, "orders", 1, "latest-in-batch")

	stale := mysqlPartitionedApplyBatch("0", 0, 2,
		mysqlPositionedChange(1, 1, "stale-one"),
		mysqlPositionedChange(2, 2, "stale-two"),
	)
	result, err = mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, stale, opts)
	if err != nil {
		t.Fatalf("stale ApplyPartitionedTableChanges failed: %v", err)
	}
	if result.AppliedRecords != 0 || result.SkippedRecords != 2 {
		t.Fatalf("stale result=%#v, want applied=0 skipped=2", result)
	}
	assertMySQLPartitionedApplyRow(t, ctx, db, database, "orders", 1, "latest-in-batch")

	next := mysqlPartitionedApplyBatch("0", 3, 4, mysqlPositionedChange(4, 1, "new-session"))
	if _, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, next, opts); err != nil {
		t.Fatalf("next ApplyPartitionedTableChanges failed: %v", err)
	}
	if _, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, first, opts); err != nil {
		t.Fatalf("late old-worker ApplyPartitionedTableChanges failed: %v", err)
	}
	assertMySQLPartitionedApplyRow(t, ctx, db, database, "orders", 1, "new-session")

	gap := mysqlPartitionedApplyBatch("0", 5, 6, mysqlPositionedChange(6, 3, "gap"))
	if _, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, gap, opts); err == nil || !strings.Contains(err.Error(), "ledger gap") {
		t.Fatalf("gap error=%v, want ledger gap", err)
	}
	drift := opts
	drift.SourceIdentity = "addp://engine/30/path/other.events?type=topic"
	if _, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, next, drift); err == nil || !strings.Contains(err.Error(), "identity drift") {
		t.Fatalf("identity drift error=%v, want identity drift", err)
	}
}

func TestIntegrationMySQLPartitionedTableChangeApplyCommitsDeleteAndSkip(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := context.Background()
	path := mysqlIntegrationTablePath(database, "orders")
	opts := mysqlPartitionedApplyOptions()
	if err := mysqlPlugin.PreparePartitionedTableChangeApply(ctx, connInfo, path, opts); err != nil {
		t.Fatalf("PreparePartitionedTableChangeApply failed: %v", err)
	}
	if _, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, mysqlPartitionedApplyBatch("0", 0, 1, mysqlPositionedChange(1, 1, "created")), opts); err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}
	skip := plugin.PartitionedTableChange{Operation: plugin.TableChangeOperationSkip, Position: pluginshared.KafkaOffsetPosition("0", 2)}
	result, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, mysqlPartitionedApplyBatch("0", 1, 2, skip), opts)
	if err != nil {
		t.Fatalf("skip apply failed: %v", err)
	}
	if result.AppliedRecords != 0 || result.SkippedRecords != 1 {
		t.Fatalf("skip result=%#v, want applied=0 skipped=1", result)
	}
	assertMySQLPartitionedApplyRow(t, ctx, db, database, "orders", 1, "created")

	deleted := plugin.PartitionedTableChange{
		Operation: plugin.TableChangeOperationDelete,
		Position:  pluginshared.KafkaOffsetPosition("0", 3),
		Row:       map[string]interface{}{"id": int64(1)},
	}
	if _, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, mysqlPartitionedApplyBatch("0", 2, 3, deleted), opts); err != nil {
		t.Fatalf("delete apply failed: %v", err)
	}
	var rowCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+mysqlDialect().QualifiedTable(database, "orders")+" WHERE id = ?", 1).Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if rowCount != 0 {
		t.Fatalf("deleted row count=%d, want 0", rowCount)
	}
	var nextOffset int64
	if err := db.QueryRowContext(ctx, "SELECT next_offset FROM "+mysqlDialect().QualifiedTable(database, mysqlTransferApplyLedgerTable)+" WHERE apply_identity = ? AND partition_key = ?", opts.ApplyIdentity, "0").Scan(&nextOffset); err != nil {
		t.Fatal(err)
	}
	if nextOffset != 3 {
		t.Fatalf("ledger next_offset=%d, want 3", nextOffset)
	}
}

func TestIntegrationMySQLPartitionedTableChangeApplyRollsBackLedgerWithBusinessWrite(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := context.Background()
	path := mysqlIntegrationTablePath(database, "orders")
	opts := mysqlPartitionedApplyOptions()
	if err := mysqlPlugin.PreparePartitionedTableChangeApply(ctx, connInfo, path, opts); err != nil {
		t.Fatalf("PreparePartitionedTableChangeApply failed: %v", err)
	}
	invalid := plugin.PartitionedTableChange{
		Operation: plugin.TableChangeOperationUpsert,
		Position:  pluginshared.KafkaOffsetPosition("0", 1),
		Row:       map[string]interface{}{"id": int64(1), "name": nil},
	}
	if _, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, mysqlPartitionedApplyBatch("0", 0, 1, invalid), opts); err == nil {
		t.Fatal("ApplyPartitionedTableChanges accepted a NULL value for a NOT NULL business column")
	}
	var ledgerCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+mysqlDialect().QualifiedTable(database, mysqlTransferApplyLedgerTable)+" WHERE apply_identity = ?", opts.ApplyIdentity).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 0 {
		t.Fatalf("ledger rows after rolled-back business write=%d, want 0", ledgerCount)
	}
}

func TestIntegrationMySQLPartitionedTableChangeApplyRejectsMalformedLedger(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+mysqlDialect().QualifiedTable(database, mysqlTransferApplyLedgerTable)+" (id BIGINT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
		t.Fatalf("create malformed ledger: %v", err)
	}
	err := mysqlPlugin.PreparePartitionedTableChangeApply(ctx, connInfo, mysqlIntegrationTablePath(database, "orders"), mysqlPartitionedApplyOptions())
	if err == nil || !strings.Contains(err.Error(), "transfer apply ledger") {
		t.Fatalf("malformed ledger error=%v, want explicit ledger rejection", err)
	}
	exists, err := mysqlBaseTableExists(ctx, db, database, "orders")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("malformed ledger rejection created the business target table")
	}
}

func TestIntegrationMySQLPartitionedTableChangeApplyCancelsWhileLedgerLocked(t *testing.T) {
	db, mysqlPlugin, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	ctx := context.Background()
	path := mysqlIntegrationTablePath(database, "orders")
	opts := mysqlPartitionedApplyOptions()
	if err := mysqlPlugin.PreparePartitionedTableChangeApply(ctx, connInfo, path, opts); err != nil {
		t.Fatalf("PreparePartitionedTableChangeApply failed: %v", err)
	}
	if _, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, mysqlPartitionedApplyBatch("0", 0, 1, mysqlPositionedChange(1, 1, "created")), opts); err != nil {
		t.Fatalf("initial apply failed: %v", err)
	}

	lockTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	qualifiedLedger := mysqlDialect().QualifiedTable(database, mysqlTransferApplyLedgerTable)
	var lockedOffset int64
	if err := lockTx.QueryRowContext(ctx, "SELECT next_offset FROM "+qualifiedLedger+" WHERE apply_identity = ? AND partition_key = ? FOR UPDATE", opts.ApplyIdentity, "0").Scan(&lockedOffset); err != nil {
		_ = lockTx.Rollback()
		t.Fatalf("lock apply ledger: %v", err)
	}

	applyCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	started := time.Now()
	_, applyErr := mysqlPlugin.ApplyPartitionedTableChanges(applyCtx, connInfo, path, mysqlPartitionedApplyBatch("0", 1, 2, mysqlPositionedChange(2, 2, "blocked")), opts)
	cancel()
	if applyErr == nil {
		_ = lockTx.Rollback()
		t.Fatal("ApplyPartitionedTableChanges succeeded while ledger row remained locked")
	}
	if time.Since(started) > 2*time.Second {
		_ = lockTx.Rollback()
		t.Fatalf("locked ledger cancellation took too long: %s", time.Since(started))
	}
	if err := lockTx.Rollback(); err != nil {
		t.Fatalf("release ledger lock: %v", err)
	}

	var nextOffset int64
	if err := db.QueryRowContext(ctx, "SELECT next_offset FROM "+qualifiedLedger+" WHERE apply_identity = ? AND partition_key = ?", opts.ApplyIdentity, "0").Scan(&nextOffset); err != nil {
		t.Fatal(err)
	}
	if nextOffset != 1 {
		t.Fatalf("ledger next_offset after cancellation=%d, want 1", nextOffset)
	}
	var blockedRows int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+mysqlDialect().QualifiedTable(database, "orders")+" WHERE id = ?", 2).Scan(&blockedRows); err != nil {
		t.Fatal(err)
	}
	if blockedRows != 0 {
		t.Fatalf("business rows after cancellation=%d, want 0", blockedRows)
	}
	if _, err := mysqlPlugin.ApplyPartitionedTableChanges(ctx, connInfo, path, mysqlPartitionedApplyBatch("0", 1, 2, mysqlPositionedChange(2, 2, "recovered")), opts); err != nil {
		t.Fatalf("apply after releasing ledger lock: %v", err)
	}
	assertMySQLPartitionedApplyRow(t, ctx, db, database, "orders", 2, "recovered")
}

func mysqlPartitionedApplyOptions() plugin.PartitionedTableChangeApplyOptions {
	return plugin.PartitionedTableChangeApplyOptions{
		ApplyIdentity:  uuid.NewString(),
		SourceIdentity: "addp://engine/30/path/orders.events?type=topic",
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "name", Type: datatype.FieldTypeString, Nullable: false},
		},
		Keys: []string{"id"},
	}
}

func mysqlPartitionedApplyBatch(partition string, start, end int64, changes ...plugin.PartitionedTableChange) *plugin.PartitionedTableChangeBatch {
	return &plugin.PartitionedTableChangeBatch{
		Partition:     partition,
		StartPosition: pluginshared.KafkaOffsetPosition(partition, start),
		EndPosition:   pluginshared.KafkaOffsetPosition(partition, end),
		Changes:       changes,
	}
}

func mysqlPositionedChange(nextOffset, id int64, name string) plugin.PartitionedTableChange {
	return plugin.PartitionedTableChange{
		Operation: plugin.TableChangeOperationUpsert,
		Position:  pluginshared.KafkaOffsetPosition("0", nextOffset),
		Row:       map[string]interface{}{"id": id, "name": name},
	}
}

func assertMySQLPartitionedApplyRow(t *testing.T, ctx context.Context, db *sql.DB, database, table string, id int64, wantName string) {
	t.Helper()
	var name string
	if err := db.QueryRowContext(ctx, "SELECT name FROM "+mysqlDialect().QualifiedTable(database, table)+" WHERE id = ?", id).Scan(&name); err != nil {
		t.Fatalf("query target id=%d: %v", id, err)
	}
	if name != wantName {
		t.Fatalf("target id=%d name=%q, want %q", id, name, wantName)
	}
}
