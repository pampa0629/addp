package client

import (
	"encoding/json"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func uploadReceiptFixture() UploadReceipt {
	return UploadReceipt{SchemaVersion: UploadReceiptSchema, UploadID: uuid.NewString(), TenantID: 7, EngineID: 9, EngineVersion: "2", Uploader: UploadUserOrigin{31, 41, 1}, CommittedAt: time.Now().UTC(), Writes: []plugin.ContentWriteEvidence{{Target: plugin.FileItemPath(9, "dir/a.ifc"), Disposition: "created", SizeBytes: 12, Committed: true}}}
}
func TestUploadReceiptClientUsesBoundedTenantRuntimeWithoutRetry(t *testing.T) {
	for _, scenario := range []string{"ok", "wrong tenant", "unknown field", "bad write", "redirect", "temporary"} {
		t.Run(scenario, func(t *testing.T) {
			receipt := uploadReceiptFixture()
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/v1/manager/runtime/uploads/"+receipt.UploadID+"/receipt" || r.Header.Get("Authorization") == "" {
					t.Error("request scope changed")
				}
				if scenario == "redirect" {
					http.Redirect(w, r, "/other", 307)
					return
				}
				if scenario == "temporary" {
					w.WriteHeader(503)
					return
				}
				value := receipt
				if scenario == "wrong tenant" {
					value.TenantID++
				}
				if scenario == "bad write" {
					value.Writes[0].Committed = false
				}
				raw, _ := json.Marshal(value)
				if scenario == "unknown field" {
					raw = []byte(strings.TrimSuffix(string(raw), "}") + `,"allow":true}`)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(raw)
			}))
			defer server.Close()
			client := NewManagerUploadClient(server.URL, staticSystemServiceTokenSource("runtime"), server.Client())
			got, err := client.ReadUploadReceipt(t.Context(), 7, receipt.UploadID)
			if calls != 1 || ((scenario == "ok") != (err == nil)) || (err != nil && got != nil) {
				t.Fatalf("calls=%d result=%+v err=%v", calls, got, err)
			}
		})
	}
}
func TestUploadReceiptRejectsUncommittedDuplicateAndAmbiguousProvenance(t *testing.T) {
	for _, change := range []func(*UploadReceipt){func(r *UploadReceipt) { r.Uploader.PrincipalID = 0 }, func(r *UploadReceipt) { r.Writes = append(r.Writes, r.Writes[0]) }, func(r *UploadReceipt) { r.Writes[0].Disposition = "appended" }, func(r *UploadReceipt) { r.Writes[0].Target.EngineID++ }, func(r *UploadReceipt) { r.Writes[0].Committed = false }} {
		r := uploadReceiptFixture()
		change(&r)
		if r.Validate() == nil {
			t.Fatal("accepted invalid upload evidence")
		}
	}
}
