package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/asset/internal/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/gin-gonic/gin"
)

type permissionRouteContract struct {
	method      string
	path        string
	body        string
	permissions []string
}

func TestRouterEnforcesDeclaredPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousWriter, previousErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = io.Discard, io.Discard
	t.Cleanup(func() {
		gin.DefaultWriter, gin.DefaultErrorWriter = previousWriter, previousErrorWriter
	})
	contracts := assetPermissionRouteContracts()
	tokenPermissions := map[string][]string{
		"Bearer unrelated": {"iam.user.read"},
	}
	for index, contract := range contracts {
		tokenPermissions[fmt.Sprintf("Bearer allowed-%d", index)] = contract.permissions
		for missing := range contract.permissions {
			permissions := append([]string(nil), contract.permissions[:missing]...)
			permissions = append(permissions, contract.permissions[missing+1:]...)
			if len(permissions) == 0 {
				permissions = []string{"iam.user.read"}
			}
			tokenPermissions[fmt.Sprintf("Bearer missing-%d-%d", index, missing)] = permissions
		}
	}

	authServer := authtest.NewTenantUserAuthContextServer(t, "7", tokenPermissions)
	t.Cleanup(authServer.Close)
	router := SetupRouter(nil, authServer.URL, nil, nil)

	for index, contract := range contracts {
		contract := contract
		t.Run(fmt.Sprintf("%s %s", contract.method, contract.path), func(t *testing.T) {
			allowed := performPermissionRequest(router, contract, fmt.Sprintf("allowed-%d", index))
			if allowed.Code == http.StatusUnauthorized || allowed.Code == http.StatusForbidden {
				t.Fatalf("complete permissions rejected: status=%d body=%s", allowed.Code, allowed.Body.String())
			}

			unrelated := performPermissionRequest(router, contract, "unrelated")
			if unrelated.Code != http.StatusForbidden {
				t.Fatalf("unrelated permission status=%d, want %d; body=%s", unrelated.Code, http.StatusForbidden, unrelated.Body.String())
			}

			for missing := range contract.permissions {
				response := performPermissionRequest(router, contract, fmt.Sprintf("missing-%d-%d", index, missing))
				if response.Code != http.StatusForbidden {
					t.Fatalf("missing permission %q status=%d, want %d; body=%s", contract.permissions[missing], response.Code, http.StatusForbidden, response.Body.String())
				}
			}
		})
	}
}

