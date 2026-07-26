package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type iamActorResolver struct {
	authContext *commonauth.AuthContext
}

func (resolver iamActorResolver) ResolveAuthContext(context.Context, string) (*commonauth.AuthContext, error) {
	return resolver.authContext, nil
}

func TestIAMTenantUserActorUsesOnlyCanonicalAuthContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := testIAMActorContext("tenant")
	for _, testCase := range []struct {
		name       string
		context    commonauth.AuthContext
		wantStatus int
	}{
		{name: "tenant user", context: valid, wantStatus: http.StatusNoContent},
		{name: "platform user", context: testIAMActorContext("platform"), wantStatus: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &testCase.context})
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.GET("/actor", authentication, func(c *gin.Context) {
				principalID, tenantID, err := iamTenantUserActor(c)
				if err != nil {
					respondIAMError(c, err)
					return
				}
				if principalID != 12 || tenantID != 3 {
					t.Fatalf("actor = (%d, %d)", principalID, tenantID)
				}
				for _, key := range []string{"user_id", "tenant_id"} {
					if _, exists := c.Get(key); exists {
						t.Fatalf("legacy context key %q was projected", key)
					}
				}
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodGet, "/actor", nil)
			request.Header.Set("Authorization", "Bearer addp_at_test")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func testIAMActorContext(contextType string) commonauth.AuthContext {
	now := time.Now().UTC()
	tenantID := "3"
	membershipID := "4"
	clientID := "addp-web"
	context := commonauth.AuthSessionContext{Type: contextType}
	assuranceLevel := "aal2"
	if contextType == "tenant" {
		context.TenantID = &tenantID
		context.TenantMembershipID = &membershipID
		assuranceLevel = "aal1"
	}
	return commonauth.AuthContext{
		SchemaVersion: commonauth.AuthContextSchemaVersion,
		Principal:     commonauth.AuthPrincipal{Type: "user", ID: "12"},
		Context:       context,
		Authentication: commonauth.AuthenticationFacts{
			Methods:         []string{"password"},
			AssuranceLevel:  assuranceLevel,
			AuthenticatedAt: now.Add(-time.Minute),
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
			Type:      middleware.IAMTokenTypeFirstPartyAccess,
			IssuedAt:  now,
			ExpiresAt: now.Add(15 * time.Minute),
		},
	}
}
