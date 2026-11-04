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
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/transfer/internal/service"
)

func TestTaskConfigDraftRouteIsolatedScopeAndPermissions(t *testing.T) {
	for _, name := range []string{"user", "delegated", "missing_manager", "missing_create", "missing_meta", "missing_system", "wrong_scope", "extra_scope"} {
		t.Run(name, func(t *testing.T) {
			permissions := []string{"manager.data_item.read", "meta.catalog.read", "system.engine.read", "transfer.task.create"}
			subject := authtest.NewTenantUserAuthContext("7", "91", permissions)
			want := 502
			switch name {
			case "missing_manager":
				subject.Authorization.RoleAssignments[0].Permissions = permissions[1:]
				want = 403
			case "missing_meta":
				subject.Authorization.RoleAssignments[0].Permissions = []string{permissions[0], permissions[2], permissions[3]}
				want = 403
			case "missing_system":
				subject.Authorization.RoleAssignments[0].Permissions = []string{permissions[0], permissions[1], permissions[3]}
				want = 403
			case "missing_create":
				subject.Authorization.RoleAssignments[0].Permissions = permissions[:3]
				want = 403
			}
			if name == "delegated" || name == "wrong_scope" || name == "extra_scope" {
				subject.Token.Type = "delegated_access_token"
				subject.Client.ScopeMode = "restricted"
				subject.Client.Audiences = []string{"transfer"}
				subject.Client.Scopes = []string{"transfer.task.draft.build"}
				subject.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *subject.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
			}
			if name == "wrong_scope" {
				subject.Client.Scopes = []string{"transfer.task.config.check"}
				want = 403
			}
			if name == "extra_scope" {
				subject.Client.Scopes = append(subject.Client.Scopes, "transfer.task.create")
				want = 403
			}
			sort.Strings(subject.Client.Scopes)
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(subject) }))
			defer system.Close()
			db := newTransferTaskHandlerTestDB(t)
			svc := service.NewTaskService(db, nil, nil)
			svc.SetTaskConfigDrafter(service.NewTaskConfigDraftService(nil, nil))
			router := SetupRouter(NewCleanupResultHandler(nil), svc, nil, nil, nil, system.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
			body := `{"name":"draft","source_locator":"addp://engine/1/path/synthetic/source?type=collection","target":{"parent_locator":"addp://engine/2/path/synthetic?type=schema","name":"target","apply_mode":"replace"},"row_grain":{"mode":"document"},"fields":[{"path":["payload"],"target":"payload"}]}`
			req := httptest.NewRequest("POST", "/api/v1/transfer/task-config-drafts", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer addp_dat_fixture")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body)
			}
			if response.Code == 502 && !strings.Contains(response.Body.String(), "draft_facts_unavailable") {
				t.Fatal(response.Body)
			}
		})
	}
}
