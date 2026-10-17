package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fakeTaskSubjectService struct {
	calls int
	input iam.AuthorizeTaskSubjectInput
}

func (f *fakeTaskSubjectService) Authorize(_ context.Context, input iam.AuthorizeTaskSubjectInput) (*iam.ResolvedTaskAuthorizationSubject, error) {
	f.calls++
	f.input = input
	return &iam.ResolvedTaskAuthorizationSubject{ID: 81, OwnerModule: input.OwnerModule, TaskType: input.TaskType, TaskRef: input.TaskRef, DefinitionHash: input.DefinitionHash, TenantID: input.TenantID, PrincipalID: input.PrincipalID, TenantMembershipID: input.TenantMembershipID, AuthorizationVersion: input.AuthorizationVersion, AuthorizedAt: time.Now().UTC()}, nil
}
func (f *fakeTaskSubjectService) Resolve(context.Context, iam.ResolveTaskSubjectInput) (*iam.ResolvedTaskAuthorizationSubject, error) {
	panic("unexpected resolve")
}
func TestTaskSubjectAuthorizeUsesStrictOwnerPermissionAndVerifiedUser(t *testing.T) {
	for _, scenario := range []string{"transfer", "orchestrator", "wrong_owner_permission", "missing_create", "unknown_owner", "unknown_type", "spoofed_principal", "service"} {
		t.Run(scenario, func(t *testing.T) {
			fake := &fakeTaskSubjectService{}
			handler, _ := NewIAMTaskAuthorizationSubjectHandler(fake)
			facts := testIAMActorContext("tenant")
			permissions := []string{"system.execution_authorization.create", "transfer.task.execute"}
			request := map[string]any{"owner_module": "transfer", "task_type": "sync", "task_ref": uuid.NewString(), "definition_hash": strings.Repeat("a", 64)}
			want := 200
			switch scenario {
			case "orchestrator":
				request["owner_module"] = "orchestrator"
				request["task_type"] = "orchestration"
				permissions[1] = "orchestrator.workflow.execute"
			case "wrong_owner_permission":
				permissions[1] = "orchestrator.workflow.execute"
				want = 403
			case "missing_create":
				permissions = permissions[1:]
				want = 403
			case "unknown_owner":
				request["owner_module"] = "manager"
				want = 400
			case "unknown_type":
				request["task_type"] = "orchestration"
				want = 400
			case "spoofed_principal":
				request["principal_id"] = "99"
				want = 400
			case "service":
				facts = testIAMServiceActorContext("tenant", "addp-transfer")
				want = 403
			}
			sort.Strings(permissions)
			tenant := *facts.Context.TenantID
			facts.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.test", Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: &tenant}, Permissions: permissions, SourceType: "manual", ValidFrom: time.Now().Add(-time.Hour)}}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if err := sharedauth.SetAuthContextForGin(c, facts); err != nil {
					t.Fatal(err)
				}
			})
			guard, _ := middleware.NewIAMPermissionGuard("system.execution_authorization.create")
			router.POST("/subject", guard, handler.Authorize)
			response := performIAMJSONRequest(t, router, http.MethodPost, "/subject", request, nil)
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body)
			}
			if want == 200 {
				if fake.calls != 1 || fake.input.PrincipalID != 12 || fake.input.TenantMembershipID != 4 || fake.input.TenantID != 3 || fake.input.AuthorizationVersion != 1 {
					t.Fatal("subject did not use verified current User")
				}
			} else if fake.calls != 0 {
				t.Fatal("rejected request reached service")
			}
		})
	}
}
