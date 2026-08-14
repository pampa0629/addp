package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	commonAuthMiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListElementsFiltersByCanonicalIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newElementHandlerTestDB(t)
	for _, element := range []models.Element{
		{ID: 11, TenantID: 7, Name: "Customer ID", Code: "customer_id", DataType: "string", CreatedBy: 1},
		{ID: 12, TenantID: 7, Name: "Order ID", Code: "order_id", DataType: "string", CreatedBy: 1},
		{ID: 13, TenantID: 8, Name: "Other tenant", Code: "other", DataType: "string", CreatedBy: 1},
	} {
		if err := db.Create(&element).Error; err != nil {
			t.Fatalf("create element: %v", err)
		}
	}

	handler := NewElementHandler(service.NewElementService(repository.NewElementRepository(db), nil, nil))
	router := gin.New()
	router.GET("/elements", withElementHandlerAuth(7), handler.ListElements)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/elements?ids=12,11,12&page_size=100", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"total":2`) || strings.Contains(response.Body.String(), "Other tenant") {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func withElementHandlerAuth(tenantID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantText := strconv.FormatUint(uint64(tenantID), 10)
		principalID := "1"
		membershipID := "1"
		clientID := "addp-web"
		issuedAt := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
		authContext := commonauth.AuthContext{
			SchemaVersion:  commonauth.AuthContextSchemaVersion,
			Principal:      commonauth.AuthPrincipal{Type: "user", ID: principalID},
			Context:        commonauth.AuthSessionContext{Type: "tenant", TenantID: &tenantText, TenantMembershipID: &membershipID},
			Authentication: commonauth.AuthenticationFacts{Methods: []string{"password"}, AssuranceLevel: "aal1", AuthenticatedAt: issuedAt},
			Client:         commonauth.ClientConstraints{ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted", Scopes: []string{}},
			Organization:   commonauth.OrganizationContext{Departments: []commonauth.DepartmentMembership{}, ProjectGroups: []commonauth.ProjectGroupMembership{}},
			Authorization:  commonauth.AuthorizationFacts{AuthorizationVersion: "1", RoleAssignments: []commonauth.RoleAssignment{}},
			Token:          commonauth.TokenFacts{Type: "first_party_access_token", IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour)},
		}
		if err := commonAuthMiddleware.SetAuthContextForGin(c, authContext); err != nil {
			panic(err)
		}
		c.Next()
	}
}

func TestListElementsRejectsInvalidIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewElementHandler(nil)
	for _, query := range []string{"ids=1,,2", "ids=0", "ids=abc", "ids=1%2C%202", "ids=1&ids=2"} {
		router := gin.New()
		router.GET("/elements", handler.ListElements)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/elements?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want %d", query, response.Code, http.StatusBadRequest)
		}
	}
}

func newElementHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE standard.elements (
		id INTEGER PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		domain_id INTEGER,
		name TEXT NOT NULL,
		code TEXT NOT NULL,
		data_type TEXT NOT NULL,
		length INTEGER,
		precision_num INTEGER,
		scale INTEGER,
		nullable BOOLEAN,
		default_value TEXT,
		format TEXT,
		value_range BLOB,
		unit_id INTEGER,
		security_level TEXT,
		classification_id INTEGER,
		code_set_id INTEGER,
		definition TEXT,
		example_values BLOB,
		quality_rules BLOB,
		status TEXT,
		steward_id INTEGER,
		tags BLOB,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		version INTEGER NOT NULL DEFAULT 1,
		lifecycle_state TEXT NOT NULL DEFAULT 'active'
	)`).Error; err != nil {
		t.Fatalf("create elements: %v", err)
	}
	return db
}
