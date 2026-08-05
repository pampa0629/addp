package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	authmiddleware "github.com/addp/common/middleware/auth"
	managerauthorization "github.com/addp/manager/internal/authorization"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEmbeddingConfigurationRoutesRejectTenantContext(t *testing.T) {
	router := newEmbeddingConfigurationTestRouter(t, func(c *gin.Context) {
		setTenantAuthContextForTest(c, 1, 7)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/manager/settings/embedding", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func TestEmbeddingConfigurationRoutesReturnConflictForStaleVersion(t *testing.T) {
	router := newEmbeddingConfigurationTestRouter(t, setPlatformEmbeddingConfigurationAuthContextForTest)
	payload := `{
		"version":0,
		"max_distance":0.7,
		"max_file_size_mb":20,
		"batch_concurrency":4
	}`

	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/manager/settings/embedding", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(first, request)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body = %s", first.Code, http.StatusOK, first.Body.String())
	}

	stale := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPut, "/api/v1/manager/settings/embedding", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(stale, request)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status = %d, want %d; body = %s", stale.Code, http.StatusConflict, stale.Body.String())
	}
}

func newEmbeddingConfigurationTestRouter(t *testing.T, setAuthContext func(*gin.Context)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ATTACH DATABASE ':memory:' AS manager`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.EmbeddingConfiguration{}); err != nil {
		t.Fatal(err)
	}
	configurationService := service.NewEmbeddingConfigurationService(repository.NewEmbeddingConfigurationRepository(db))
	if err := configurationService.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := NewEmbeddingConfigurationHandler(configurationService)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		setAuthContext(c)
		c.Next()
	})
	platform := router.Group("/api/v1/manager")
	platform.Use(authmiddleware.MustNewContextGuard("platform"))
	platform.GET("/settings/embedding", authmiddleware.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationRead), handler.Get)
	platform.PUT("/settings/embedding", authmiddleware.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationUpdate), handler.Update)
	return router
}

func setPlatformEmbeddingConfigurationAuthContextForTest(c *gin.Context) {
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	clientID := "addp-web"
	authContext := commonauth.AuthContext{
		SchemaVersion: commonauth.AuthContextSchemaVersion,
		Principal:     commonauth.AuthPrincipal{Type: "user", ID: "7"},
		Context:       commonauth.AuthSessionContext{Type: "platform"},
		Authentication: commonauth.AuthenticationFacts{
			Methods: []string{"password", "totp"}, AssuranceLevel: "aal2", AuthenticatedAt: now,
		},
		Client: commonauth.ClientConstraints{
			ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted", Scopes: []string{},
		},
		Organization: commonauth.OrganizationContext{
			Departments: []commonauth.DepartmentMembership{}, ProjectGroups: []commonauth.ProjectGroupMembership{},
		},
		Authorization: commonauth.AuthorizationFacts{
			AuthorizationVersion: "1",
			RoleAssignments: []commonauth.RoleAssignment{{
				AssignmentID: "1", RoleKey: "platform.system_administrator",
				Scope: commonauth.AssignmentScope{Type: "platform"},
				Permissions: []string{
					managerauthorization.PermissionManagerConfigurationRead,
					managerauthorization.PermissionManagerConfigurationUpdate,
				},
				SourceType: "bootstrap", ValidFrom: now,
			}},
		},
		Token: commonauth.TokenFacts{
			Type: "first_party_access_token", IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		},
	}
	if err := authmiddleware.SetAuthContextForGin(c, authContext); err != nil {
		panic(err)
	}
}
