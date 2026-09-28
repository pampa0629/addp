package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/modulelifecycle"
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
	router := SetupRouter(cfg, nil, assetClient, modulelifecycle.NewStandalone("portal"))

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
	router := SetupRouter(cfg, nil, commonClient.NewAssetClient("http://asset.invalid"), modulelifecycle.NewStandalone("portal"))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/portal/assets/12/apply", strings.NewReader(`{"reason":"research"}`))
	request.Header.Set("Authorization", "Bearer reader")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", response.Code, response.Body.String())
	}
}

func TestPortalRatingRoutesUseIndependentPermissionsAndUserBearer(t *testing.T) {
	forwarded := make([]string, 0, 2)
	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/asset/consumer/assets/12/ratings" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer create" && r.Header.Get("Authorization") != "Bearer update" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		forwarded = append(forwarded, r.Method+" "+r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		_, _ = w.Write([]byte(`{"id":1,"asset_id":12,"user_id":9,"score":5}`))
	}))
	defer assetServer.Close()
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer create": {"asset.rating.create"},
		"Bearer update": {"asset.rating.update"},
		"Bearer reader": {"asset.rating.read"},
	})
	defer authServer.Close()
	router := SetupRouter(&config.Config{SystemURL: authServer.URL}, nil, commonClient.NewAssetClient(assetServer.URL), modulelifecycle.NewStandalone("portal"))
	request := func(method, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/portal/assets/12/ratings", strings.NewReader(`{"score":5}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	for _, testCase := range []struct {
		method, token string
		want          int
	}{
		{http.MethodPost, "reader", http.StatusForbidden},
		{http.MethodPut, "reader", http.StatusForbidden},
		{http.MethodPut, "create", http.StatusForbidden},
		{http.MethodPost, "update", http.StatusForbidden},
		{http.MethodPost, "create", http.StatusCreated},
		{http.MethodPut, "update", http.StatusOK},
	} {
		response := request(testCase.method, testCase.token)
		if response.Code != testCase.want {
			t.Fatalf("%s %s: status=%d, want %d; body=%s", testCase.method, testCase.token, response.Code, testCase.want, response.Body.String())
		}
	}
	if len(forwarded) != 2 || forwarded[0] != "POST Bearer create" || forwarded[1] != "PUT Bearer update" {
		t.Fatalf("unexpected downstream calls: %#v", forwarded)
	}
}

func TestPortalRatingProjectionUsesOwnerSummaryBeyondFirstPage(t *testing.T) {
	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/asset/consumer/assets/12/ratings" || r.Header.Get("Authorization") != "Bearer reader" {
			t.Errorf("unexpected Asset request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":2,"user_id":10,"score":1}],"total":2,"avg_score":3,"my_rating":{"id":1,"user_id":9,"score":5}}`))
	}))
	defer assetServer.Close()
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer reader": {"asset.rating.read"},
	})
	defer authServer.Close()
	router := SetupRouter(&config.Config{SystemURL: authServer.URL}, nil, commonClient.NewAssetClient(assetServer.URL), modulelifecycle.NewStandalone("portal"))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/portal/assets/12/ratings", nil)
	request.Header.Set("Authorization", "Bearer reader")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		MyRating struct {
			UserID int64 `json:"user_id"`
		} `json:"my_rating"`
		AvgScore float64 `json:"avg_score"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode Portal ratings: %v", err)
	}
	if result.MyRating.UserID != 9 || result.AvgScore != 3 {
		t.Fatalf("Portal did not preserve Asset summary: %#v", result)
	}
}

func TestPortalOwnRatingProjectionNeedsUpdatePermission(t *testing.T) {
	requests := 0
	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/asset/consumer/assets/12/my-rating" || r.Header.Get("Authorization") != "Bearer update" {
			t.Errorf("unexpected Asset request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rating":{"id":1,"asset_id":12,"user_id":9,"score":5}}`))
	}))
	defer assetServer.Close()
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer update": {"asset.rating.update"},
		"Bearer reader": {"asset.rating.read"},
	})
	defer authServer.Close()
	router := SetupRouter(&config.Config{SystemURL: authServer.URL}, nil, commonClient.NewAssetClient(assetServer.URL), modulelifecycle.NewStandalone("portal"))
	for _, testCase := range []struct {
		token string
		want  int
	}{{"reader", http.StatusForbidden}, {"update", http.StatusOK}} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/portal/assets/12/my-rating", nil)
		request.Header.Set("Authorization", "Bearer "+testCase.token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != testCase.want {
			t.Fatalf("%s status=%d want=%d body=%s", testCase.token, response.Code, testCase.want, response.Body.String())
		}
		if testCase.want == http.StatusOK && !strings.Contains(response.Body.String(), `"user_id":9`) {
			t.Fatalf("own rating projection missing: %s", response.Body.String())
		}
	}
	if requests != 1 {
		t.Fatalf("Asset requests=%d, want 1", requests)
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
				modulelifecycle.NewStandalone("portal"),
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
