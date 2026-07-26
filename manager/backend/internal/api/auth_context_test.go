package api

import (
	"strconv"
	"time"

	commonauth "github.com/addp/common/authorization"
	authmiddleware "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func setTenantAuthContextForTest(c *gin.Context, tenantID, userID uint) {
	tenantIDText := strconv.FormatUint(uint64(tenantID), 10)
	userIDText := strconv.FormatUint(uint64(userID), 10)
	membershipID := "1"
	clientID := "addp-web"
	issuedAt := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	authContext := commonauth.AuthContext{
		SchemaVersion: commonauth.AuthContextSchemaVersion,
		Principal:     commonauth.AuthPrincipal{Type: "user", ID: userIDText},
		Context: commonauth.AuthSessionContext{
			Type:               "tenant",
			TenantID:           &tenantIDText,
			TenantMembershipID: &membershipID,
		},
		Authentication: commonauth.AuthenticationFacts{
			Methods:         []string{"password"},
			AssuranceLevel:  "aal1",
			AuthenticatedAt: issuedAt,
		},
		Client: commonauth.ClientConstraints{
			ClientID:  &clientID,
			Audiences: []string{"addp.api"},
			ScopeMode: "unrestricted",
			Scopes:    []string{},
		},
		Organization: commonauth.OrganizationContext{
			Departments:   []commonauth.DepartmentMembership{},
			ProjectGroups: []commonauth.ProjectGroupMembership{},
		},
		Authorization: commonauth.AuthorizationFacts{
			AuthorizationVersion: "1",
			RoleAssignments:      []commonauth.RoleAssignment{},
		},
		Token: commonauth.TokenFacts{
			Type:      "first_party_access_token",
			IssuedAt:  issuedAt,
			ExpiresAt: issuedAt.Add(time.Hour),
		},
	}
	if err := authmiddleware.SetAuthContextForGin(c, authContext); err != nil {
		panic(err)
	}
}
