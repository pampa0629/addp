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
		ConnectorName: "secret-connector", TopicName: "__addp_cdc.1.2.1",
		PostgreSQL: &PostgreSQLCaptureResource{SlotName: "secret_slot"},
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

func TestCaptureSummaryExposesTypedSourceRecoveryFacts(t *testing.T) {
	summary := NewCaptureSummary(&CaptureResource{SourceRecovery: JSONMap{
		"schema_version": "capture.source_recovery/v1", "provider": "oracle", "health": "critical",
		"capture_position": "100", "earliest_available_position": "110", "position_headroom": "-10",
		"source_password": "must-not-leak",
	}})
	if summary.SourceRecovery == nil || summary.SourceRecovery.Health != "critical" || summary.SourceRecovery.PositionHeadroom != "-10" {
		t.Fatalf("source recovery summary = %#v", summary.SourceRecovery)
	}
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "must-not-leak") || strings.Contains(string(data), "source_password") {
		t.Fatalf("capture summary leaked private recovery fields: %s", data)
	}
}

func TestCaptureSummaryExposesTypedSourceTransactionFacts(t *testing.T) {
	duration := int64(3600)
	summary := NewCaptureSummary(&CaptureResource{SourceTransactions: JSONMap{
		"schema_version": "capture.source_transactions/v1", "provider": "oracle", "status": "available",
		"active_count": 2, "oldest_start_position": "1234", "oldest_duration_seconds": duration,
		"used_undo_blocks": "10", "used_undo_records": "20", "source_password": "must-not-leak",
	}})
	if summary.SourceTransactions == nil || summary.SourceTransactions.ActiveCount != 2 || summary.SourceTransactions.OldestDurationSeconds == nil || *summary.SourceTransactions.OldestDurationSeconds != duration {
		t.Fatalf("source transaction summary = %#v", summary.SourceTransactions)
	}
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "must-not-leak") || strings.Contains(string(data), "source_password") {
		t.Fatalf("capture summary leaked private transaction fields: %s", data)
	}
	emptyData, err := json.Marshal(NewCaptureSummary(&CaptureResource{SourceTransactions: JSONMap{
		"schema_version": "capture.source_transactions/v1", "provider": "oracle", "status": "available", "active_count": 0,
	}}))
	if err != nil || !strings.Contains(string(emptyData), `"active_count":0`) {
		t.Fatalf("empty transaction count is not explicit: %s, %v", emptyData, err)
	}
}

func TestCaptureResourceKeepsProviderFieldsOutOfGenericGeneration(t *testing.T) {
	typeOfCapture := reflect.TypeOf(CaptureResource{})
	for _, field := range []string{"SlotName", "PublicationName", "SlotOwned", "PublicationOwned", "ConnectorServerID"} {
		if _, ok := typeOfCapture.FieldByName(field); ok {
			t.Fatalf("generic CaptureResource contains provider field %s", field)
		}
	}
	if _, ok := reflect.TypeOf(PostgreSQLCaptureResource{}).FieldByName("SlotName"); !ok {
		t.Fatal("PostgreSQL provider facts do not own SlotName")
	}
	if _, ok := reflect.TypeOf(MySQLCaptureResource{}).FieldByName("ConnectorServerID"); !ok {
		t.Fatal("MySQL provider facts do not own ConnectorServerID")
	}
	if _, ok := reflect.TypeOf(OracleCaptureResource{}).FieldByName("SchemaHistoryTopicName"); !ok {
		t.Fatal("Oracle provider facts do not own SchemaHistoryTopicName")
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
