package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
)

func newTestMetaClient(baseURL string) *MetaClient {
	return NewMetaClient(baseURL, ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "test-token", nil
	}))
}

func TestMetaCatalogLookupsPreserveTypedErrorsWithoutResponseBodies(t *testing.T) {
	for _, status := range []int{404, 403, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"private-upstream-body","error_code":"lookup_failure"}`))
		}))
		c := newTestMetaClient(server.URL).WithTenantID(7)
		for _, lookup := range []func() error{
			func() error { _, err := c.GetItemByID(context.Background(), 1); return err },
			func() error { _, err := c.GetNodeByID(context.Background(), 1); return err },
			func() error { _, err := c.GetItemByCatalogPath(context.Background(), 2, "public.people"); return err },
			func() error { _, err := c.GetNodeByCatalogPath(context.Background(), 2, "public"); return err },
		} {
			err := lookup()
			if got, ok := TenantAPIStatusCode(err); !ok || got != status || strings.Contains(err.Error(), "private-upstream-body") {
				t.Fatalf("lookup lost safe typed HTTP status: %v", err)
			}
		}
		server.Close()
	}
}

func TestMetaClientGetDataItemSecurityFactsUsesExactRuntimeContract(t *testing.T) {
	t.Parallel()
	fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString}}
	hash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	var gotPath, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuthorization = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(dataprotection.DataItemSecurityFacts{
			SchemaVersion: dataprotection.DataItemSecurityFactsSchemaV1, ItemFingerprint: "fingerprint-1",
			ItemType: "collection", Fields: fields, SourceSnapshotHash: hash, ObservedAt: time.Now().UTC(),
		})
	}))
	defer server.Close()
	facts, err := newTestMetaClient(server.URL).WithTenantID(8).GetDataItemSecurityFacts(context.Background(), "fingerprint-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/meta/runtime/data-items/fingerprint-1/security-facts" || gotAuthorization != "Bearer test-token" {
		t.Fatalf("request = %s auth=%q", gotPath, gotAuthorization)
	}
	if facts.ItemFingerprint != "fingerprint-1" || facts.SourceSnapshotHash != hash {
		t.Fatalf("facts = %#v", facts)
	}
}

func TestMetaClientGetDataItemSecuritySampleUsesExactRuntimeContract(t *testing.T) {
	t.Parallel()
	hash, err := dataprotection.DocumentTextSnapshotHash("contact 13661384499", false)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/runtime/data-items/fingerprint-1/security-sample" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("request = %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(dataprotection.DataItemSecuritySample{
			SchemaVersion: dataprotection.DataItemSecuritySampleSchemaV1, ItemFingerprint: "fingerprint-1",
			ItemType: "document", Text: "contact 13661384499", SourceSnapshotHash: hash, ObservedAt: time.Now().UTC(),
		})
	}))
	defer server.Close()
	sample, err := newTestMetaClient(server.URL).WithTenantID(8).GetDataItemSecuritySample(context.Background(), "fingerprint-1")
	if err != nil {
		t.Fatal(err)
	}
	if sample.SourceSnapshotHash != hash || sample.Text == "" {
		t.Fatalf("sample = %#v", sample)
	}
}

func TestMetaClientCollectExecutionLineageUsesDevelopServiceContract(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotPath string
	var gotHeader string
	var gotLegacyHeaders bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("Authorization")
		gotLegacyHeaders = r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != ""
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"observed":1,"skipped":2}`))
	}))
	defer server.Close()

	result, err := newTestMetaClient(server.URL).WithTenantID(8).CollectExecutionLineage(context.Background(), "execution-1")
	if err != nil {
		t.Fatalf("CollectExecutionLineage() error = %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/meta/lineage/executions/execution-1/collect" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotHeader != "Bearer test-token" || gotLegacyHeaders {
		t.Fatalf("auth headers = authorization:%q legacy:%t", gotHeader, gotLegacyHeaders)
	}
	if result == nil || result.Observed != 1 || result.Skipped != 2 {
		t.Fatalf("result = %#v", result)
	}
}

func TestMetaClientListDataItemChangesUsesCatalogFeedContract(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotCursor string
	var gotLimit string
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCursor = r.URL.Query().Get("after_cursor")
		gotLimit = r.URL.Query().Get("limit")
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema_version":"meta.data_item_changes/v1","changes":[{"change_id":"NDI","operation":"upsert","source_identity":"fingerprint-1","source_version":"00000000000000000042","observed_at":"2026-08-26T10:00:00Z","snapshot":{"item_id":21,"name":"orders"}}],"next_cursor":"NDI","has_more":true}`))
	}))
	defer server.Close()

	result, err := newTestMetaClient(server.URL).WithTenantID(8).ListDataItemChanges(context.Background(), "NDE", 200)
	if err != nil {
		t.Fatalf("ListDataItemChanges() error = %v", err)
	}
	if gotPath != "/api/v1/meta/data-items/changes" || gotCursor != "NDE" || gotLimit != "200" {
		t.Fatalf("request = %s?after_cursor=%s&limit=%s", gotPath, gotCursor, gotLimit)
	}
	if gotAuthorization != "Bearer test-token" {
		t.Fatalf("Authorization = %q", gotAuthorization)
	}
	if result.SchemaVersion != "meta.data_item_changes/v1" || len(result.Changes) != 1 || result.Changes[0].SourceIdentity != "fingerprint-1" || !result.HasMore {
		t.Fatalf("result = %#v", result)
	}
	if result.Changes[0].Snapshot["name"] != "orders" {
		t.Fatalf("snapshot = %#v", result.Changes[0].Snapshot)
	}
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

	result, err := client.CreateManualScanRun(context.Background(), MetaScanOptions{
		ParentExecutionID: "11111111-1111-4111-8111-111111111111",
		EngineID:          26,
		NodeID:            1831,
		ScanDepth:         "deep",
		Force:             true,
		TriggerType:       "manual",
		Source:            commonExecution.ModuleTransfer,
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
	if gotPayload["parent_execution_id"] != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("parent provenance missing: %#v", gotPayload)
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
	_, err := client.CreateManualScanRun(context.Background(), MetaScanOptions{EngineID: 26, TriggerType: commonExecution.TriggerTypeScheduled})
	if err == nil {
		t.Fatal("CreateManualScanRun() should reject scheduled trigger_type")
	}
	if called {
		t.Fatal("CreateManualScanRun() should reject before sending request")
	}
}

func TestMetaUserRefreshItemUsesItemRefreshPath(t *testing.T) {
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

	client := NewMetaScanUserClient(server.URL)

	headerless := false
	result, err := client.RefreshItem(t.Context(), "current-user", 1831, MetaScanOptions{EngineID: 26, ScanDepth: "deep", TriggerType: "manual", Source: commonExecution.ModuleManager, Force: true, Encoding: "gbk", HasHeader: &headerless})
	if err != nil {
		t.Fatalf("RefreshItem() error = %v", err)
	}
	if decodeErr != nil {
		t.Fatalf("decode payload: %v", decodeErr)
	}
	if gotPath != "/api/v1/meta/items/1831/refresh" {
		t.Fatalf("path = %q, want /api/v1/meta/items/1831/refresh", gotPath)
	}
	if gotHeader != "Bearer current-user" || gotLegacyHeaders {
		t.Fatalf("auth headers = authorization:%q legacy:%t", gotHeader, gotLegacyHeaders)
	}
	if gotPayload["encoding"] != "gbk" || gotPayload["has_header"] != false {
		t.Fatalf("encoding missing: %#v", gotPayload)
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

func TestMetaUserRefreshItemRejectsScheduledTrigger(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	client := NewMetaScanUserClient(server.URL)
	_, err := client.RefreshItem(t.Context(), "current-user", 1831, MetaScanOptions{EngineID: 26, TriggerType: commonExecution.TriggerTypeScheduled})
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
	item, err := client.GetItemByID(context.Background(), 21)
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

func TestMetaUserRefreshItemPreservesErrorContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"localized message","error_code":"scan_delimited_options_invalid"}`))
	}))
	defer server.Close()
	_, err := NewMetaScanUserClient(server.URL).RefreshItem(t.Context(), "current-user", 1, MetaScanOptions{Encoding: "bad"})
	if status, ok := TenantAPIStatusCode(err); !ok || status != 400 {
		t.Fatalf("status: %v", err)
	}
	if code, ok := TenantAPIErrorCode(err); !ok || code != "scan_delimited_options_invalid" {
		t.Fatalf("code: %v", err)
	}
}
func TestMetaClientManualItemRunCarriesEncoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["encoding"] != "gbk" || payload["item_id"] != float64(1) || payload["has_header"] != false {
			t.Errorf("payload = %#v", payload)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"execution_id":"run-1"}`))
	}))
	defer server.Close()
	headerless := false
	if _, err := newTestMetaClient(server.URL).WithTenantID(8).CreateManualScanRun(context.Background(), MetaScanOptions{ItemID: 1, Encoding: "gbk", HasHeader: &headerless}); err != nil {
		t.Fatal(err)
	}
}

func TestMetaSchemaChangeScanSourceIsAReferenceNotAnActor(t *testing.T) {
	opts := MetaScanOptions{EngineID: 9, Targets: []string{"addp://engine/9/path/public/changed?type=table"}, Source: "transfer", ScanDepth: "deep", Force: true, SchemaChangeSource: &commonExecution.SchemaChangeScanSource{ExecutionID: "blocked-execution", RequestID: 61, ClaimToken: "current-claim"}}
	body, err := manualMetaScanBody(opts)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]interface{}
	if err = json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	ref, ok := data["schema_change_source"].(map[string]interface{})
	if !ok || len(ref) != 3 || ref["execution_id"] != "blocked-execution" || ref["request_id"] != float64(61) || ref["claim_token"] != "current-claim" {
		t.Fatalf("source=%#v", data)
	}
	opts.ParentExecutionID = "unrelated-parent"
	if _, err = manualMetaScanBody(opts); err == nil {
		t.Fatal("mutually exclusive origins were accepted")
	}
}
