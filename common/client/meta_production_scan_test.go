package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"github.com/google/uuid"
)

func TestMetaProductionBasicScanStrictClient(t *testing.T) {
	id := uuid.NewString()
	result := MetaProductionIdentification{Reference: production.Reference{Owner: "transfer", ExecutionID: id, ResultID: id}, TenantID: 7, EngineVersion: 1, ScanExecutionID: uuid.NewString(), Status: "pending"}
	encoded, _ := json.Marshal(result)
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"valid", string(encoded), 200, true}, {"wrong origin", strings.Replace(string(encoded), id, uuid.NewString(), 1), 200, false},
		{"unknown field", strings.TrimSuffix(string(encoded), "}") + `,"private":"secret"}`, 200, false},
		{"invalid status", strings.Replace(string(encoded), "pending", "issued", 1), 200, false}, {"null", "null", 200, false},
		{"trailing", string(encoded) + `{}`, 200, false}, {"too large", strings.Repeat(" ", 256<<10) + string(encoded), 200, false},
		{"not ready", `{"error":"secret"}`, 503, false}, {"refused", `{"error":"secret"}`, 409, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/meta/runtime/productions/transfer/"+id+"/"+id+"/basic-scan" || r.URL.RawQuery != "" || r.ContentLength != 0 || r.Header.Get("Authorization") != "Bearer addp_at_transfer" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("unsafe production scan transport")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			got, err := NewMetaClient(server.URL, staticTenantToken("addp_at_transfer")).EnsureProductionBasicScan(t.Context(), 7, production.Reference{Owner: "transfer", ExecutionID: id, ResultID: id})
			if (err == nil) != tc.ok || calls != 1 || err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("result=%+v err=%v calls=%d", got, err, calls)
			}
		})
	}
}

func TestMetaProductionDeepScanStrictClient(t *testing.T) {
	ref := production.Reference{Owner: "transfer", ExecutionID: uuid.NewString(), ResultID: uuid.NewString()}
	result := MetaProductionDeepScan{Reference: ref, TenantID: 7, EngineVersion: 3, BasicScanExecutionID: uuid.NewString(), ScanExecutionID: uuid.NewString(), Status: "pending"}
	encoded, _ := json.Marshal(result)
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"valid", string(encoded), true},
		{"wrong tenant", strings.Replace(string(encoded), `"tenant_id":"7"`, `"tenant_id":"8"`, 1), false},
		{"wrong result", strings.Replace(string(encoded), ref.ResultID, uuid.NewString(), 1), false},
		{"same scans", strings.Replace(string(encoded), result.ScanExecutionID, result.BasicScanExecutionID, 1), false},
		{"producer as dependency", strings.Replace(string(encoded), result.BasicScanExecutionID, ref.ExecutionID, 1), false},
		{"missing dependency", strings.Replace(string(encoded), result.BasicScanExecutionID, "", 1), false},
		{"fake grant", strings.TrimSuffix(string(encoded), "}") + `,"grant":"private-secret"}`, false},
		{"bad status", strings.Replace(string(encoded), "pending", "issued", 1), false},
		{"trailing", string(encoded) + `{}`, false},
		{"oversize", strings.Repeat(" ", 256<<10) + string(encoded), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/meta/runtime/productions/transfer/"+ref.ExecutionID+"/"+ref.ResultID+"/deep-scan" || r.URL.RawQuery != "" || r.ContentLength != 0 || r.Header.Get("Authorization") != "Bearer addp_at_transfer" {
					t.Error("unsafe deep scan transport")
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			got, err := NewMetaClient(server.URL, staticTenantToken("addp_at_transfer")).EnsureProductionDeepScan(t.Context(), 7, ref)
			if (err == nil) != tc.ok || calls != 1 || err != nil && strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("result=%+v err=%v calls=%d", got, err, calls)
			}
		})
	}
}

func TestMetaProductionReadIsBodylessAndDoesNotInventNewness(t *testing.T) {
	ref := production.Reference{Owner: "develop", ExecutionID: uuid.NewString(), ResultID: uuid.NewString()}
	binding := factstest.Binding(plugin.TabularItemPath(9, "schema", "public", "result"))
	for _, state := range []string{"new", "existing", ""} {
		t.Run("state "+state, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/meta/runtime/productions/develop/"+ref.ExecutionID+"/"+ref.ResultID+"/basic-scan" || r.URL.RawQuery != "" || r.ContentLength != 0 {
					t.Error("identification read became a mutation or caller-supplied scope")
				}
				_ = json.NewEncoder(w).Encode(MetaProductionIdentification{Reference: ref, TenantID: 7, ScanExecutionID: uuid.NewString(), Status: "success", EngineVersion: 3, Binding: &binding, LogicalItemState: state})
			}))
			defer server.Close()
			result, err := NewMetaClient(server.URL, staticTenantToken("system")).ReadProductionIdentification(t.Context(), 7, ref)
			if (err == nil) != (state != "") {
				t.Fatal("omitted newness accepted as a fact", result, err)
			}
		})
	}
}
