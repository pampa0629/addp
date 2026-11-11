package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestDevelopFileRecoveryAfterOriginalExpiry(t *testing.T) {
	_, issued := developFileClientFixture(t, true)
	issued.Authorization.ExpiresAt = time.Now().Add(-time.Minute)
	original := plugin.NewFilePublicationRequest{OperationID: "0b33cbf0-5c67-4c99-8ac3-5573d505630f", Target: issued.Scope.Target.Target, SizeBytes: 1, SHA256: strings.Repeat("a", 64)}
	digest, _ := DevelopFilePublicationRequestDigest(original)
	for _, fault := range []string{"valid", "version", "digest", "expired"} {
		t.Run(fault, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.ContentLength != 0 || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer addp_at_service" {
					t.Error("invalid recovery transport")
				}
				tenant := uint(3)
				now := time.Now()
				response := DevelopFilePublicationRecoveryAccess{
					History:    DevelopFilePublicationHistory{AuthorizationID: issued.Authorization.ID, ExecutionID: issued.Authorization.ExecutionID, Attempt: 1, ScopeDigest: issued.ScopeDigest, ApprovalID: developFileClientApproval, OperationID: original.OperationID, RequestDigest: digest, PrepareAdmittedAt: now.Add(-time.Hour)},
					ObservedAt: now, ExpiresAt: now.Add(10 * time.Second), Engine: &models.Engine{ID: 12, TenantID: &tenant, Version: 4, EngineType: "nfs", LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"server": "recovery"}},
				}
				switch fault {
				case "version":
					response.Engine.Version++
				case "digest":
					response.History.RequestDigest = strings.Repeat("b", 64)
				case "expired":
					response.ExpiresAt = now.Add(-time.Second)
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
			result, err := service.RecoverDevelopFilePublication(context.Background(), issued, developFileClientApproval, original)
			if (result != nil) != (fault == "valid") || (err == nil) != (fault == "valid") {
				t.Fatal("invalid recovery outcome", err)
			}
		})
	}
}
