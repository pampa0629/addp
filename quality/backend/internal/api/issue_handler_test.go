package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	commonAuthMiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIssueListRejectsUnsupportedFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newIssueHandlerTestDB(t)
	handler := NewIssueHandler(service.NewIssueService(repository.NewIssueRepository(db)))

	for _, query := range []string{"status=closed", "engine_id=0", "engine_id=-2", "engine_id=not-an-id"} {
		t.Run(query, func(t *testing.T) {
			router := gin.New()
			router.GET("/issues", withIssueHandlerAuth(7, 11), handler.List)
			request := httptest.NewRequest(http.MethodGet, "/issues?"+query, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestIssueListUsesTenantFromAuthContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newIssueHandlerTestDB(t)
	createIssueHandlerIssue(t, db, models.Issue{TenantID: 7, RuleApplicationID: 701, EngineID: 2, Table: "tenant_7"})
	createIssueHandlerIssue(t, db, models.Issue{TenantID: 8, RuleApplicationID: 801, EngineID: 2, Table: "tenant_8"})
	handler := NewIssueHandler(service.NewIssueService(repository.NewIssueRepository(db)))
	router := gin.New()
	router.GET("/issues", withIssueHandlerAuth(7, 11), handler.List)

	request := httptest.NewRequest(http.MethodGet, "/issues?engine_id=2", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var body qualityIssueListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 1 || len(body.Data) != 1 || body.Data[0].TenantID != 7 || body.Data[0].Table != "tenant_7" {
		t.Fatalf("tenant-scoped response = %#v", body)
	}
}

func TestIssueGetAndUpdateRespectTenantAndStateContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newIssueHandlerTestDB(t)
	issue := models.Issue{TenantID: 7, RuleApplicationID: 702, EngineID: 2, Table: "tenant_7"}
	createIssueHandlerIssue(t, db, issue)
	if err := db.First(&issue, "tenant_id = ? AND rule_application_id = ?", 7, 702).Error; err != nil {
		t.Fatalf("load issue: %v", err)
	}
	handler := NewIssueHandler(service.NewIssueService(repository.NewIssueRepository(db)))

	getRouter := gin.New()
	getRouter.GET("/issues/:id", withIssueHandlerAuth(8, 22), handler.Get)
	getRequest := httptest.NewRequest(http.MethodGet, "/issues/"+strconv.FormatInt(issue.ID, 10), nil)
	getResponse := httptest.NewRecorder()
	getRouter.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant get status = %d, want %d, body=%s", getResponse.Code, http.StatusNotFound, getResponse.Body.String())
	}

	updateRouter := gin.New()
	updateRouter.PUT("/issues/:id/status", withIssueHandlerAuth(7, 42), handler.UpdateStatus)
	path := "/issues/" + strconv.FormatInt(issue.ID, 10) + "/status"
	blankRequest := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"status":"resolved","note":"  "}`))
	blankRequest.Header.Set("Content-Type", "application/json")
	blankResponse := httptest.NewRecorder()
	updateRouter.ServeHTTP(blankResponse, blankRequest)
	if blankResponse.Code != http.StatusBadRequest {
		t.Fatalf("blank note status = %d, want %d, body=%s", blankResponse.Code, http.StatusBadRequest, blankResponse.Body.String())
	}

	validRequest := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"status":"resolved","note":"已修复源数据"}`))
	validRequest.Header.Set("Content-Type", "application/json")
	validResponse := httptest.NewRecorder()
	updateRouter.ServeHTTP(validResponse, validRequest)
	if validResponse.Code != http.StatusOK {
		t.Fatalf("valid update status = %d, want %d, body=%s", validResponse.Code, http.StatusOK, validResponse.Body.String())
	}

	conflictRequest := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"status":"ignored","note":"再次处理"}`))
	conflictRequest.Header.Set("Content-Type", "application/json")
	conflictResponse := httptest.NewRecorder()
	updateRouter.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("terminal update status = %d, want %d, body=%s", conflictResponse.Code, http.StatusConflict, conflictResponse.Body.String())
	}
}

func withIssueHandlerAuth(tenantID, userID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantText := strconv.FormatUint(uint64(tenantID), 10)
		userText := strconv.FormatUint(uint64(userID), 10)
		membershipID := "1"
		clientID := "addp-web"
		issuedAt := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
		authContext := commonauth.AuthContext{
			SchemaVersion:  commonauth.AuthContextSchemaVersion,
			Principal:      commonauth.AuthPrincipal{Type: "user", ID: userText},
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

func newIssueHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.issues (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		execution_id TEXT NOT NULL DEFAULT '',
		last_execution_id TEXT NOT NULL DEFAULT '',
		rule_application_id INTEGER NOT NULL,
		rule_type TEXT NOT NULL DEFAULT 'not_null',
		severity TEXT NOT NULL DEFAULT 'error',
		message TEXT NOT NULL DEFAULT '',
		column_name TEXT NOT NULL DEFAULT 'id',
		table_name TEXT NOT NULL,
		schema_name TEXT NOT NULL DEFAULT 'public',
		engine_id INTEGER NOT NULL,
		failed_count INTEGER NOT NULL DEFAULT 1,
		total_count INTEGER NOT NULL DEFAULT 1,
		pass_rate REAL NOT NULL DEFAULT 0,
		detail BLOB,
		status TEXT NOT NULL DEFAULT 'open',
		resolved_at DATETIME,
		resolved_by INTEGER,
		resolution_note TEXT NOT NULL DEFAULT '',
		last_observed_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		UNIQUE (tenant_id, rule_application_id)
	)`).Error; err != nil {
		t.Fatalf("create quality issues table: %v", err)
	}
	return db
}

func createIssueHandlerIssue(t *testing.T, db *gorm.DB, issue models.Issue) {
	t.Helper()
	if issue.ExecutionID == "" {
		issue.ExecutionID = "execution-1"
	}
	if issue.LastExecutionID == "" {
		issue.LastExecutionID = issue.ExecutionID
	}
	if issue.RuleType == "" {
		issue.RuleType = "not_null"
	}
	if issue.Severity == "" {
		issue.Severity = "error"
	}
	if issue.ColumnName == "" {
		issue.ColumnName = "id"
	}
	if issue.SchemaName == "" {
		issue.SchemaName = "public"
	}
	if issue.Status == "" {
		issue.Status = "open"
	}
	if issue.FailedCount == 0 {
		issue.FailedCount = 1
	}
	if issue.TotalCount == 0 {
		issue.TotalCount = 1
	}
	if issue.PassRate == 0 {
		issue.PassRate = 0
	}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatalf("create test issue: %v", err)
	}
}
