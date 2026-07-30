package api

import (
	"net/http/httptest"
	"testing"
	"time"

	commonAuthorization "github.com/addp/common/authorization"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func TestCanonicalAuthContextIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	tenantID := "7"
	membershipID := "2"
	clientID := "addp-web"
	issuedAt := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	authContext := commonAuthorization.AuthContext{
		SchemaVersion: commonAuthorization.AuthContextSchemaVersion,
		Principal:     commonAuthorization.AuthPrincipal{Type: "user", ID: "9"},
		Context: commonAuthorization.AuthSessionContext{
			Type: "tenant", TenantID: &tenantID, TenantMembershipID: &membershipID,
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
		t.Fatalf("set AuthContext: %v", err)
	}
	if getTenantID(c) != 7 || getUserID(c) != 9 {
		t.Fatalf("tenant=%d user=%d, want tenant=7 user=9", getTenantID(c), getUserID(c))
	}
}
