package service

import "testing"

func TestLogicalTextPhysicalMapping(t *testing.T) {
	svc := &LogicalTableService{}
	length := 32
	if got := svc.mapDataTypeToPostgreSQL("string", nil); got != "TEXT" {
		t.Fatalf("unbounded text=%s", got)
	}
	if got := svc.mapDataTypeToPostgreSQL("string", &length); got != "VARCHAR(32)" {
		t.Fatalf("bounded text=%s", got)
	}
	if got := svc.mapDataTypeToPostgreSQL("text", nil); got != "" {
		t.Fatalf("retired logical type=%s", got)
	}
}

func TestLogicalDecimalPhysicalMapping(t *testing.T) {
	got := (&LogicalTableService{}).mapDataTypeToPostgreSQL("decimal", nil)
	if got != "NUMERIC(38,18)" {
		t.Fatalf("decimal physical type=%s", got)
	}
	if normalizePostgreSQLType(got) != normalizePostgreSQLType("numeric(38,18)") {
		t.Fatalf("decimal physical type cannot be validated: %s", got)
	}
}
