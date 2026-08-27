package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/addp/common/authorization/authtest"
	commonAuth "github.com/addp/common/middleware/auth"
	workbenchauthorization "github.com/addp/workbench/internal/authorization"
	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/repository"
	"github.com/addp/workbench/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResourceGrantRoutesRequireAssetServiceAndPersistOwnerRule(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:workbench-resource-grant-api?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS workbench").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE workbench.resource_access_rules (
		id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, resource_type TEXT NOT NULL, resource_id TEXT NOT NULL,
		subject_type TEXT NOT NULL, subject_id INTEGER NOT NULL, permission TEXT NOT NULL, effect TEXT NOT NULL,
		source_module TEXT NOT NULL, source_identity TEXT NOT NULL, expires_at DATETIME, revoked_at DATETIME,
		created_at DATETIME, updated_at DATETIME, UNIQUE(tenant_id, source_module, source_identity))`).Error; err != nil {
		t.Fatal(err)
	}
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer asset-token": {
			ClientID:    "addp-asset",
			Permissions: []string{workbenchauthorization.PermissionWorkbenchResourceGrantCreate, workbenchauthorization.PermissionWorkbenchResourceGrantRevoke},
		},
		"Bearer wrong-client":  {ClientID: "addp-catalog", Permissions: []string{workbenchauthorization.PermissionWorkbenchResourceGrantCreate}},
		"Bearer no-permission": {ClientID: "addp-asset", Permissions: []string{workbenchauthorization.PermissionWorkbenchCatalogRead}},
	})
	defer authServer.Close()

	handler := NewResourceGrantHandler(service.NewResourceGrantService(repository.NewResourceAccessRuleRepository(db)))
	router := gin.New()
	api := router.Group("/api/v1/workbench")
	api.Use(commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: authServer.URL}), commonAuth.MustNewContextGuard("tenant"))
	routes := api.Group("/runtime/resource-grants")
	routes.Use(commonAuth.MustNewServiceClientGuard("addp-asset"))
	routes.PUT("/:source_identity", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchResourceGrantCreate), handler.FulfillAssetGrant)
	routes.DELETE("/:source_identity", commonAuth.MustNewPermissionGuard(workbenchauthorization.PermissionWorkbenchResourceGrantRevoke), handler.RevokeAssetGrant)

	body := `{"resource_type":"data_application","resource_id":"1714dcf7-f34e-4996-a8dc-3b88998ebe55","subject_type":"user","subject_id":"91","permission":"workbench.data_application.execute"}`
	response := performWorkbenchCatalogRequest(router, http.MethodPut, "/api/v1/workbench/runtime/resource-grants/73", "asset-token", body)
	if response.Code != http.StatusOK {
		t.Fatalf("fulfill status=%d body=%s", response.Code, response.Body.String())
	}
	var fulfilled models.AssetResourceGrantResponse
	if err := json.Unmarshal(response.Body.Bytes(), &fulfilled); err != nil {
		t.Fatal(err)
	}
	if fulfilled.Status != models.ResourceGrantFulfillmentStatusActive || fulfilled.SourceIdentity != "73" {
		t.Fatalf("fulfilled=%#v", fulfilled)
	}
	response = performWorkbenchCatalogRequest(router, http.MethodDelete, "/api/v1/workbench/runtime/resource-grants/73", "asset-token", body)
	if response.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", response.Code, response.Body.String())
	}

	for _, testCase := range []struct {
		token string
		want  int
	}{{"wrong-client", http.StatusForbidden}, {"no-permission", http.StatusForbidden}, {"", http.StatusUnauthorized}} {
		response := performWorkbenchCatalogRequest(router, http.MethodPut, "/api/v1/workbench/runtime/resource-grants/74", testCase.token, body)
		if response.Code != testCase.want {
			t.Fatalf("token=%q status=%d want=%d body=%s", testCase.token, response.Code, testCase.want, response.Body.String())
		}
	}
}
