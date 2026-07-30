package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
)

func newTestMetaClient(baseURL string) *MetaClient {
	return NewMetaClient(baseURL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "test-token", nil
	}))
}

func TestMetaClientCreateManualScanRunUsesAsyncPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotLegacyHeaders bool
	var gotPayload map[string]interface{}
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("Authorization")
		gotLegacyHeaders = r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != ""
		decodeErr = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":11,"tenant_id":8,"execution_id":"run-1","module":"meta","task_type":"scan","status":"pending","trigger_type":"manual"}`))
	}))
	defer server.Close()

	client := newTestMetaClient(server.URL)
	tenantID := uint(8)
	client = client.WithTenantID(tenantID)

	result, err := client.CreateManualScanRun(MetaScanOptions{
		EngineID:    26,
		NodeID:      1831,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: "manual",
		Source:      commonExecution.ModuleTransfer,
		RefGroups: []MetaScanRefGroup{
			{
				Primary: "bucket/path/roads.shp",
				Refs: []MetaScanRef{
					{Path: "bucket/path/roads.shp", Role: "main", Required: true},
					{Path: "bucket/path/roads.dbf", Role: "sidecar", Required: true},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateManualScanRun() error = %v", err)
	}
	if decodeErr != nil {
		t.Fatalf("decode payload: %v", decodeErr)
	}
	if gotPath != "/api/v1/meta/scan/run/manual" {
		t.Fatalf("path = %q, want /api/v1/meta/scan/run/manual", gotPath)
	}
	if gotHeader != "Bearer test-token" || gotLegacyHeaders {
		t.Fatalf("auth headers = authorization:%q legacy:%t", gotHeader, gotLegacyHeaders)
	}
	if gotPayload["engine_id"] != float64(26) || gotPayload["node_id"] != float64(1831) {
		t.Fatalf("payload target = %#v", gotPayload)
	}
	if gotPayload["scan_depth"] != "deep" || gotPayload["force"] != true {
		t.Fatalf("payload scan options = %#v", gotPayload)
	}
	if gotPayload["trigger_type"] != "manual" || gotPayload["source"] != "transfer" {
		t.Fatalf("payload trigger/source = %#v", gotPayload)
	}
	refGroups, ok := gotPayload["ref_groups"].([]interface{})
	if !ok || len(refGroups) != 1 {
		t.Fatalf("ref_groups = %#v", gotPayload["ref_groups"])
	}
	firstGroup, ok := refGroups[0].(map[string]interface{})
	if !ok || firstGroup["primary"] != "bucket/path/roads.shp" {
		t.Fatalf("ref group = %#v", refGroups[0])
	}
	if result.ExecutionID != "run-1" || result.Status != "pending" {
		t.Fatalf("result = %#v", result)
	}
}

func TestMetaClientCreateManualScanRunRejectsScheduledTrigger(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	client := newTestMetaClient(server.URL)
	_, err := client.CreateManualScanRun(MetaScanOptions{EngineID: 26, TriggerType: commonExecution.TriggerTypeScheduled})
	if err == nil {
		t.Fatal("CreateManualScanRun() should reject scheduled trigger_type")
	}
	if called {
		t.Fatal("CreateManualScanRun() should reject before sending request")
	}
}

func TestMetaClientRefreshItemUsesItemRefreshPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotLegacyHeaders bool
	var gotPayload map[string]interface{}
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("Authorization")
		gotLegacyHeaders = r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != ""
		decodeErr = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","message":"ok","items_scanned":1,"fields_scanned":5,"extraction":{"documents":1,"extracted":0,"unsupported":1,"failed":0,"indexed":0,"index_failed":0}}`))
	}))
	defer server.Close()

	client := newTestMetaClient(server.URL)
	tenantID := uint(8)
	client = client.WithTenantID(tenantID)

	result, err := client.RefreshItem(1831, MetaScanOptions{EngineID: 26, ScanDepth: "deep", TriggerType: "manual", Source: commonExecution.ModuleManager, Force: true})
	if err != nil {
		t.Fatalf("RefreshItem() error = %v", err)
	}
	if decodeErr != nil {
		t.Fatalf("decode payload: %v", decodeErr)
	}
	if gotPath != "/api/v1/meta/items/1831/refresh" {
		t.Fatalf("path = %q, want /api/v1/meta/items/1831/refresh", gotPath)
	}
	if gotHeader != "Bearer test-token" || gotLegacyHeaders {
		t.Fatalf("auth headers = authorization:%q legacy:%t", gotHeader, gotLegacyHeaders)
	}
	if gotPayload["engine_id"] != float64(26) || gotPayload["force"] != true {
		t.Fatalf("payload = %#v", gotPayload)
	}
	if gotPayload["scan_depth"] != "deep" || gotPayload["trigger_type"] != "manual" || gotPayload["source"] != "manager" {
		t.Fatalf("payload scan context = %#v", gotPayload)
	}
	if _, ok := gotPayload["item_id"]; ok {
		t.Fatalf("payload should not include item_id: %#v", gotPayload)
	}
	if result.ItemsScanned != 1 || result.FieldsScanned != 5 {
		t.Fatalf("result = %#v", result)
	}
	if result.Extraction == nil || result.Extraction.Unsupported != 1 {
		t.Fatalf("extraction = %#v", result.Extraction)
	}
}

func TestMetaClientRefreshItemRejectsScheduledTrigger(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	client := newTestMetaClient(server.URL)
	_, err := client.RefreshItem(1831, MetaScanOptions{EngineID: 26, TriggerType: commonExecution.TriggerTypeScheduled})
	if err == nil {
		t.Fatal("RefreshItem() should reject scheduled trigger_type")
	}
	if called {
		t.Fatal("RefreshItem() should reject before sending request")
	}
}

func TestMetaClientDecodesItemDataUpdatedAt(t *testing.T) {
	t.Parallel()

	const dataUpdatedAt = "2026-06-06T08:30:00Z"
	const fingerprint = "item-fingerprint-21"
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":21,"tenant_id":8,"engine_id":7,"node_id":3,"item_type":"object","name":"roads.geojson","full_name":"bucket/roads.geojson","fingerprint":"` + fingerprint + `","data_updated_at":"` + dataUpdatedAt + `"}`))
	}))
	defer server.Close()

	client := newTestMetaClient(server.URL).WithTenantID(8)
	item, err := client.GetItemByID(21)
	if err != nil {
		t.Fatalf("GetItemByID() error = %v", err)
	}
	if gotPath != "/api/v1/meta/items/21" {
		t.Fatalf("path = %q, want /api/v1/meta/items/21", gotPath)
	}
	want, err := time.Parse(time.RFC3339, dataUpdatedAt)
	if err != nil {
		t.Fatalf("parse want time: %v", err)
	}
	if item.DataUpdatedAt == nil || !item.DataUpdatedAt.Equal(want) {
		t.Fatalf("DataUpdatedAt = %#v, want %v", item.DataUpdatedAt, want)
	}
	if item.Fingerprint != fingerprint {
		t.Fatalf("Fingerprint = %q, want %q", item.Fingerprint, fingerprint)
	}
}
