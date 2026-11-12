package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"github.com/google/uuid"
)

func TestDevelopProductionReceiptStrictTransport(t *testing.T) {
	target := plugin.FileItemPath(11, "result.gpkg")
	receipt := production.Receipt{Reference: production.Reference{Owner: "develop", ExecutionID: uuid.NewString(), ResultID: uuid.NewString()}, TenantID: 7, TargetEngineVersion: 2, Attempt: 1, OperationID: uuid.NewString(), CommittedAt: time.Now().UTC(), Primary: target, Objects: []production.CommittedObject{{Target: target, Created: true}}, Sources: production.SourceEvidence{Kind: production.SourcesUnknown}}
	receipt.OperationID = receipt.Reference.ResultID
	raw, _ := json.Marshal(receipt)
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"valid", string(raw), 200, true},
		{"other tenant", strings.Replace(string(raw), `"tenant_id":"7"`, `"tenant_id":"8"`, 1), 200, false},
		{"other result", strings.Replace(string(raw), receipt.Reference.ResultID, uuid.NewString(), 1), 200, false},
		{"unknown field", strings.Replace(string(raw), `"attempt":1`, `"attempt":1,"config_json":{}`, 1), 200, false},
		{"trailing data", string(raw) + `{}`, 200, false},
		{"too large", strings.Repeat(" ", 256<<10) + string(raw), 200, false},
		{"unresolved", `{"error":"private diagnostic"}`, 404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/develop/runtime/productions/"+receipt.Reference.ExecutionID+"/"+receipt.Reference.ResultID+"/receipt" || r.URL.RawQuery != "" || r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("Authorization") != "Bearer tenant-token" || r.ContentLength != 0 {
					t.Error("unexpected receipt transport")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := newDevelopCatalogTestClient(server).ReadCommittedProduction(t.Context(), 7, receipt.Reference)
			if (err == nil) != tc.ok || (err != nil && strings.Contains(err.Error(), "private diagnostic")) {
				t.Fatal(err)
			}
		})
	}
	redirected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/leak" {
			redirected = true
		}
		http.Redirect(w, r, "/leak", http.StatusFound)
	}))
	defer server.Close()
	if _, err := newDevelopCatalogTestClient(server).ReadCommittedProduction(t.Context(), 7, receipt.Reference); err == nil || redirected {
		t.Fatal("receipt followed redirect")
	}
}
