package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetaClientCreateManualScanRunUsesAsyncPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotTenant string
	var gotPayload map[string]interface{}
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Internal-API-Key")
		gotTenant = r.Header.Get("X-Tenant-ID")
		decodeErr = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":11,"tenant_id":8,"execution_id":"run-1","module":"meta","task_type":"scan","status":"pending","trigger_type":"manual"}`))
	}))
	defer server.Close()

	client := NewMetaClientWithInternalKey(server.URL, "internal-key")
	tenantID := uint(8)
	client.SetTenantID(&tenantID)

	result, err := client.CreateManualScanRun(MetaScanOptions{
		EngineID:    26,
		NodeID:      1831,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: "manual",
		Source:      "transfer",
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
	if gotHeader != "internal-key" || gotTenant != "8" {
		t.Fatalf("auth headers = key:%q tenant:%q", gotHeader, gotTenant)
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

func TestMetaClientRefreshItemUsesItemRefreshPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotTenant string
	var gotPayload map[string]interface{}
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Internal-API-Key")
		gotTenant = r.Header.Get("X-Tenant-ID")
		decodeErr = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","message":"ok","items_scanned":1,"fields_scanned":5,"extraction":{"documents":1,"extracted":0,"unsupported":1,"failed":0,"indexed":0,"index_failed":0}}`))
	}))
	defer server.Close()

	client := NewMetaClientWithInternalKey(server.URL, "internal-key")
	tenantID := uint(8)
	client.SetTenantID(&tenantID)

	result, err := client.RefreshItem(1831, MetaScanOptions{EngineID: 26, Force: true})
	if err != nil {
		t.Fatalf("RefreshItem() error = %v", err)
	}
	if decodeErr != nil {
		t.Fatalf("decode payload: %v", decodeErr)
	}
	if gotPath != "/api/v1/meta/items/1831/refresh" {
		t.Fatalf("path = %q, want /api/v1/meta/items/1831/refresh", gotPath)
	}
	if gotHeader != "internal-key" || gotTenant != "8" {
		t.Fatalf("auth headers = key:%q tenant:%q", gotHeader, gotTenant)
	}
	if gotPayload["engine_id"] != float64(26) || gotPayload["force"] != true {
		t.Fatalf("payload = %#v", gotPayload)
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
