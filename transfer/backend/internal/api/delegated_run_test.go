package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/transfer/internal/service"
	"github.com/google/uuid"
)

func TestDelegatedTransferRunRouteBoundary(t *testing.T) {
	db := newTransferTaskHandlerTestDB(t)
	svc := service.NewTaskService(db, nil, nil)
	executions := service.NewExecutionService(db, execution.NewTaskExecutionRepository(db))
	svc.SetExecutionService(executions)
	for _, tc := range []struct {
		name, method, path, scope, body string
		extra                           bool
		want                            int
	}{
		{name: "start_reaches_owner", method: "POST", path: "/task-definitions/999/start", scope: "transfer.task.start", body: "{}", want: 404},
		{name: "read_reaches_owner", method: "GET", path: "/executions/" + uuid.NewString(), scope: "transfer.execution.get", want: 404},
		{name: "cannot_self_approve", method: "POST", path: "/run-approvals/" + uuid.NewString() + "/decision", scope: "transfer.task.start", body: `{"decision":"approved"}`, want: 403},
		{name: "cannot_read_approval", method: "GET", path: "/run-approvals/" + uuid.NewString(), scope: "transfer.execution.get", want: 403},
		{name: "read_scope_cannot_start", method: "POST", path: "/task-definitions/999/start", scope: "transfer.execution.get", body: "{}", want: 403},
		{name: "start_scope_cannot_read_execution", method: "GET", path: "/executions/" + uuid.NewString(), scope: "transfer.task.start", want: 403},
		{name: "extra_scope_denied", method: "POST", path: "/task-definitions/999/start", scope: "transfer.task.start", extra: true, body: "{}", want: 403},
		{name: "model_approved_flag_denied", method: "POST", path: "/task-definitions/999/start", scope: "transfer.task.start", body: `{"approved":true}`, want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := authtest.NewTenantUserAuthContext("7", "42", []string{"transfer.task.execute", "transfer.task.read"})
			a.Token.Type = "delegated_access_token"
			a.Client.Audiences = []string{"transfer"}
			a.Client.ScopeMode = "restricted"
			a.Client.Scopes = []string{tc.scope}
			if tc.extra {
				a.Client.Scopes = append(a.Client.Scopes, "transfer.task.create")
				sort.Strings(a.Client.Scopes)
			}
			a.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *a.Client.ClientID, AgentRunID: uuid.NewString(), ToolCallID: uuid.NewString()}
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/system/auth/context" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(a)
			}))
			defer system.Close()
			router := SetupRouter(NewCleanupResultHandler(nil), svc, executions, nil, nil, system.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
			req := httptest.NewRequest(tc.method, "/api/v1/transfer"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer delegated-fixture")
			req.Header.Set("Content-Type", "application/json")
			result := httptest.NewRecorder()
			router.ServeHTTP(result, req)
			if result.Code != tc.want {
				t.Fatalf("status=%d want %d body=%s", result.Code, tc.want, result.Body.String())
			}
		})
	}
}
