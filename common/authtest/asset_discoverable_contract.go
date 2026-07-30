package authtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// AssertAssetDiscoverableContract verifies the canonical service identity,
// Permission, and legacy-header rejection contract on an owner route.
func AssertAssetDiscoverableContract(t *testing.T, handler http.Handler, path, wantName string) {
	t.Helper()

	t.Run("asset service with permission", func(t *testing.T) {
		response := performRequest(handler, path, AssetServiceToken, false)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
		}
		var items []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(items) != 1 || items[0].Name != wantName {
			t.Fatalf("items = %#v, want one item named %q", items, wantName)
		}
	})

	for _, testCase := range []struct {
		name       string
		token      string
		legacy     bool
		wantStatus int
	}{
		{name: "asset service without permission", token: AssetServiceNoPermissionToken, wantStatus: http.StatusForbidden},
		{name: "different service client", token: OtherServiceToken, wantStatus: http.StatusForbidden},
		{name: "user actor", token: UserToken, wantStatus: http.StatusForbidden},
		{name: "legacy internal headers", legacy: true, wantStatus: http.StatusUnauthorized},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := performRequest(handler, path, testCase.token, testCase.legacy)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.wantStatus, response.Body.String())
			}
		})
	}
}

func performRequest(handler http.Handler, path, token string, legacy bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if legacy {
		request.Header.Set("X-Internal-API-Key", "legacy-internal-key")
		request.Header.Set("X-Tenant-ID", "7")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
