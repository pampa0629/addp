package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDelegatedAccessPolicyDefaultsToDeny(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setAuthorizationContext(c, AuthorizationContext{
			SubjectType: "user",
			UserID:      12,
			AuthType:    AuthTypeDelegatedAccessToken,
			Audiences:   []string{"develop"},
			Scopes:      []string{"workflow.run"},
		})
	})
	router.Use(DelegatedAccessPolicy("develop", DelegatedRoutePolicy{
		"POST /api/v1/develop/executions": {"workflow.run"},
	}))
	router.POST("/api/v1/develop/executions", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/api/v1/develop/executions/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, httptest.NewRequest(http.MethodPost, "/api/v1/develop/executions", nil))
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("allowed route status = %d", allowed.Code)
	}

	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/api/v1/develop/executions/1", nil))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unregistered route status = %d, want 403", denied.Code)
	}
}

func TestValidateDelegatedAccessRequiresAudienceAndAllScopes(t *testing.T) {
	if !ValidateDelegatedAccess(AuthTypeDelegatedAccessToken, []string{"manager"}, []string{"data.preview"}, "manager", []string{"data.preview"}) {
		t.Fatal("matching delegated access was rejected")
	}
	if ValidateDelegatedAccess(AuthTypeDelegatedAccessToken, []string{"manager"}, []string{"data.search"}, "manager", []string{"data.preview"}) {
		t.Fatal("missing scope was accepted")
	}
	if ValidateDelegatedAccess(AuthTypeDelegatedAccessToken, []string{"meta"}, []string{"data.preview"}, "manager", []string{"data.preview"}) {
		t.Fatal("wrong audience was accepted")
	}
	if !ValidateDelegatedAccess("first_party_access_token", nil, nil, "manager", []string{"data.preview"}) {
		t.Fatal("non-delegated user access should remain on the ordinary permission path")
	}
}
