package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonAuth "github.com/addp/common/middleware/auth"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestResourceTreeNodeHandlerMapsInvalidLocatorToBadRequest(t *testing.T) {
	router, cleanup := newResourceTreeHandlerTestRouter(t)
	defer cleanup()

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource-tree/9/node?locator=not-a-locator", nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

func TestResourceTreeAncestorsHandlerMapsMissingTargetToNotFound(t *testing.T) {
	router, cleanup := newResourceTreeHandlerTestRouter(t)
	defer cleanup()

	resp := httptest.NewRecorder()
	locator := url.QueryEscape("addp://engine/9/path/missing?type=bucket&node_id=23")
	req := httptest.NewRequest(http.MethodGet, "/resource-tree/9/ancestors?locator="+locator, nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

func TestResourceTreeSearchHandlerMapsShortKeywordToBadRequest(t *testing.T) {
	router, cleanup := newResourceTreeHandlerTestRouter(t)
	defer cleanup()

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource-tree/9/search?q=r", nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

func TestResourceTreeRefreshHandlerRequiresExecutionService(t *testing.T) {
	router, cleanup := newResourceTreeHandlerTestRouter(t)
	defer cleanup()

	resp := httptest.NewRecorder()
	locator := url.QueryEscape("addp://engine/9/path/manager?type=bucket&node_id=1")
	req := httptest.NewRequest(http.MethodPost, "/resource-tree/9/refresh?locator="+locator, nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusServiceUnavailable, resp.Body.String())
	}
}

func TestResourceTreeRefreshHandlerMapsMissingLocatorIdentityToBadRequest(t *testing.T) {
	router, cleanup := newResourceTreeRefreshHandlerTestRouter(t)
	defer cleanup()

	resp := httptest.NewRecorder()
	locator := url.QueryEscape("addp://engine/9/path/manager?type=bucket")
	req := httptest.NewRequest(http.MethodPost, "/resource-tree/9/refresh?locator="+locator, nil)
	req.Header.Set("Authorization", "Bearer user-token")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

func TestResourceTreeRefreshHandlerSubmitsNodeScanRun(t *testing.T) {
	router, cleanup := newResourceTreeRefreshHandlerTestRouter(t)
	defer cleanup()

	resp := httptest.NewRecorder()
	locator := url.QueryEscape("addp://engine/9/path/manager?type=bucket&node_id=2")
	req := httptest.NewRequest(http.MethodPost, "/resource-tree/9/refresh?locator="+locator, nil)
	req.Header.Set("Authorization", "Bearer user-token")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	var body struct {
		Data models.ResourceTreeRefreshResponse `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Locator != "addp://engine/9/path/manager?type=bucket&node_id=2" {
		t.Fatalf("locator = %q", body.Data.Locator)
	}
	if body.Data.Run == nil || body.Data.Run.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("run = %#v, want pending run", body.Data.Run)
	}
	if got := jsonMapStringSliceForAPITest(body.Data.Run.ExecutionConfig, "catalog_paths"); len(got) != 1 || got[0] != "manager" {
		t.Fatalf("run catalog_paths = %#v, want [manager]", got)
	}
	if body.Data.Run.Source != commonExecution.ModuleMeta {
		t.Fatalf("run source = %q, want meta", body.Data.Run.Source)
	}
}

func newResourceTreeHandlerTestRouter(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tenantID := uint(7)
	engine := &commonModels.Engine{
		ID:             9,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "active",
	}
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if r.Form.Get("tenant_id") != "7" {
				t.Errorf("token tenant_id = %q, want 7", r.Form.Get("tenant_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}
		if r.URL.Path != "/api/v1/system/engines/9" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer addp_at_meta" ||
			r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Errorf("unexpected System authentication headers: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(engine); err != nil {
			t.Errorf("encode engine: %v", err)
		}
	}))
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		systemServer.URL, "addp-meta", "meta-resource-tree-test-secret-32-bytes", systemServer.Client(),
	)
	if err != nil {
		t.Fatalf("create Meta service token source: %v", err)
	}
	db := metatest.OpenMetadataDB(t)
	engineSvc := service.NewEngineService(
		db,
		commonClient.NewSystemServiceClient(systemServer.URL, tokenSource, systemServer.Client()),
	)
	metadataSvc := service.NewMetadataQueryService(db)
	handler := NewHandler(engineSvc, nil, nil, nil, metadataSvc, nil)

	router := gin.New()
	installResourceTreeTenantContext(t, router, tenantID)
	router.GET("/resource-tree/:engine_id/node", handler.GetResourceTreeNode)
	router.GET("/resource-tree/:engine_id/ancestors", handler.GetResourceTreeAncestors)
	router.GET("/resource-tree/:engine_id/search", handler.SearchResourceTree)
	router.POST("/resource-tree/:engine_id/refresh", handler.RefreshResourceTreeNode)
	return router, systemServer.Close
}

func newResourceTreeRefreshHandlerTestRouter(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tenantID := uint(7)
	engineID := uint(9)
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	engine := commonModels.Engine{
		ID:             engineID,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "active",
	}
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}
		if r.URL.Path != "/api/v1/system/engines/9" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer addp_at_meta" ||
			r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Errorf("unexpected System authentication headers: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(engine); err != nil {
			t.Fatalf("encode engine: %v", err)
		}
	}))
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		systemServer.URL, "addp-meta", "meta-resource-tree-test-secret-32-bytes", systemServer.Client(),
	)
	if err != nil {
		t.Fatalf("create Meta service token source: %v", err)
	}
	engineSvc := service.NewEngineService(
		db,
		commonClient.NewSystemServiceClient(systemServer.URL, tokenSource, systemServer.Client()),
	)
	root := createResourceTreeHandlerNode(t, db, models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: "service", Name: "Business MinIO", FullName: "", Depth: 0})
	createResourceTreeHandlerNode(t, db, models.MetaNode{TenantID: tenantID, EngineID: engineID, ParentNodeID: &root.ID, NodeType: "bucket", Name: "manager", FullName: "manager", Depth: 1})

	scanSvc := service.NewScanService(db, engineSvc)
	executionSvc := service.NewScanExecutionService(db, scanSvc, engineSvc, nil)
	handler := NewHandler(engineSvc, scanSvc, nil, executionSvc, service.NewMetadataQueryService(db), nil)

	router := gin.New()
	installResourceTreeTenantContext(t, router, tenantID)
	router.POST("/resource-tree/:engine_id/refresh", handler.RefreshResourceTreeNode)
	return router, systemServer.Close
}

func installResourceTreeTenantContext(t *testing.T, router *gin.Engine, tenantID uint) {
	t.Helper()
	router.Use(func(c *gin.Context) {
		now := time.Now().UTC()
		formattedTenantID := strconv.FormatUint(uint64(tenantID), 10)
		membershipID := "1"
		clientID := "addp-web"
		authContext := commonauth.AuthContext{
			SchemaVersion: commonauth.AuthContextSchemaVersion,
			Principal:     commonauth.AuthPrincipal{Type: "user", ID: "3"},
			Context: commonauth.AuthSessionContext{
				Type: "tenant", TenantID: &formattedTenantID, TenantMembershipID: &membershipID,
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
		if err := commonAuth.SetAuthContextForGin(c, authContext); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
}

func createResourceTreeHandlerNode(t *testing.T, db *gorm.DB, node models.MetaNode) models.MetaNode {
	t.Helper()
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	return node
}

func jsonMapStringSliceForAPITest(m commonModels.JSONMap, key string) []string {
	value, ok := m[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
