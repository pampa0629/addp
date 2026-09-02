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
	for _, fixture := range []struct {
		element  models.Element
		revision models.ElementRevision
	}{
		{models.Element{ID: 11, TenantID: 7, Code: "customer_id", CreatedBy: 1, LifecycleState: "active"}, models.ElementRevision{Name: "Customer ID", Definition: "Customer identifier", DataType: "string", Status: models.RevisionStatusDraft, RevisionNo: 1, ValueDomainKind: models.ValueDomainUnrestricted, ChangeSummary: "initial", CreatedBy: 1}},
		{models.Element{ID: 12, TenantID: 7, Code: "order_id", CreatedBy: 1, LifecycleState: "active"}, models.ElementRevision{Name: "Order ID", Definition: "Order identifier", DataType: "string", Status: models.RevisionStatusDraft, RevisionNo: 1, ValueDomainKind: models.ValueDomainUnrestricted, ChangeSummary: "initial", CreatedBy: 1}},
		{models.Element{ID: 13, TenantID: 8, Code: "other", CreatedBy: 1, LifecycleState: "active"}, models.ElementRevision{Name: "Other tenant", Definition: "Other", DataType: "string", Status: models.RevisionStatusDraft, RevisionNo: 1, ValueDomainKind: models.ValueDomainUnrestricted, ChangeSummary: "initial", CreatedBy: 1}},
	} {
		if err := db.Create(&fixture.element).Error; err != nil {
			t.Fatalf("create element: %v", err)
		}
		fixture.revision.ElementID = fixture.element.ID
		if err := db.Create(&fixture.revision).Error; err != nil {
			t.Fatalf("create revision: %v", err)
		}
		if err := db.Model(&fixture.element).Update("draft_revision_id", fixture.revision.ID).Error; err != nil {
			t.Fatalf("link revision: %v", err)
		}
	}

	handler := NewElementHandler(service.NewElementService(repository.NewElementRepository(db), nil, nil, nil))
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

func TestListElementsRejectsInvalidAsOf(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"as_of=not-a-time", "as_of=2026-08-28T00%3A00%3A00Z&as_of=2026-08-29T00%3A00%3A00Z"} {
		router := gin.New()
		router.GET("/elements", NewElementHandler(nil).ListElements)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/elements?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want %d", query, response.Code, http.StatusBadRequest)
		}
	}
}

func TestListElementsRejectsInvalidRevisionStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"status=superseded", "status=draft&status=published"} {
		router := gin.New()
		router.GET("/elements", NewElementHandler(nil).ListElements)
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
		code TEXT NOT NULL,
		steward_id INTEGER,
		tags BLOB,
		draft_revision_id INTEGER,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		version INTEGER NOT NULL DEFAULT 1,
		lifecycle_state TEXT NOT NULL DEFAULT 'active'
	)`).Error; err != nil {
		t.Fatalf("create elements: %v", err)
	}
	if err := db.Exec(`CREATE TABLE standard.element_revisions (
		id INTEGER PRIMARY KEY AUTOINCREMENT, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL,
		status TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, data_type TEXT NOT NULL,
		length INTEGER, precision_num INTEGER, scale INTEGER, nullable BOOLEAN, default_value TEXT, format TEXT,
		value_domain_kind TEXT NOT NULL, range_constraint TEXT, code_set_revision_id INTEGER, unit_id INTEGER,
		example_values TEXT, extra_quality_rules TEXT,
		compiled_quality_rules TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME,
		submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME,
		created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create element revisions: %v", err)
	}
	return db
}
