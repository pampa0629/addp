package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type cdcRiskToken struct{}

func (cdcRiskToken) Token(context.Context, uint) (string, error) {
	panic("tenant credential used for platform risk inventory")
}
func (cdcRiskToken) PlatformToken(context.Context) (string, error) { return "addp_st_monitor", nil }

func TestCDCRiskClientUsesOnlyPlatformCredentialAndClosedProjection(t *testing.T) {
	now := time.Now().UTC()
	expiry := now.Add(time.Minute)
	risk := CDCRiskObservation{State: "active", Reason: "wal_lost", SampledAt: &now, ExpiresAt: &expiry}
	row := TransferCDCRiskItem{TenantID: 2, TaskID: 7, Generation: 3, ExecutionID: "bb706a62-4256-453e-b471-0dbdbb305c53", State: "observing", Capture: CDCCaptureObservation{State: "unknown"}, Source: risk, Buffer: CDCRiskObservation{State: "unknown"}}
	reply := TransferCDCRisks{SchemaVersion: TransferCDCRiskSchema, ObservedAt: now, Items: []TransferCDCRiskItem{row}, NextAfterID: 7}
	code := 200
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/v1/transfer/runtime/cdc-risk-observations" || r.URL.Query().Get("after_id") != "0" || r.Header.Get("Authorization") != "Bearer addp_st_monitor" {
			t.Error("wrong risk request")
		}
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(reply)
	}))
	defer server.Close()
	client := NewTransferClient(server.URL, cdcRiskToken{})
	if result, err := client.ReadCDCRisks(context.Background(), 0); err != nil || len(result.Items) != 1 {
		t.Fatal("valid risk projection rejected", err)
	}
	original := reply
	for _, mutate := range []func(){func() { reply.Items = []TransferCDCRiskItem{row, row} }, func() { reply.NextAfterID = 8 }, func() { reply.HasMore = true }, func() { reply.Items = []TransferCDCRiskItem{{TaskID: 7, State: "observing"}} }, func() { reply.SchemaVersion = "foreign" }, func() { code = 503 }} {
		reply = original
		code = 200
		mutate()
		if result, err := client.ReadCDCRisks(context.Background(), 0); err == nil || result != nil {
			t.Fatal("invalid risk response accepted")
		}
	}
	if calls != 7 {
		t.Fatal("unexpected request retry")
	}
}

func TestCDCCaptureClientRejectsInvalidFailureEvidenceAndOldVersion(t *testing.T) {
	now := time.Now().UTC()
	since := now.Add(-time.Minute)
	expiry := now.Add(time.Minute)
	row := TransferCDCRiskItem{TenantID: 2, TaskID: 7, Generation: 1, ExecutionID: "bb706a62-4256-453e-b471-0dbdbb305c53", State: "observing", Source: CDCRiskObservation{State: "unknown"}, Buffer: CDCRiskObservation{State: "unknown"}, Capture: CDCCaptureObservation{State: "failed", Reason: "source_disconnected", FailedSince: &since, SampledAt: &now, ExpiresAt: &expiry}}
	reply := TransferCDCRisks{SchemaVersion: TransferCDCRiskSchema, ObservedAt: now, NextAfterID: 7, Items: []TransferCDCRiskItem{row}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(reply) }))
	defer server.Close()
	client := NewTransferClient(server.URL, cdcRiskToken{})
	if _, err := client.ReadCDCRisks(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []CDCCaptureObservation{{State: "failed", Reason: "foreign", FailedSince: &since, SampledAt: &now, ExpiresAt: &expiry}, {State: "failed", Reason: "task_not_running", FailedSince: &expiry, SampledAt: &now, ExpiresAt: &expiry}, {State: "healthy", FailedSince: &since, SampledAt: &now, ExpiresAt: &expiry}, {State: "failed", Reason: "task_not_running", SampledAt: &now, ExpiresAt: &expiry}, {State: "other"}} {
		reply.Items = []TransferCDCRiskItem{row}
		reply.Items[0].Capture = bad
		if _, err := client.ReadCDCRisks(context.Background(), 0); err == nil {
			t.Fatal("invalid capture evidence accepted")
		}
	}
	reply.Items = []TransferCDCRiskItem{row}
	reply.SchemaVersion = "transfer.cdc_risk_observations/v1"
	if _, err := client.ReadCDCRisks(context.Background(), 0); err == nil {
		t.Fatal("old CDC observation contract accepted")
	}
}
