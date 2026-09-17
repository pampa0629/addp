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
