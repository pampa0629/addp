package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetaClientScanEngineUsesV1PathAndPreciseItemPayload(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"status":"success","message":"ok","items_scanned":1,"fields_scanned":3}`))
	}))
	defer server.Close()

	client := NewMetaClientWithInternalKey(server.URL, "internal-key")
	tenantID := uint(8)
	client.SetTenantID(&tenantID)

	result, err := client.ScanEngine(MetaScanOptions{
		EngineID:    26,
		ItemID:      1831,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: "manual",
	})
	if err != nil {
		t.Fatalf("ScanEngine() error = %v", err)
	}
	if decodeErr != nil {
		t.Fatalf("decode payload: %v", decodeErr)
	}
	if gotPath != "/api/v1/meta/scan/engine" {
		t.Fatalf("path = %q, want /api/v1/meta/scan/engine", gotPath)
	}
	if gotHeader != "internal-key" || gotTenant != "8" {
		t.Fatalf("auth headers = key:%q tenant:%q", gotHeader, gotTenant)
	}
	if gotPayload["engine_id"] != float64(26) || gotPayload["item_id"] != float64(1831) {
		t.Fatalf("payload target = %#v", gotPayload)
	}
	if _, ok := gotPayload["targets"]; ok {
		t.Fatalf("payload should not include targets: %#v", gotPayload)
	}
	if _, ok := gotPayload["catalog_paths"]; ok {
		t.Fatalf("payload should not include catalog_paths: %#v", gotPayload)
	}
	if gotPayload["scan_depth"] != "deep" || gotPayload["force"] != true {
		t.Fatalf("payload scan options = %#v", gotPayload)
	}
	if result.ItemsScanned != 1 || result.FieldsScanned != 3 {
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
		_, _ = w.Write([]byte(`{"status":"success","message":"ok","items_scanned":1,"fields_scanned":5}`))
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
}
