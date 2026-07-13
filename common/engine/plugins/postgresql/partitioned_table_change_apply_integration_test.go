package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestIntegrationPostgresPartitionedTableChangeApplyIsMonotonic(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	ctx := context.Background()
	schema := "common_pg_it"
	table := fmt.Sprintf("partitioned_apply_%d", time.Now().UnixNano())
	path := postgresPrepareTablePath(schema, table)
	applyIdentity := uuid.NewString()
	opts := plugin.PartitionedTableChangeApplyOptions{
		ApplyIdentity:  applyIdentity,
		SourceIdentity: "addp://engine/30/path/orders.events?type=topic",
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "name", Type: datatype.FieldTypeString, Nullable: false},
		},
		Keys: []string{"id"},
	}
	defer dropPostgresPrepareTable(db, schema, table)
	defer db.ExecContext(ctx, `DELETE FROM addp_transfer.apply_positions WHERE apply_identity = $1::uuid`, applyIdentity)

	if err := pg.PreparePartitionedTableChangeApply(ctx, connInfo, path, opts); err != nil {
		t.Fatalf("PreparePartitionedTableChangeApply failed: %v", err)
	}
	first := partitionedApplyBatch("0", 0, 3,
		positionedChange(1, 1, "first"),
		positionedChange(2, 1, "latest-in-batch"),
		positionedChange(3, 2, "second-key"),
	)
	result, err := pg.ApplyPartitionedTableChanges(ctx, connInfo, path, first, opts)
	if err != nil {
		t.Fatalf("first ApplyPartitionedTableChanges failed: %v", err)
	}
	if result.AppliedRecords != 2 || result.SkippedRecords != 1 {
		t.Fatalf("first result=%#v, want applied=2 skipped=1", result)
	}
	assertPartitionedApplyRow(t, ctx, db, schema, table, 1, "latest-in-batch")

	stale := partitionedApplyBatch("0", 0, 2,
		positionedChange(1, 1, "stale-one"),
		positionedChange(2, 2, "stale-two"),
	)
	result, err = pg.ApplyPartitionedTableChanges(ctx, connInfo, path, stale, opts)
	if err != nil {
		t.Fatalf("stale ApplyPartitionedTableChanges failed: %v", err)
	}
	if result.AppliedRecords != 0 || result.SkippedRecords != 2 {
		t.Fatalf("stale result=%#v, want applied=0 skipped=2", result)
	}
	assertPartitionedApplyRow(t, ctx, db, schema, table, 1, "latest-in-batch")

	next := partitionedApplyBatch("0", 3, 4, positionedChange(4, 1, "new-session"))
	if _, err := pg.ApplyPartitionedTableChanges(ctx, connInfo, path, next, opts); err != nil {
		t.Fatalf("next ApplyPartitionedTableChanges failed: %v", err)
	}
	assertPartitionedApplyRow(t, ctx, db, schema, table, 1, "new-session")

	if _, err := pg.ApplyPartitionedTableChanges(ctx, connInfo, path, first, opts); err != nil {
		t.Fatalf("late old-worker ApplyPartitionedTableChanges failed: %v", err)
	}
	assertPartitionedApplyRow(t, ctx, db, schema, table, 1, "new-session")

	gap := partitionedApplyBatch("0", 5, 6, positionedChange(6, 3, "gap"))
	if _, err := pg.ApplyPartitionedTableChanges(ctx, connInfo, path, gap, opts); err == nil || !strings.Contains(err.Error(), "ledger gap") {
		t.Fatalf("gap error=%v, want ledger gap", err)
	}

	drift := opts
	drift.SourceIdentity = "addp://engine/30/path/other.events?type=topic"
	if _, err := pg.ApplyPartitionedTableChanges(ctx, connInfo, path, next, drift); err == nil || !strings.Contains(err.Error(), "identity drift") {
		t.Fatalf("identity drift error=%v, want identity drift", err)
	}
}

func partitionedApplyBatch(partition string, start, end int64, changes ...plugin.PartitionedTableChange) *plugin.PartitionedTableChangeBatch {
	return &plugin.PartitionedTableChangeBatch{
		Partition:     partition,
		StartPosition: kafkaOffsetPosition(partition, start),
		EndPosition:   kafkaOffsetPosition(partition, end),
		Changes:       changes,
	}
}

func positionedChange(nextOffset, id int64, name string) plugin.PartitionedTableChange {
	return plugin.PartitionedTableChange{
		Operation: plugin.TableChangeOperationUpsert,
		Position:  kafkaOffsetPosition("0", nextOffset),
		Row:       map[string]interface{}{"id": id, "name": name},
	}
}

func assertPartitionedApplyRow(t *testing.T, ctx context.Context, db queryRower, schema, table string, id int64, wantName string) {
	t.Helper()
	var name string
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT name FROM "%s"."%s" WHERE id = $1`, schema, table), id).Scan(&name); err != nil {
		t.Fatalf("query target id=%d: %v", id, err)
	}
	if name != wantName {
		t.Fatalf("target id=%d name=%q, want %q", id, name, wantName)
	}
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}
