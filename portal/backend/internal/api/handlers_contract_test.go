package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/portal/internal/config"
)

func TestPortalForwardsCurrentUserBearerWithoutIdentityFields(t *testing.T) {
	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/asset/consumer/assets/12/applications" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer user-token" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Errorf("legacy headers were forwarded: %#v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		for _, key := range []string{"tenant_id", "applicant_id", "user_id", "asset_id"} {
			if _, exists := body[key]; exists {
				t.Errorf("body contains identity field %q", key)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"asset_id":12,"applicant_id":9}`))
	}))
	defer assetServer.Close()

	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer user-token": {"asset.application.create"},
	})
	defer authServer.Close()

	cfg := &config.Config{SystemURL: authServer.URL, AssetURL: assetServer.URL}
	assetClient := commonClient.NewAssetClient(assetServer.URL)
	serviceClient := commonClient.NewServiceClient("http://service.invalid", commonClient.ServiceTokenProviderFunc(
		func(context.Context, uint) (string, error) { return "service-token", nil },
	), nil)
	router := SetupRouter(cfg, nil, assetClient, serviceClient)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/portal/assets/12/apply", strings.NewReader(`{"reason":"research","duration_day":7}`))
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-API-Key", "legacy")
	request.Header.Set("X-Tenant-ID", "999")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestPortalApplyRouteRequiresApplicationCreatePermission(t *testing.T) {
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer reader": {"asset.entry.read"},
	})
	defer authServer.Close()
	cfg := &config.Config{SystemURL: authServer.URL}
	router := SetupRouter(cfg, nil, commonClient.NewAssetClient("http://asset.invalid"), commonClient.NewServiceClient(
		"http://service.invalid", commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "token", nil }), nil,
	))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/portal/assets/12/apply", strings.NewReader(`{"reason":"research"}`))
	request.Header.Set("Authorization", "Bearer reader")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", response.Code, response.Body.String())
	}
}

func TestPortalEndpointAuthorizationDependencyFailureIsBadGateway(t *testing.T) {
	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/asset/consumer/assets/12/application-status" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		http.Error(w, "Asset unavailable", http.StatusInternalServerError)
	}))
	defer assetServer.Close()

	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer user-token": {"asset.entry.read", "asset.application.read", "asset.authorization.read"},
	})
	defer authServer.Close()

	serviceCalled := false
	serviceServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		serviceCalled = true
	}))
	defer serviceServer.Close()

	router := SetupRouter(
		&config.Config{SystemURL: authServer.URL}, nil,
		commonClient.NewAssetClient(assetServer.URL),
		commonClient.NewServiceClient(serviceServer.URL, commonClient.ServiceTokenProviderFunc(
			func(context.Context, uint) (string, error) { return "service-token", nil },
		), nil),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/portal/assets/12/endpoints", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", response.Code, response.Body.String())
	}
	if serviceCalled {
		t.Fatal("Service endpoint was called after Asset authorization lookup failed")
	}
}

func TestPortalAssetDetailPreservesDownstreamClientStatus(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		assetStatus int
		wantStatus  int
	}{
		{name: "not found", assetStatus: http.StatusNotFound, wantStatus: http.StatusNotFound},
		{name: "dependency failure", assetStatus: http.StatusInternalServerError, wantStatus: http.StatusBadGateway},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Asset error", testCase.assetStatus)
			}))
			defer assetServer.Close()
			authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
				"Bearer user-token": {"asset.entry.read"},
			})
			defer authServer.Close()

			router := SetupRouter(
				&config.Config{SystemURL: authServer.URL}, nil,
				commonClient.NewAssetClient(assetServer.URL),
				commonClient.NewServiceClient("http://service.invalid", commonClient.ServiceTokenProviderFunc(
					func(context.Context, uint) (string, error) { return "service-token", nil },
				), nil),
			)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/portal/assets/12", nil)
			request.Header.Set("Authorization", "Bearer user-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, testCase.wantStatus, response.Body.String())
			}
		})
	}
}
