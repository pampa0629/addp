package postgresql

import (
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestFilterAndCoalescePostgresChangesSkipsReplayAndKeepsLatestKey(t *testing.T) {
	batch := &plugin.PartitionedTableChangeBatch{
		Partition: "0",
		Changes: []plugin.PartitionedTableChange{
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 6), Row: map[string]interface{}{"id": int64(1), "name": "replayed"}},
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 7), Row: map[string]interface{}{"id": int64(2), "name": "first"}},
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 8), Row: map[string]interface{}{"id": int64(2), "name": "last"}},
		},
	}

	rows, skipped, err := filterAndCoalescePostgresChanges(batch, []string{"id"}, 5, 6, 8)
	if err != nil {
		t.Fatalf("filterAndCoalescePostgresChanges() error = %v", err)
	}
	if skipped != 2 || len(rows) != 1 {
		t.Fatalf("skipped=%d rows=%d, want skipped=2 rows=1", skipped, len(rows))
	}
	if rows[0].row["name"] != "last" || rows[0].nextOffset != 8 {
		t.Fatalf("row=%#v offset=%d, want latest id=2 state", rows[0].row, rows[0].nextOffset)
	}
}

func TestFilterAndCoalescePostgresChangesKeepsLatestDeletePerKey(t *testing.T) {
	batch := &plugin.PartitionedTableChangeBatch{
		Partition: "0",
		Changes: []plugin.PartitionedTableChange{
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 6), Row: map[string]interface{}{"id": int64(1), "name": "created"}},
			{Operation: plugin.TableChangeOperationDelete, Position: kafkaOffsetPosition("0", 7), Row: map[string]interface{}{"id": int64(1)}},
			{Operation: plugin.TableChangeOperationDelete, Position: kafkaOffsetPosition("0", 8), Row: map[string]interface{}{"id": int64(2)}},
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 9), Row: map[string]interface{}{"id": int64(2), "name": "recreated"}},
		},
	}

	changes, skipped, err := filterAndCoalescePostgresChanges(batch, []string{"id"}, 5, 5, 9)
	if err != nil {
		t.Fatalf("filterAndCoalescePostgresChanges() error = %v", err)
	}
	if skipped != 2 || len(changes) != 2 {
		t.Fatalf("skipped=%d changes=%d, want skipped=2 changes=2", skipped, len(changes))
	}
	if changes[0].operation != plugin.TableChangeOperationDelete || changes[0].row["id"] != int64(1) {
		t.Fatalf("first change = %#v, want id=1 delete", changes[0])
	}
	if changes[1].operation != plugin.TableChangeOperationUpsert || changes[1].row["id"] != int64(2) {
		t.Fatalf("second change = %#v, want id=2 upsert", changes[1])
	}
}

func TestFilterAndCoalescePostgresChangesAcceptsLedgerOnlySkip(t *testing.T) {
	batch := &plugin.PartitionedTableChangeBatch{
		Partition: "0",
		Changes: []plugin.PartitionedTableChange{
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 6), Row: map[string]interface{}{"id": int64(1), "name": "one"}},
			{Operation: plugin.TableChangeOperationSkip, Position: kafkaOffsetPosition("0", 7)},
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 8), Row: map[string]interface{}{"id": int64(2), "name": "two"}},
		},
	}

	changes, skipped, err := filterAndCoalescePostgresChanges(batch, []string{"id"}, 5, 5, 8)
	if err != nil {
		t.Fatalf("filterAndCoalescePostgresChanges() error = %v", err)
	}
	if skipped != 1 || len(changes) != 2 {
		t.Fatalf("skipped=%d changes=%d, want skipped=1 changes=2", skipped, len(changes))
	}
	if changes[0].nextOffset != 6 || changes[1].nextOffset != 8 {
		t.Fatalf("changes=%#v, want data operations around skipped offset", changes)
	}
}

func TestFilterAndCoalescePostgresChangesRejectsSkipRow(t *testing.T) {
	batch := &plugin.PartitionedTableChangeBatch{
		Partition: "0",
		Changes: []plugin.PartitionedTableChange{{
			Operation: plugin.TableChangeOperationSkip,
			Position:  kafkaOffsetPosition("0", 6),
			Row:       map[string]interface{}{"id": int64(1)},
		}},
	}

	_, _, err := filterAndCoalescePostgresChanges(batch, []string{"id"}, 5, 5, 6)
	if err == nil || !strings.Contains(err.Error(), "skip operation must not contain a row") {
		t.Fatalf("error=%v, want skip row rejection", err)
	}
}

func TestFilterAndCoalescePostgresChangesRejectsNonIncreasingPositions(t *testing.T) {
	batch := &plugin.PartitionedTableChangeBatch{
		Partition: "0",
		Changes: []plugin.PartitionedTableChange{
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 8), Row: map[string]interface{}{"id": 1}},
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 7), Row: map[string]interface{}{"id": 2}},
		},
	}

	_, _, err := filterAndCoalescePostgresChanges(batch, []string{"id"}, 0, 0, 8)
	if err == nil || !strings.Contains(err.Error(), "strictly increasing") {
		t.Fatalf("error=%v, want strictly increasing error", err)
	}
}

func TestFilterAndCoalescePostgresChangesRejectsUnrepresentedEndPosition(t *testing.T) {
	batch := &plugin.PartitionedTableChangeBatch{
		Partition: "0",
		Changes: []plugin.PartitionedTableChange{
			{Operation: plugin.TableChangeOperationUpsert, Position: kafkaOffsetPosition("0", 7), Row: map[string]interface{}{"id": 1}},
		},
	}
	_, _, err := filterAndCoalescePostgresChanges(batch, []string{"id"}, 6, 6, 8)
	if err == nil || !strings.Contains(err.Error(), "does not match batch end") {
		t.Fatalf("error=%v, want batch end mismatch", err)
	}
}

func TestPostgresChangeKeyRejectsMissingOrNullKey(t *testing.T) {
	for _, row := range []map[string]interface{}{{}, {"id": nil}} {
		if _, err := postgresChangeKey(row, []string{"id"}); err == nil {
			t.Fatalf("postgresChangeKey(%#v) succeeded, want error", row)
		}
	}
}
