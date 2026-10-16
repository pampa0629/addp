package client

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestManagerImportSourceClientBindsCurrentExecutionAndRejectsUntrustedResponses(t *testing.T) {
	request := ManagerImportSourceRequest{ExecutionID: uuid.NewString(), TaskID: 9, PrincipalID: 31, SourceLocator: "addp-infra://minio/manager/tenant_7/import/date/upload/roads.shp?type=object"}
	for _, kind := range []string{"valid", "tenant", "uploader", "task", "locator", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			redirected := false
			other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
			defer other.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/api/v1/manager/runtime/import-sources/verify" || r.Header.Get("Authorization") != "Bearer transfer-service" {
					t.Error("wrong private source request")
				}
				var received ManagerImportSourceRequest
				if json.NewDecoder(r.Body).Decode(&received) != nil || received != request {
					t.Error("execution binding was lost")
				}
				result := ManagerImportSource{TenantID: 7, UploaderID: 31, TaskID: 9, SourceLocator: request.SourceLocator}
				switch kind {
				case "tenant":
					result.TenantID = 8
				case "uploader":
					result.UploaderID = 32
				case "task":
					result.TaskID = 10
				case "locator":
					result.SourceLocator += "changed"
				case "redirect":
					http.Redirect(w, r, other.URL, 307)
					return
				}
				_ = json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			registry := exportRegistryStub{modules: []*ModuleInfo{{ModuleName: "manager", Enabled: true, Instances: []ModuleRuntimeInstanceInfo{{Role: ModuleRuntimeRoleBackend, Status: "up", ModuleURL: server.URL, LeaseExpiresAt: time.Now().Add(time.Minute)}}}}}
			tokens := ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
				if tenant != 7 {
					t.Error("wrong token tenant")
				}
				return "transfer-service", nil
			})
			result, err := NewManagerImportSourceClient(registry, tokens).VerifyImportSource(t.Context(), 7, request)
			if (kind == "valid") != (err == nil) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if redirected {
				t.Fatal("credential followed redirect")
			}
		})
	}
}
