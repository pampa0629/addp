package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/modulelifecycle"
)

type auditTestTokens struct{}

func (auditTestTokens) Token(context.Context, uint) (string, error) {
	return "audit-service-token", nil
}
func (auditTestTokens) PlatformToken(context.Context) (string, error) {
	return "platform-service-token", nil
}

func TestDiagnosticReadAuditRecordsPermissionDenialWithoutQueryOrUserCredential(t *testing.T) {
	facts := monitorTenantAuthContext()
	facts.Authorization.RoleAssignments = []authorization.RoleAssignment{}
	received := make(chan []byte, 1)
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/tenant/audit/events" {
			body, _ := io.ReadAll(r.Body)
			if r.Header.Get("Authorization") != "Bearer audit-service-token" {
				t.Errorf("wrong audit credential")
			}
			received <- body
			w.WriteHeader(204)
			return
		}
		_ = json.NewEncoder(w).Encode(facts)
	}))
	defer system.Close()
	systemClient := client.NewSystemServiceClient(system.URL, auditTestTokens{}, system.Client())
	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, system.URL, nil, systemClient, modulelifecycle.NewStandalone("monitor"), nil, nil)
	request := httptest.NewRequest("GET", "/api/v1/monitor/executions?keyword=private-query-sentinel", nil)
	request.Header.Set("Authorization", "Bearer private-user-sentinel")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatalf("status=%d", response.Code)
	}
	select {
	case payload := <-received:
		if strings.Contains(string(payload), "private-") || strings.Contains(string(payload), "Authorization") {
			t.Fatalf("audit leaked request: %s", payload)
		}
		var event map[string]interface{}
		if json.Unmarshal(payload, &event) != nil || event["result"] != "denied" || event["event_name"] != "execution.diagnostics.read" {
			t.Fatalf("audit=%s", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("permission denial was not audited")
	}
}
