package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCaptureSummaryDoesNotExposeInternalResourceNames(t *testing.T) {
	summary := NewCaptureSummary(&CaptureResource{
		Generation: 1, Status: CaptureStatusRunning, ConnectorStatus: "RUNNING",
		ConnectorName: "secret-connector", TopicName: "__addp_cdc.1.2.1", SlotName: "secret_slot",
	})
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"secret-connector", "__addp_cdc", "secret_slot"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("capture summary leaked %q: %s", forbidden, text)
		}
	}
}

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
