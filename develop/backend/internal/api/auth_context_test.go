package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	commonAuthorization "github.com/addp/common/authorization"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func setTenantAuthContextForTest(c *gin.Context, tenantID, userID uint) {
	tenantIDText := strconv.FormatUint(uint64(tenantID), 10)
	userIDText := strconv.FormatUint(uint64(userID), 10)
	membershipID := "1"
	clientID := "addp-web"
	issuedAt := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	authContext := commonAuthorization.AuthContext{
		SchemaVersion: commonAuthorization.AuthContextSchemaVersion,
		Principal:     commonAuthorization.AuthPrincipal{Type: "user", ID: userIDText},
		Context: commonAuthorization.AuthSessionContext{
			Type: "tenant", TenantID: &tenantIDText, TenantMembershipID: &membershipID,
		},
		Authentication: commonAuthorization.AuthenticationFacts{
			Methods: []string{"password"}, AssuranceLevel: "aal1", AuthenticatedAt: issuedAt,
		},
		Client: commonAuthorization.ClientConstraints{
			ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted", Scopes: []string{},
		},
		Organization: commonAuthorization.OrganizationContext{
			Departments: []commonAuthorization.DepartmentMembership{}, ProjectGroups: []commonAuthorization.ProjectGroupMembership{},
		},
		Authorization: commonAuthorization.AuthorizationFacts{
			AuthorizationVersion: "1", RoleAssignments: []commonAuthorization.RoleAssignment{},
		},
		Token: commonAuthorization.TokenFacts{
			Type: "first_party_access_token", IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour),
		},
	}
	if err := commonAuth.SetAuthContextForGin(c, authContext); err != nil {
		panic(err)
	}
}

func TestInternalAPIKeyMiddlewareCreatesOnlyInternalTenantContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(internalAPIKeyMiddleware("secret"))
	router.GET("/resource", func(c *gin.Context) {
		if _, exists := commonAuth.AuthContextFromGin(c); exists {
			t.Fatal("internal API key must not create AuthContext")
		}
		if tenantIDValue(c) != 0 || userIDValue(c) != 0 {
			t.Fatal("internal API key must not create user identity")
		}
		c.JSON(http.StatusOK, gin.H{"tenant_id": internalTenantIDValue(c)})
	})

	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	request.Header.Set("X-Internal-API-Key", "secret")
	request.Header.Set("X-Tenant-ID", "7")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "{\"tenant_id\":7}" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestInternalAPIKeyMiddlewareFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		tenantID   string
		wantStatus int
	}{
		{name: "invalid key", key: "wrong", tenantID: "7", wantStatus: http.StatusUnauthorized},
		{name: "missing tenant", key: "secret", wantStatus: http.StatusBadRequest},
		{name: "zero tenant", key: "secret", tenantID: "0", wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(internalAPIKeyMiddleware("secret"))
			router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodGet, "/resource", nil)
			request.Header.Set("X-Internal-API-Key", test.key)
			request.Header.Set("X-Tenant-ID", test.tenantID)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d, want=%d", response.Code, test.wantStatus)
			}
		})
	}
}
