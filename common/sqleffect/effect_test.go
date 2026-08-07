package sqleffect

import "testing"

func TestRequireReadOnly(t *testing.T) {
	t.Parallel()

	for _, query := range []string{
		"SELECT * FROM orders",
		"WITH recent AS (SELECT * FROM orders) SELECT * FROM recent",
		"EXPLAIN SELECT * FROM orders",
	} {
		if err := RequireReadOnly(query); err != nil {
			t.Fatalf("RequireReadOnly(%q) error = %v", query, err)
		}
	}
	for _, query := range []string{
		"DELETE FROM orders",
		"SELECT * INTO archived_orders FROM orders",
		"SELECT * FROM orders; DELETE FROM orders",
	} {
		if err := RequireReadOnly(query); err == nil {
			t.Fatalf("RequireReadOnly(%q) error = nil, want rejection", query)
		}
	}
}

func TestAnalyzeReportsTargetsAndWarnings(t *testing.T) {
	read, err := Analyze("SELECT * FROM activities WHERE id = 1")
	if err != nil {
		t.Fatalf("Analyze(read) error = %v", err)
	}
	if read.Effect != Read || read.Statement != "SELECT" || len(read.TargetObjects) != 1 || read.TargetObjects[0] != "ACTIVITIES" {
		t.Fatalf("unexpected read analysis: %#v", read)
	}
	if read.RequiresConfirmation {
		t.Fatal("read query should not require confirmation")
	}

	write, err := Analyze("DELETE FROM activities")
	if err != nil {
		t.Fatalf("Analyze(write) error = %v", err)
	}
	if write.Effect != Write || !write.RequiresConfirmation || len(write.Warnings) == 0 {
		t.Fatalf("unexpected write analysis: %#v", write)
	}

	ddl, err := Analyze("DROP TABLE activities")
	if err != nil {
		t.Fatalf("Analyze(ddl) error = %v", err)
	}
	if ddl.Effect != DDL || !ddl.RequiresConfirmation || len(ddl.TargetObjects) != 1 {
		t.Fatalf("unexpected ddl analysis: %#v", ddl)
	}
	if ddl.Fingerprint == "" || len(ddl.Fingerprint) != 64 {
		t.Fatalf("invalid fingerprint: %q", ddl.Fingerprint)
	}
}
