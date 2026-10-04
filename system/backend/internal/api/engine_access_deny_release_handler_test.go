package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/google/uuid"
)

type denyReleaseServiceFunc func(context.Context, engineaccess.ReleaseDenyInput) (*engineaccess.DenyRelease, error)

func (f denyReleaseServiceFunc) ReleaseDeny(ctx context.Context, input engineaccess.ReleaseDenyInput) (*engineaccess.DenyRelease, error) {
	return f(ctx, input)
}
func (f denyReleaseServiceFunc) CreateDeny(context.Context, engineaccess.CreateDenyInput) (*engineaccess.SourceDeny, error) {
	panic("unexpected creation call on release fixture")
}

func TestEngineSourceDenyReleaseHTTPContract(t *testing.T) {
	id := uuid.New()
	path := "/api/v1/system/engines/9/access_denies/" + id.String() + "/release"
	body := map[string]any{"reason": "Remove this restriction"}
	unused := denyReleaseServiceFunc(func(context.Context, engineaccess.ReleaseDenyInput) (*engineaccess.DenyRelease, error) {
		t.Fatal("invalid request reached release service")
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
	setPermission := func(key, scope string) {
		assignmentScope := shared.AssignmentScope{Type: scope, TenantID: projection.Context.TenantID}
		if scope == "project_group" {
			groupID := "7"
			assignmentScope.ProjectGroupID = &groupID
		}
		projection.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.deny_release", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: assignmentScope, Permissions: []string{key}}}
	}
	for _, key := range []string{"system.engine_access_deny.create", "system.engine_access_grant.revoke", "system.engine_access_delegation.create", "catalog.entry.update"} {
		setPermission(key, "tenant")
		engineDelegationTestRequest(t, denyTestRouter(t, &projection, unused), "POST", path, body, 403)
	}
	setPermission("system.engine_access_deny.release", "project_group")
	engineDelegationTestRequest(t, denyTestRouter(t, &projection, unused), "POST", path, body, 403)
	setPermission("system.engine_access_deny.release", "tenant")
	router := denyTestRouter(t, &projection, unused)
	for _, bad := range []any{nil, map[string]any{}, map[string]any{"reason": " "}, map[string]any{"reason": strings.Repeat("文", 2001)}, map[string]any{"reason": 1}, map[string]any{"reason": "ok", "tenant_id": "1"}, map[string]any{"reason": "ok", "expires_at": nil}, map[string]any{"reason": "ok", "operator": "2"}} {
		engineDelegationTestRequest(t, router, "POST", path, bad, 400)
	}
	engineDelegationTestRequest(t, router, "POST", path+"?tenant_id=1", body, 400)
	for _, badID := range []string{"not-a-uuid", uuid.Nil.String(), strings.ReplaceAll(id.String(), "-", "")} {
		engineDelegationTestRequest(t, router, "POST", "/api/v1/system/engines/9/access_denies/"+badID+"/release", body, 400)
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		engineDelegationTestRequest(t, router, method, path, nil, 404)
	}
	engineDelegationTestRequest(t, router, "POST", "/api/v1/system/engines/9/access_denies", body, 403)
	qualified := denyReleaseServiceFunc(func(_ context.Context, input engineaccess.ReleaseDenyInput) (*engineaccess.DenyRelease, error) {
		if input.EngineID != 9 || input.DenyID != id || input.Actor.TenantID <= 0 || input.Actor.PrincipalID <= 0 || input.Actor.MembershipID <= 0 {
			t.Fatalf("lost trusted identity: %+v", input)
		}
		return &engineaccess.DenyRelease{DenyID: id, ReleasedByPrincipalID: 9007199254740993, ReleasedByMembershipID: 9007199254740995, Reason: input.Reason}, nil
	})
	response = engineDelegationTestRequest(t, denyTestRouter(t, &projection, qualified), "POST", path, body, 200)
	var fields map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || fields["released_by_principal_id"] != "9007199254740993" || fields["released_by_membership_id"] != "9007199254740995" {
		t.Fatalf("precision lost: %s %v", response.Body, err)
	}
	for _, expected := range []struct {
		err          error
		code, zh, en string
	}{
		{engineaccess.ErrDenyReleaseExpired, "engine_access_deny_expired", "拒绝规则已到期，无须解除", "The Deny has expired; release is unnecessary"},
		{engineaccess.ErrDenyReleaseConflict, "engine_access_deny_release_conflict", "此拒绝规则已解除；本次操作者或原因与原记录不同，请查看原解除历史", "This Deny was released; the operator or reason differs from the original release history"},
	} {
		service := denyReleaseServiceFunc(func(context.Context, engineaccess.ReleaseDenyInput) (*engineaccess.DenyRelease, error) {
			return nil, expected.err
		})
		for _, language := range []string{"zh-CN", "en"} {
			encoded, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", path, strings.NewReader(string(encoded)))
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
