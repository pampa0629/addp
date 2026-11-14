package service

import (
	"context"
	"errors"
	"fmt"
	commonClient "github.com/addp/common/client"
	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
	"time"
)

type riskPageReader struct {
	pages    []*commonClient.TransferCDCRisks
	err      error
	calls    []uint
	errAfter int
}

func (r *riskPageReader) ReadCDCRisks(_ context.Context, after uint) (*commonClient.TransferCDCRisks, error) {
	r.calls = append(r.calls, after)
	if r.err != nil && (r.errAfter == 0 || len(r.calls) >= r.errAfter) {
		return nil, r.err
	}
	page := r.pages[0]
	r.pages = r.pages[1:]
	return page, nil
}

func cdcRiskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:cdc-risk-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("ATTACH DATABASE ':memory:' AS monitor").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`CREATE TABLE monitor.alert_incidents (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, module TEXT NOT NULL,
		task_type TEXT NOT NULL, source_task_id TEXT NOT NULL, execution_id TEXT NOT NULL,
		alert_rule_id INTEGER, rule_id TEXT NOT NULL DEFAULT '', rule_name TEXT NOT NULL DEFAULT '',
		signal_code TEXT NOT NULL, fingerprint TEXT NOT NULL, severity TEXT NOT NULL, status TEXT NOT NULL,
		details JSON NOT NULL, opened_at DATETIME NOT NULL, last_observed_at DATETIME NOT NULL,
		acknowledged_at DATETIME, acknowledged_by TEXT, suppressed_until DATETIME, resolved_at DATETIME,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE monitor.alert_rules (id INTEGER PRIMARY KEY, enabled BOOLEAN, rule_type TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCDCRiskEvaluationPreservesUnknownWithoutHidingOtherFacts(t *testing.T) {
	db := cdcRiskTestDB(t)
	var err error
	now := time.Now().UTC()
	at := now.Add(-time.Second)
	expiry := now.Add(time.Minute)
	existing := monitorModels.AlertIncident{TenantID: 2, Module: "transfer", TaskType: "sync", SourceTaskID: "7", ExecutionID: "old", SignalCode: cdcRiskCodes[0], Fingerprint: "keep-source", Severity: "critical", Status: "acknowledged", Details: map[string]interface{}{}, OpenedAt: at, LastObservedAt: at}
	if err = db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	unknown := commonClient.CDCRiskObservation{State: "unknown"}
	active := commonClient.CDCRiskObservation{State: "active", Reason: "buffer_position_lost", SampledAt: &at, ExpiresAt: &expiry}
	row := commonClient.TransferCDCRiskItem{TenantID: 2, TaskID: 7, Generation: 3, ExecutionID: "bb706a62-4256-453e-b471-0dbdbb305c53", State: "observing", Source: unknown, Buffer: active}
	svc := NewAlertService(db, nil)
	reader := &riskPageReader{pages: []*commonClient.TransferCDCRisks{{Items: []commonClient.TransferCDCRiskItem{row}, NextAfterID: 7}}}
	svc.SetCDCRiskReader(reader)
	signals, keep, err := svc.evaluateCDCRisks(context.Background(), now)
	if err != nil || len(signals) != 1 || signals[0].Code != cdcRiskCodes[1] || len(keep) != 1 || keep[0] != "keep-source" {
		t.Fatalf("signals=%#v keep=%v err=%v", signals, keep, err)
	}
	if signals[0].Severity != "critical" || signals[0].Details["generation"] != "3" {
		t.Fatal("risk lost generation")
	}
	oldFP := signals[0].Fingerprint
	row.Generation = 4
	row.Source = commonClient.CDCRiskObservation{State: "clear", SampledAt: &at, ExpiresAt: &expiry}
	reader.pages = []*commonClient.TransferCDCRisks{{Items: []commonClient.TransferCDCRiskItem{row}}}
	signals, keep, err = svc.evaluateCDCRisks(context.Background(), now)
	if err != nil || len(keep) != 0 || len(signals) != 1 || signals[0].Fingerprint == oldFP {
		t.Fatal("generation and known-clear scope collapsed")
	}
	reader.err = errors.New("owner unavailable")
	signals, keep, err = svc.evaluateCDCRisks(context.Background(), now)
	if err != nil || len(signals) != 0 || len(keep) != 1 {
		t.Fatal("Owner failure implied recovery")
	}
	reader.err = nil
	expired := now.Add(-time.Millisecond)
	row.Buffer.ExpiresAt = &expired
	reader.pages = []*commonClient.TransferCDCRisks{{Items: []commonClient.TransferCDCRiskItem{row}}}
	signals, _, err = svc.evaluateCDCRisks(context.Background(), now)
	if err != nil || len(signals) != 0 {
		t.Fatal("expired risk emitted a new signal")
	}
	reader.pages = []*commonClient.TransferCDCRisks{{Items: []commonClient.TransferCDCRiskItem{row}, NextAfterID: 7, HasMore: true}, {}}
	_, _, err = svc.evaluateCDCRisks(context.Background(), now)
	if err != nil || reader.calls[len(reader.calls)-1] != 7 {
		t.Fatal("did not drain Owner inventory")
	}
	row.Buffer = active
	reader.calls = nil
	reader.err = errors.New("second Owner page failed")
	reader.errAfter = 2
	reader.pages = []*commonClient.TransferCDCRisks{{Items: []commonClient.TransferCDCRiskItem{row}, NextAfterID: 7, HasMore: true}}
	signals, keep, err = svc.evaluateCDCRisks(context.Background(), now)
	if err != nil || len(signals) != 0 || len(keep) != 1 || keep[0] != "keep-source" {
		t.Fatal("partial inventory published or cleared risks")
	}
}

func TestCDCCaptureFailureRequiresSixtySecondsOfAdvancingEvidence(t *testing.T) {
	db := cdcRiskTestDB(t)
	var err error
	svc := NewAlertService(db, nil)
	now := time.Now().UTC()
	since := now.Add(-60 * time.Second)
	expiry := now.Add(time.Minute)
	row := commonClient.TransferCDCRiskItem{TenantID: 2, TaskID: 7, Generation: 1, State: "observing", ExecutionID: "bb706a62-4256-453e-b471-0dbdbb305c53", Source: commonClient.CDCRiskObservation{State: "unknown"}, Buffer: commonClient.CDCRiskObservation{State: "unknown"}, Capture: commonClient.CDCCaptureObservation{State: "failed", Reason: "task_not_running", FailedSince: &since, SampledAt: &now, ExpiresAt: &expiry}}
	reader := &riskPageReader{}
	svc.SetCDCRiskReader(reader)
	evaluate := func(at time.Time) ([]activeAlertSignal, []string) {
		reader.pages = []*commonClient.TransferCDCRisks{{Items: []commonClient.TransferCDCRiskItem{row}}}
		signals, keep, e := svc.evaluateCDCRisks(context.Background(), at)
		if e != nil {
			t.Fatal(e)
		}
		return signals, keep
	}
	sampled := since.Add(59 * time.Second)
	row.Capture.SampledAt = &sampled
	if signals, _ := evaluate(now.Add(20 * time.Second)); len(signals) != 0 {
		t.Fatal("waiting without new evidence completed duration")
	}
	row.Capture.SampledAt = &now
	signals, _ := evaluate(now)
	if len(signals) != 1 || signals[0].Code != "cdc_capture_failed" || signals[0].Severity != "warning" {
		t.Fatal(signals)
	}
	incident := monitorModels.AlertIncident{TenantID: 2, Module: "transfer", TaskType: "sync", SourceTaskID: "7", ExecutionID: row.ExecutionID, SignalCode: signals[0].Code, Fingerprint: signals[0].Fingerprint, Severity: "warning", Status: "acknowledged", OpenedAt: now, LastObservedAt: now, Details: map[string]interface{}{}}
	if err = db.Create(&incident).Error; err != nil {
		t.Fatal(err)
	}
	row.Capture = commonClient.CDCCaptureObservation{State: "unknown"}
	if signals, keep := evaluate(now); len(signals) != 0 || len(keep) != 1 || keep[0] != incident.Fingerprint {
		t.Fatal("unknown did not preserve acknowledged incident")
	}
	row.Capture = commonClient.CDCCaptureObservation{State: "failed", Reason: "source_disconnected", FailedSince: &now, SampledAt: &now, ExpiresAt: &expiry}
	if signals, _ := evaluate(now); len(signals) != 1 {
		t.Fatal("new failure episode closed an existing incident")
	}
	row.Capture = commonClient.CDCCaptureObservation{State: "healthy", SampledAt: &now, ExpiresAt: &expiry}
	if signals, keep := evaluate(now); len(signals) != 0 || len(keep) != 0 {
		t.Fatal("fresh healthy did not release incident")
	}
	row.State = "closed"
	if signals, keep := evaluate(now); len(signals) != 0 || len(keep) != 0 {
		t.Fatal("closed Owner did not release incident")
	}
}

func TestCaptureWarningReadProjectionKeepsOnlySafeTimingAndReason(t *testing.T) {
	alert := monitorModels.AlertIncident{SignalCode: "cdc_capture_failed", Details: map[string]interface{}{"generation": "3", "reason": "source_disconnected", "failed_since": "2026-11-14T00:00:00Z", "trigger_seconds": 60, "private_connector": "secret", "raw_error": "secret"}}
	safe := safeAlertForRead(alert)
	if len(safe.Details) != 4 || safe.Details["reason"] != "source_disconnected" || safe.Details["trigger_seconds"] != 60 || safe.Details["failed_since"] == nil {
		t.Fatal(safe.Details)
	}
}
