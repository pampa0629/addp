package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/gin-gonic/gin"
)

type currentRolesFixture struct {
	calls  int
	bearer string
	err    error
}

func (f *currentRolesFixture) ResolveCurrentSubjectRoles(_ context.Context, bearer string) (*authorization.CurrentSubjectRoles, error) {
	f.calls++
	f.bearer = bearer
	return &authorization.CurrentSubjectRoles{RoleKeys: []string{"custom.analysis"}}, f.err
}

func TestCurrentSubjectRolesAcceptsOnlyOwnCredentialAndNoSelectors(t *testing.T) {
	for _, test := range []struct {
		name, query, body, bearer string
		status                    int
		calls                     int
	}{
		{"own", "", "", "Bearer addp_at_current", 200, 1},
		{"another user", "?principal_id=42", "", "Bearer addp_at_current", 400, 0},
		{"tenant", "?tenant_id=9", "", "Bearer addp_at_current", 400, 0},
		{"role", "?role_key=custom.analysis", "", "Bearer addp_at_current", 400, 0},
		{"body", "", `{}`, "Bearer addp_at_current", 400, 0},
		{"no credential", "", "", "", 401, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &currentRolesFixture{}
			router := gin.New()
			router.GET("/auth/subject-roles", (&IAMCurrentSubjectRolesHandler{service: fixture}).Get)
			request := httptest.NewRequest(http.MethodGet, "/auth/subject-roles"+test.query, strings.NewReader(test.body))
			request.Header.Set("Authorization", test.bearer)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || fixture.calls != test.calls || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d calls=%d", response.Code, fixture.calls)
			}
			if fixture.calls > 0 && fixture.bearer != "addp_at_current" {
				t.Fatal("credential substituted")
			}
		})
	}
	fixture := &currentRolesFixture{err: commonapi.ErrUnauthorized}
	router := gin.New()
	router.GET("/auth/subject-roles", (&IAMCurrentSubjectRolesHandler{service: fixture}).Get)
	request := httptest.NewRequest(http.MethodGet, "/auth/subject-roles", nil)
	request.Header.Set("Authorization", "Bearer addp_at_revoked")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 401 || fixture.calls != 1 {
		t.Fatal("upstream rejection ignored")
	}
}
