package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	plugin "github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestTransferProductionReceiptBoundedServiceClient(t *testing.T) {
	receipt := TransferProductionReceipt{SchemaVersion: TransferProductionReceiptSchema, TenantID: 7, ExecutionID: uuid.NewString(), Attempt: 1, Target: plugin.TabularItemPath(2, "schema", "outdoor", "persons"), Preparation: plugin.TableTargetCreated, CommittedAt: time.Now().UTC()}
	encoded, _ := json.Marshal(receipt)
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"valid machine origin", string(encoded), 200, true},
		{"valid user origin", strings.Replace(string(encoded), `"user_origin":null`, `"user_origin":{"principal_id":"31","tenant_membership_id":"33","authorization_version":"5"}`, 1), 200, true},
		{"incomplete user origin", strings.Replace(string(encoded), `"user_origin":null`, `"user_origin":{"principal_id":"31"}`, 1), 200, false},
		{"invalid UTF8 target", strings.Replace(string(encoded), "persons", string([]byte{255}), 1), 200, false},
		{"cross tenant", strings.Replace(string(encoded), `"tenant_id":"7"`, `"tenant_id":"8"`, 1), 200, false},
		{"other execution", strings.Replace(string(encoded), receipt.ExecutionID, uuid.NewString(), 1), 200, false},
		{"unknown field", strings.Replace(string(encoded), `"attempt":1`, `"attempt":1,"token":"secret"`, 1), 200, false},
		{"trailing", string(encoded) + `{}`, 200, false},
		{"oversized", strings.Repeat(" ", 16<<10) + string(encoded), 200, false},
		{"not ready", `{"error":"secret"}`, 409, false},
		{"not found", `{"error":"secret"}`, 404, false},
		{"dependency failed", `{"error":"secret"}`, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/transfer/runtime/executions/"+receipt.ExecutionID+"/production-receipt" || r.URL.RawQuery != "" || r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("Authorization") != "Bearer addp_at_system" {
					t.Error("unexpected receipt transport")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := NewTransferClient(server.URL, staticTenantToken("addp_at_system")).ReadProductionReceipt(t.Context(), 7, receipt.ExecutionID)
			if (err == nil) != tc.ok || err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("receipt=%+v error=%v", result, err)
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
	if _, err := NewTransferClient(server.URL, staticTenantToken("addp_at_system")).ReadProductionReceipt(t.Context(), 7, receipt.ExecutionID); err == nil || redirected {
		t.Fatal("receipt request followed redirect")
	}
}
