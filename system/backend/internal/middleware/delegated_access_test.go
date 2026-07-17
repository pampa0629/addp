package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
)

func TestDelegatedAccessPolicyAllowsContextResolutionOnlyAsInfrastructureException(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(AuthorizationContextKey, &models.AuthorizationContext{
			SubjectType: models.SubjectTypeUser,
			UserID:      12,
			AuthType:    models.AuthTypeDelegatedAccessToken,
			Audiences:   []string{"manager"},
			Scopes:      []string{"data.search"},
		})
	})
	router.Use(DelegatedAccessPolicy("system", commonAuth.DelegatedRoutePolicy{
		"GET /api/v1/system/engines": {"engine.list"},
	}))
	router.GET("/api/v1/system/auth/context", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/api/v1/system/auth/delegations", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	contextResponse := httptest.NewRecorder()
	router.ServeHTTP(contextResponse, httptest.NewRequest(http.MethodGet, "/api/v1/system/auth/context", nil))
	if contextResponse.Code != http.StatusNoContent {
		t.Fatalf("context resolution status = %d", contextResponse.Code)
	}

	delegationResponse := httptest.NewRecorder()
	router.ServeHTTP(delegationResponse, httptest.NewRequest(http.MethodPost, "/api/v1/system/auth/delegations", nil))
	if delegationResponse.Code != http.StatusForbidden {
		t.Fatalf("delegation chaining status = %d, want 403", delegationResponse.Code)
	}
}
