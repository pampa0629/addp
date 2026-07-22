package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
)

func TestTokenTypePolicyAllowsDelegatedContextResolutionOnlyAsInfrastructureException(t *testing.T) {
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
	router.Use(TokenTypePolicy("system", commonAuth.DelegatedRoutePolicy{
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

func TestTokenTypePolicyRejectsResourceTicketOutsideContextResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(AuthorizationContextKey, &models.AuthorizationContext{
			SubjectType: models.SubjectTypeUser,
			UserID:      12,
			AuthType:    models.AuthTypeResourceAccessTicket,
			Audiences:   []string{"manager"},
			Scopes:      []string{models.BrowserResourceAccessScope},
		})
	})
	router.Use(TokenTypePolicy("system", nil))
	router.GET("/api/v1/system/auth/context", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/api/v1/system/users/me", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/api/v1/system/oauth/authorizations", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	contextResponse := httptest.NewRecorder()
	router.ServeHTTP(contextResponse, httptest.NewRequest(http.MethodGet, "/api/v1/system/auth/context", nil))
	if contextResponse.Code != http.StatusNoContent {
		t.Fatalf("context resolution status = %d", contextResponse.Code)
	}

	for method, path := range map[string]string{
		http.MethodGet:  "/api/v1/system/users/me",
		http.MethodPost: "/api/v1/system/oauth/authorizations",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s status = %d, want 403", method, path, response.Code)
		}
	}
}
