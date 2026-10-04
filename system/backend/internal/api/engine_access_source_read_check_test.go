package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	auth "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type sourceReadCheckFixture struct {
	calls      int
	err        error
	incomplete bool
}

func (f *sourceReadCheckFixture) CheckManagerPreviewRead(_ context.Context, credential string, request engineaccess.ManagerPreviewReadCheckRequest) (*engineaccess.SourceReadCheck, error) {
	f.calls++
	if credential != "addp_at_test" || len(request.Targets) != 1 || request.Targets[0].EngineID != 12 {
		return nil, errors.New("unexpected trusted consumer input")
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.incomplete {
		return &engineaccess.SourceReadCheck{}, nil
	}
	return &engineaccess.SourceReadCheck{ObservedAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)}, nil
}

func sourceReadCheckRouter(t *testing.T, f *sourceReadCheckFixture, current auth.AuthContext) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	authenticate, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &current})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	if err := RegisterEngineAccessSourceReadCheckRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authenticate, UserAccessCredential: credential}, &EngineAccessSourceReadCheckHandler{service: f}); err != nil {
		t.Fatal(err)
	}
	return router
}

func sourceReadCheckAuth() auth.AuthContext {
	current := testIAMActorContext("tenant")
	current.Authorization.RoleAssignments = []auth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.preview", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute),
		Scope: auth.AssignmentScope{Type: "tenant", TenantID: current.Context.TenantID}, Permissions: []string{engineaccess.ManagerPreviewReadPermission}}}
	return current
}

func TestSourceReadCheckRouteContract(t *testing.T) {
	const path = "/api/v1/system/engine-access/read-checks/manager-preview"
	target := engineplugin.TabularItemPath(12, "schema", "public", "C")
	body, err := json.Marshal(engineaccess.ManagerPreviewReadCheckRequest{Targets: []engineplugin.EngineCatalogPath{target}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, suffix, body string
		err                        error
		incomplete                 bool
		status, calls              int
	}{
		{"current observation", "POST", "", string(body), nil, false, 200, 1},
		{"empty", "POST", "", `{}`, nil, false, 400, 0},
		{"self reported Tenant", "POST", "", `{"targets":[],"tenant_id":3}`, nil, false, 400, 0},
		{"self reported permission", "POST", "", `{"targets":[],"required_permissions":[]}`, nil, false, 400, 0},
		{"query identity", "POST", "?tenant_id=3", string(body), nil, false, 400, 0},
		{"multiple bodies", "POST", "", string(body) + `{}`, nil, false, 400, 0},
		{"no history GET", "GET", "", "", nil, false, 404, 0},
		{"no rule coverage", "POST", "", string(body), commonapi.ErrForbidden, false, 403, 1},
		{"credential expired", "POST", "", string(body), commonapi.ErrUnauthorized, false, 401, 1},
		{"database failure", "POST", "", string(body), errors.New("private database failure"), false, 500, 1},
		{"incomplete observation", "POST", "", string(body), nil, true, 500, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &sourceReadCheckFixture{err: tc.err, incomplete: tc.incomplete}
			router := sourceReadCheckRouter(t, fixture, sourceReadCheckAuth())
			request := httptest.NewRequest(tc.method, path+tc.suffix, strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer addp_at_test")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status || fixture.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, fixture.calls, response.Body.String())
			}
			if tc.status == 200 {
				var result map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result) != 1 || result["observed_at"] == nil || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("cacheable or expanded observation: %s %v", response.Body.String(), err)
				}
			}
			if strings.Contains(response.Body.String(), "private database") || strings.Contains(response.Body.String(), "addp_at_test") {
				t.Fatal("private details or credential leaked")
			}
		})
	}
	for _, current := range []auth.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-manager")} {
		fixture := &sourceReadCheckFixture{}
		router := sourceReadCheckRouter(t, fixture, current)
		request := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
		request.Header.Set("Authorization", "Bearer addp_at_test")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 403 || fixture.calls != 0 {
			t.Fatalf("unqualified context reached consumer: %d/%d", response.Code, fixture.calls)
		}
	}
}
