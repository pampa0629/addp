package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func TestQueryOutputFieldSizeOmitsUnboundedAndUnsafeLengths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		length    int64
		available bool
		want      int
	}{
		{name: "bounded varchar", length: 50, available: true, want: 50},
		{name: "driver does not provide length", length: 0, available: false, want: 0},
		{name: "postgres text sentinel", length: math.MaxInt64, available: true, want: 0},
		{name: "not exactly representable in JSON clients", length: maxJSONSafeInteger + 1, available: true, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryOutputFieldSize(tt.length, tt.available); got != tt.want {
				t.Fatalf("queryOutputFieldSize(%d, %t) = %d, want %d", tt.length, tt.available, got, tt.want)
			}
		})
	}
}

type recordingServiceTokenProvider struct {
	tenantID atomic.Uint32
}

func (provider *recordingServiceTokenProvider) Token(_ context.Context, tenantID uint) (string, error) {
	provider.tenantID.Store(uint32(tenantID))
	return "addp_at_service_token", nil
}

func TestResourceCapabilityHandlerUsesServiceIdentityForMeta(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_service_token" {
			t.Fatalf("Meta Authorization = %q", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("Meta request sent legacy authentication headers")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "attributes": map[string]any{}})
	}))
	defer metaServer.Close()

	tokenProvider := &recordingServiceTokenProvider{}
	metaClient := commonClient.NewMetaClient(metaServer.URL, tokenProvider)
	handler := NewResourceCapabilityHandler(nil, metaClient)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if err := commonAuth.SetAuthContextForGin(c, testTenantUserAuthContext(t, 7)); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	router.GET("/graphs/node-shapes", handler.GetGraphNodeShapes)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/graphs/node-shapes?engine_id=9", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := tokenProvider.tenantID.Load(); got != 7 {
		t.Fatalf("service token tenant ID = %d, want 7", got)
	}
}

func testTenantUserAuthContext(t *testing.T, tenantID uint) commonauth.AuthContext {
	t.Helper()
	now := time.Now().UTC()
	tenant := strconv.FormatUint(uint64(tenantID), 10)
	membershipID := "1"
	clientID := "addp-web"
	return commonauth.AuthContext{
		SchemaVersion: commonauth.AuthContextSchemaVersion,
		Principal:     commonauth.AuthPrincipal{Type: "user", ID: "3"},
		Context: commonauth.AuthSessionContext{
			Type: "tenant", TenantID: &tenant, TenantMembershipID: &membershipID,
		},
		Authentication: commonauth.AuthenticationFacts{
			Methods: []string{"password"}, AssuranceLevel: "aal1", AuthenticatedAt: now,
		},
		Client: commonauth.ClientConstraints{
			ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted", Scopes: []string{},
		},
		Organization: commonauth.OrganizationContext{
			Departments: []commonauth.DepartmentMembership{}, ProjectGroups: []commonauth.ProjectGroupMembership{},
		},
		Authorization: commonauth.AuthorizationFacts{
			AuthorizationVersion: "1", RoleAssignments: []commonauth.RoleAssignment{},
		},
		Token: commonauth.TokenFacts{
			Type: "first_party_access_token", IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		},
	}
}
