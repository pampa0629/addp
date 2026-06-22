package models

import (
	"reflect"
	"strings"
	"testing"
)

func TestTransferTaskCreateKeepsExplicitAutoScanFalse(t *testing.T) {
	field, ok := reflect.TypeOf(TransferTask{}).FieldByName("AutoScanMetadata")
	if !ok {
		t.Fatal("AutoScanMetadata field missing")
	}
	if tag := field.Tag.Get("gorm"); strings.Contains(tag, "default:") {
		t.Fatalf("AutoScanMetadata gorm tag = %q; default tags make explicit false values fall back to the database default", tag)
	}
}

func TestTransferTaskTypeDefaultIsSync(t *testing.T) {
	field, ok := reflect.TypeOf(TransferTask{}).FieldByName("TaskType")
	if !ok {
		t.Fatal("TaskType field missing")
	}
	tag := field.Tag.Get("gorm")
	if !strings.Contains(tag, "default:'sync'") {
		t.Fatalf("TaskType gorm tag = %q, want default:'sync'", tag)
	}
}