func performPermissionRequest(handler http.Handler, contract permissionRouteContract, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(contract.method, contract.path, bytes.NewBufferString(contract.body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assetPermissionRouteContracts() []permissionRouteContract {
	return []permissionRouteContract{
		{http.MethodGet, "/api/v1/asset/type-definitions", "", managementPermissions(authorization.PermissionAssetEntryRead)},
		{http.MethodGet, "/api/v1/asset/type-definitions/invalid", "", managementPermissions(authorization.PermissionAssetEntryRead)},
		{http.MethodGet, "/api/v1/asset/catalogs", "", managementPermissions(authorization.PermissionAssetCatalogRead)},
		{http.MethodGet, "/api/v1/asset/catalogs/tree", "", managementPermissions(authorization.PermissionAssetCatalogRead)},
		{http.MethodGet, "/api/v1/asset/catalogs/invalid", "", managementPermissions(authorization.PermissionAssetCatalogRead)},
		{http.MethodPost, "/api/v1/asset/catalogs", `{}`, managementPermissions(authorization.PermissionAssetCatalogCreate)},
		{http.MethodPut, "/api/v1/asset/catalogs/invalid", `{}`, managementPermissions(authorization.PermissionAssetCatalogUpdate)},
		{http.MethodDelete, "/api/v1/asset/catalogs/invalid", "", managementPermissions(authorization.PermissionAssetCatalogDelete)},
		{http.MethodGet, "/api/v1/asset/assets", "", managementPermissions(authorization.PermissionAssetEntryRead)},
		{http.MethodGet, "/api/v1/asset/assets/stats", "", managementPermissions(authorization.PermissionAssetEntryRead)},
		{http.MethodGet, "/api/v1/asset/assets/stats/dashboard", "", managementPermissions(authorization.PermissionAssetEntryRead)},
		{http.MethodGet, "/api/v1/asset/assets/type-fields/invalid", "", managementPermissions(authorization.PermissionAssetEntryRead)},
		{http.MethodGet, "/api/v1/asset/assets/invalid", "", managementPermissions(authorization.PermissionAssetEntryRead)},
		{http.MethodPut, "/api/v1/asset/assets/invalid", `{}`, managementPermissions(authorization.PermissionAssetEntryUpdate)},
		{http.MethodDelete, "/api/v1/asset/assets/invalid", "", managementPermissions(authorization.PermissionAssetEntryDelete)},
		{http.MethodPost, "/api/v1/asset/assets/invalid/publish", `{}`, managementPermissions(authorization.PermissionAssetEntryPublish)},
		{http.MethodPost, "/api/v1/asset/assets/invalid/offline", `{}`, managementPermissions(authorization.PermissionAssetEntryOffline)},
		{http.MethodPost, "/api/v1/asset/assets/batch-publish", `{}`, managementPermissions(authorization.PermissionAssetEntryPublish)},
		{http.MethodPost, "/api/v1/asset/assets/batch-offline", `{}`, managementPermissions(authorization.PermissionAssetEntryOffline)},
		{http.MethodPost, "/api/v1/asset/assets/batch-catalog", `{}`, managementPermissions(authorization.PermissionAssetEntryUpdate)},
		{http.MethodPost, "/api/v1/asset/assets/sync", `{}`, managementPermissions(authorization.PermissionAssetEntryUpdate)},
		{http.MethodGet, "/api/v1/asset/applications", "", managementPermissions(authorization.PermissionAssetApplicationRead)},
		{http.MethodGet, "/api/v1/asset/applications/invalid", "", managementPermissions(authorization.PermissionAssetApplicationRead)},
		{http.MethodPost, "/api/v1/asset/applications/invalid/approve", `{}`, managementPermissions(authorization.PermissionAssetApplicationApprove)},
		{http.MethodPost, "/api/v1/asset/applications/invalid/reject", `{}`, managementPermissions(authorization.PermissionAssetApplicationReject)},
		{http.MethodPost, "/api/v1/asset/applications/invalid/revoke", `{}`, managementPermissions(authorization.PermissionAssetApplicationRevoke)},
		{http.MethodGet, "/api/v1/asset/authorizations", "", managementPermissions(authorization.PermissionAssetAuthorizationRead)},
		{http.MethodGet, "/api/v1/asset/authorizations/invalid", "", managementPermissions(authorization.PermissionAssetAuthorizationRead)},
		{http.MethodPost, "/api/v1/asset/authorizations/invalid/revoke", `{}`, managementPermissions(authorization.PermissionAssetAuthorizationRevoke)},
		{http.MethodGet, "/api/v1/asset/ratings", "", managementPermissions(authorization.PermissionAssetRatingRead)},
		{http.MethodPost, "/api/v1/asset/ratings/invalid/mark-handled", `{}`, managementPermissions(authorization.PermissionAssetRatingUpdate)},
		{http.MethodGet, "/api/v1/asset/ratings/stats?asset_id=invalid", "", managementPermissions(authorization.PermissionAssetRatingRead)},
		{http.MethodGet, "/api/v1/asset/consumer/assets", "", []string{authorization.PermissionAssetEntryRead}},
		{http.MethodGet, "/api/v1/asset/consumer/assets/stats", "", []string{authorization.PermissionAssetEntryRead}},
		{http.MethodGet, "/api/v1/asset/consumer/assets/invalid", "", []string{authorization.PermissionAssetEntryRead}},
		{http.MethodGet, "/api/v1/asset/consumer/catalogs", "", []string{authorization.PermissionAssetCatalogRead}},
		{http.MethodPost, "/api/v1/asset/consumer/assets/invalid/applications", `{}`, []string{authorization.PermissionAssetApplicationCreate}},
		{http.MethodGet, "/api/v1/asset/consumer/applications", "", []string{authorization.PermissionAssetApplicationRead}},
		{http.MethodGet, "/api/v1/asset/consumer/assets/invalid/application-status", "", []string{authorization.PermissionAssetApplicationRead, authorization.PermissionAssetAuthorizationRead}},
		{http.MethodGet, "/api/v1/asset/consumer/assets/invalid/ratings", "", []string{authorization.PermissionAssetRatingRead}},
		{http.MethodPost, "/api/v1/asset/consumer/assets/invalid/ratings", `{}`, []string{authorization.PermissionAssetRatingCreate, authorization.PermissionAssetRatingUpdate}},
	}
}

func managementPermissions(keys ...string) []string {
	return append([]string{authorization.PermissionAssetManagementRead}, keys...)
}

func TestRouterPublishesOnlyImplementedTypeDefinitionOperations(t *testing.T) {
	router := SetupRouter(nil, "http://system", nil, nil)
	routes := make(map[string]struct{})
	for _, route := range router.Routes() {
		routes[fmt.Sprintf("%s %s", route.Method, route.Path)] = struct{}{}
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		key := fmt.Sprintf("%s /api/v1/asset/type-definitions", method)
		if method != http.MethodPost {
			key += "/:id"
		}
		if _, exists := routes[key]; exists {
			t.Fatalf("unsupported route remains published: %s", key)
		}
	}

	publicBusinessRoutes := 0
	for _, route := range router.Routes() {
		if len(route.Path) >= len("/api/v1/asset") && route.Path[:len("/api/v1/asset")] == "/api/v1/asset" {
			publicBusinessRoutes++
		}
	}
	if publicBusinessRoutes != 41 {
		t.Fatalf("public business route count = %d, want 41", publicBusinessRoutes)
	}
}
