package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestMetaProductionBasicScanStrictClient(t *testing.T) {
	id := uuid.NewString()
	result := MetaProductionBasicScan{ProductionExecutionID: id, ScanExecutionID: uuid.NewString(), Status: "pending"}
	encoded, _ := json.Marshal(result)
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"valid", string(encoded), 200, true}, {"wrong origin", strings.Replace(string(encoded), id, uuid.NewString(), 1), 200, false},
		{"unknown field", strings.TrimSuffix(string(encoded), "}") + `,"private":"secret"}`, 200, false},
		{"invalid status", strings.Replace(string(encoded), "pending", "issued", 1), 200, false}, {"null", "null", 200, false},
		{"trailing", string(encoded) + `{}`, 200, false}, {"too large", strings.Repeat(" ", 16<<10) + string(encoded), 200, false},
		{"not ready", `{"error":"secret"}`, 503, false}, {"refused", `{"error":"secret"}`, 409, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/meta/runtime/transfer-productions/"+id+"/basic-scan" || r.URL.RawQuery != "" || r.ContentLength != 0 || r.Header.Get("Authorization") != "Bearer addp_at_transfer" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("unsafe production scan transport")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			got, err := NewMetaClient(server.URL, staticTenantToken("addp_at_transfer")).EnsureTransferProductionBasicScan(t.Context(), 7, id)
			if (err == nil) != tc.ok || calls != 1 || err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("result=%+v err=%v calls=%d", got, err, calls)
			}
		})
	}
}
