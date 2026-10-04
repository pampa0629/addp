package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type denyServiceFunc func(context.Context, engineaccess.CreateDenyInput) (*engineaccess.SourceDeny, error)

func (f denyServiceFunc) CreateDeny(ctx context.Context, input engineaccess.CreateDenyInput) (*engineaccess.SourceDeny, error) {
	return f(ctx, input)
}

func (f denyServiceFunc) ReleaseDeny(context.Context, engineaccess.ReleaseDenyInput) (*engineaccess.DenyRelease, error) {
	panic("unexpected release call on creation fixture")
}

func denyTestRouter(t *testing.T, projection *shared.AuthContext, service engineAccessDenyService) *gin.Engine {
	t.Helper()
	authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: projection})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	if err := RegisterEngineAccessDenyRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, UserAccessCredential: credential}, &EngineAccessDenyHandler{service: service}); err != nil {
		t.Fatal(err)
	}
	return router
}

func TestEngineSourceDenyHTTPContract(t *testing.T) {
	id := uuid.New()
	const path = "/api/v1/system/engines/9/access_denies"
	body := map[string]any{"deny_id": id.String(), "catalog_path": engineplugin.TabularItemPath(9, "schema", "public", "events"), "recipient_type": "user", "recipient_id": "9007199254740993", "action": "read", "expiry_mode": "until_revoked", "reason": "Restrict reading"}
	unused := denyServiceFunc(func(context.Context, engineaccess.CreateDenyInput) (*engineaccess.SourceDeny, error) {
		t.Fatal("invalid request reached service")
		return nil, nil
	})
	response := httptest.NewRecorder()
	denyTestRouter(t, nil, unused).ServeHTTP(response, httptest.NewRequest("POST", path, nil))
	if response.Code != 401 {
		t.Fatalf("missing credential=%d", response.Code)
	}
	for _, projection := range []shared.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-catalog")} {
		engineDelegationTestRequest(t, denyTestRouter(t, &projection, unused), "POST", path, body, 403)
	}
	projection := testIAMActorContext("tenant")
	permission := func(key, scope string) {
		assignmentScope := shared.AssignmentScope{Type: scope, TenantID: projection.Context.TenantID}
		if scope == "project_group" {
			groupID := "7"
			assignmentScope.ProjectGroupID = &groupID
		}
		projection.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.deny", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: assignmentScope, Permissions: []string{key}}}
	}
	for _, key := range []string{"system.engine_access_grant.revoke", "system.engine_access_delegation.create", "catalog.entry.update", "system.engine_access_deny.release"} {
		permission(key, "tenant")
		engineDelegationTestRequest(t, denyTestRouter(t, &projection, unused), "POST", path, body, 403)
	}
	permission("system.engine_access_deny.create", "project_group")
	engineDelegationTestRequest(t, denyTestRouter(t, &projection, unused), "POST", path, body, 403)
	permission("system.engine_access_deny.create", "tenant")
	router := denyTestRouter(t, &projection, unused)
	for key, value := range map[string]any{"deny_id": "not-a-uuid", "recipient_id": 4, "tenant_id": "1", "operator": "2"} {
		bad := map[string]any{}
		for k, v := range body {
			bad[k] = v
		}
		bad[key] = value
		engineDelegationTestRequest(t, router, "POST", path, bad, 400)
	}
	engineDelegationTestRequest(t, router, "POST", path+"?tenant_id=1", body, 400)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		engineDelegationTestRequest(t, router, method, path, nil, 404)
	}
	engineDelegationTestRequest(t, router, "POST", path+"/"+id.String()+"/release", body, 403)
	qualified := denyServiceFunc(func(_ context.Context, input engineaccess.CreateDenyInput) (*engineaccess.SourceDeny, error) {
		if input.EngineID != 9 || input.RecipientID != 9007199254740993 || input.Actor.TenantID <= 0 || input.Actor.PrincipalID <= 0 || input.Actor.MembershipID <= 0 || input.DenyID != id {
			t.Fatalf("lost trusted IDs: %+v", input)
		}
		return &engineaccess.SourceDeny{DenyID: id, EngineID: input.EngineID, RecipientID: input.RecipientID, EstablishedByPrincipalID: 9007199254740995}, nil
	})
	response = engineDelegationTestRequest(t, denyTestRouter(t, &projection, qualified), "POST", path, body, 200)
	var fields map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || fields["recipient_id"] != "9007199254740993" || fields["established_by_principal_id"] != "9007199254740995" {
		t.Fatalf("precision lost: %s %v", response.Body, err)
	}
	for _, expected := range []struct {
		err    error
		code   string
		zh, en string
	}{{engineaccess.ErrDenyConflict, "engine_access_deny_conflict", "本次参数与原拒绝记录不一致，请使用原参数查询历史", "Parameters differ from the original Deny; use the original parameters to recover history"},
		{engineaccess.ErrDenyExpiry, "engine_access_deny_expiry", "拒绝期限已不在未来，未建立新规则；请明确选择有效期限", "The Deny expiry is no longer future; no new rule was established. Select an explicit valid expiry"}} {
		service := denyServiceFunc(func(context.Context, engineaccess.CreateDenyInput) (*engineaccess.SourceDeny, error) {
			return nil, expected.err
		})
		for _, language := range []string{"zh-CN", "en"} {
			encoded, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
			req.Header.Set("Authorization", "Bearer addp_at_fixture")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept-Language", language)
			response := httptest.NewRecorder()
			denyTestRouter(t, &projection, service).ServeHTTP(response, req)
			message := expected.en
			if language == "zh-CN" {
				message = expected.zh
			}
			if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || response.Code != 409 || fields["error_code"] != expected.code || fields["error"] != message {
				t.Fatalf("language=%s response=%s err=%v", language, response.Body, err)
			}
		}
	}
}
