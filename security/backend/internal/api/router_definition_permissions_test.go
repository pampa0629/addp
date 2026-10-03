package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/security/internal/authorization"
	"github.com/gin-gonic/gin"
)

func TestProtectionAlgorithmCatalogueHasTrustedDescriptorsAndRequiresPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer reader":    {authorization.PermissionSecurityProtectionBaselineRead},
		"Bearer unrelated": {authorization.PermissionSecurityGradeRead},
	})
	defer authServer.Close()
	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, authServer.URL, modulelifecycle.NewStandalone("security"))
	for _, test := range []struct {
		token  string
		status int
	}{{"", 401}, {"unrelated", 403}, {"reader", 200}} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/security/protection-algorithms", nil)
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != test.status {
			t.Fatalf("catalogue status = %d, want %d", response.Code, test.status)
		}
		if test.status == 200 {
			var catalogue []dataprotection.Algorithm
			if err := json.Unmarshal(response.Body.Bytes(), &catalogue); err != nil {
				t.Fatal(err)
			}
			if len(catalogue) != 3 || catalogue[2].Key != dataprotection.AlgorithmSM3V1 || len(catalogue[2].Parameters) != 0 {
				t.Fatal("catalogue descriptor contract changed")
			}
		}
	}
}

func TestDefinitionProfileRoutesRequireBothRelevantPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer unrelated":           {"iam.user.read"},
		"Bearer classification-read": {authorization.PermissionSecurityClassificationRead},
		"Bearer grade-read":          {authorization.PermissionSecurityGradeRead},
		"Bearer both-reads": {
			authorization.PermissionSecurityClassificationRead,
			authorization.PermissionSecurityGradeRead,
		},
		"Bearer both-creates": {
			authorization.PermissionSecurityClassificationCreate,
			authorization.PermissionSecurityGradeCreate,
		},
	})
	defer authServer.Close()

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, authServer.URL, modulelifecycle.NewStandalone("security"))
	for _, test := range []struct {
		name, method, path, token string
		want                      int
	}{
		{"anonymous read", http.MethodGet, "/api/v1/security/definition-profiles", "", http.StatusUnauthorized},
		{"unrelated read", http.MethodGet, "/api/v1/security/definition-profiles", "unrelated", http.StatusForbidden},
		{"classification only", http.MethodGet, "/api/v1/security/definition-profiles", "classification-read", http.StatusForbidden},
		{"grade only", http.MethodGet, "/api/v1/security/definition-profiles", "grade-read", http.StatusForbidden},
		{"both reads", http.MethodGet, "/api/v1/security/definition-profiles", "both-reads", http.StatusOK},
		{"read only cannot apply", http.MethodPost, "/api/v1/security/definition-profile-applications", "both-reads", http.StatusForbidden},
		{"create only cannot read", http.MethodGet, "/api/v1/security/definition-profiles", "both-creates", http.StatusForbidden},
		{"both creates reach input validation", http.MethodPost, "/api/v1/security/definition-profile-applications", "both-creates", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
			if test.name == "both reads" && !strings.Contains(response.Body.String(), `"key":"addp.recommended_data_security/v1"`) {
				t.Fatalf("authorized profile list is missing profile data: %s", response.Body.String())
			}
		})
	}
}
