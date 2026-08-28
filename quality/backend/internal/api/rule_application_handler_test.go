package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRuleApplicationListUsesTenantFiltersAndNormalizesPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newRuleApplicationHandlerTestDB(t)
	createRuleApplicationHandlerApplication(t, db, models.RuleApplication{TenantID: 7, ElementID: 11, EngineID: 2, SchemaName: "public", Table: "orders", ColumnName: "id", CreatedBy: 1})
	createRuleApplicationHandlerApplication(t, db, models.RuleApplication{TenantID: 7, ElementID: 12, EngineID: 2, SchemaName: "public", Table: "customers", ColumnName: "id", CreatedBy: 1})
	createRuleApplicationHandlerApplication(t, db, models.RuleApplication{TenantID: 8, ElementID: 13, EngineID: 2, SchemaName: "public", Table: "orders", ColumnName: "id", CreatedBy: 1})
	standardServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/elements" || r.URL.Query().Get("ids") != "11" {
			t.Fatalf("unexpected Standard request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":11,"code":"order_id","current_revision":{"id":1101,"revision_no":2,"status":"published","name":"Order ID","data_type":"string"}}],"total":1,"page":1,"page_size":100,"total_pages":1}`))
	}))
	defer standardServer.Close()
	standardClient := commonClient.NewStandardClient(standardServer.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token", nil
	}), standardServer.Client())
	handler := NewRuleApplicationHandler(service.NewRuleEngineService(standardClient, nil, repository.NewRuleApplicationRepository(db)))
	router := gin.New()
	router.GET("/rule-applications", withIssueHandlerAuth(7, 11), handler.List)

	request := httptest.NewRequest(http.MethodGet, "/rule-applications?engine_id=2&schema_name=public&table_name=orders&page=0&page_size=999", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var body qualityRuleApplicationListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 1 || len(body.Data) != 1 || body.Data[0].TenantID != 7 || body.Data[0].Table != "orders" {
		t.Fatalf("filtered response = %#v", body)
	}
	if body.Data[0].Element.ID != 11 || body.Data[0].Element.Name != "Order ID" || body.Data[0].Element.Code != "order_id" {
		t.Fatalf("element projection = %#v", body.Data[0].Element)
	}
	if body.Page != 1 || body.PageSize != 100 || body.TotalPages != 1 {
		t.Fatalf("normalized pagination = %#v", body)
	}
}

func TestRuleApplicationElementCandidatesUseTenantServiceProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	standardServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/elements" || r.URL.Query().Get("keyword") != "gender" {
			t.Fatalf("unexpected Standard request: %s", r.URL.String())
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("page_size") != "100" {
			t.Fatalf("unexpected pagination: %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer tenant-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":12,"code":"gender","current_revision":{"id":1201,"revision_no":3,"status":"published","name":"Gender","data_type":"string","compiled_quality_rules":{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","enabled":true,"severity":"error","message":"required","params":{}}]}}}],"total":1,"page":1,"page_size":100,"total_pages":1}`))
	}))
	defer standardServer.Close()
	standardClient := commonClient.NewStandardClient(standardServer.URL, commonClient.ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		if tenantID != 7 {
			t.Fatalf("tenant ID = %d, want 7", tenantID)
		}
		return "tenant-token", nil
	}), standardServer.Client())
	handler := NewRuleApplicationHandler(service.NewRuleEngineService(standardClient, nil, nil))
	router := gin.New()
	router.GET("/rule-applications/element-candidates", withIssueHandlerAuth(7, 11), handler.ListElementCandidates)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rule-applications/element-candidates?keyword=gender&page=0&page_size=999", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var body qualityElementCandidateListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 1 || body.Page != 1 || body.PageSize != 100 || len(body.Data) != 1 || body.Data[0].ID != 12 || len(body.Data[0].QualityRules.EnabledRules()) != 1 {
		t.Fatalf("candidate response = %#v", body)
	}

	emptyResponse := httptest.NewRecorder()
	router.ServeHTTP(emptyResponse, httptest.NewRequest(http.MethodGet, "/rule-applications/element-candidates", nil))
	if emptyResponse.Code != http.StatusBadRequest {
		t.Fatalf("empty keyword status = %d, want %d, body=%s", emptyResponse.Code, http.StatusBadRequest, emptyResponse.Body.String())
	}
}

func TestRuleApplicationGetRejectsInvalidIDAndEnforcesTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newRuleApplicationHandlerTestDB(t)
	application := createRuleApplicationHandlerApplication(t, db, models.RuleApplication{TenantID: 7, ElementID: 21, EngineID: 2, SchemaName: "public", Table: "orders", ColumnName: "id", CreatedBy: 1})
	handler := NewRuleApplicationHandler(service.NewRuleEngineService(nil, nil, repository.NewRuleApplicationRepository(db)))

	invalidRouter := gin.New()
	invalidRouter.GET("/rule-applications/:id", withIssueHandlerAuth(7, 11), handler.Get)
	invalidResponse := httptest.NewRecorder()
	invalidRouter.ServeHTTP(invalidResponse, httptest.NewRequest(http.MethodGet, "/rule-applications/not-an-id", nil))
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d, want %d, body=%s", invalidResponse.Code, http.StatusBadRequest, invalidResponse.Body.String())
	}

	crossTenantResponse := httptest.NewRecorder()
	crossTenantRouter := gin.New()
	crossTenantRouter.GET("/rule-applications/:id", withIssueHandlerAuth(8, 22), handler.Get)
	crossTenantRouter.ServeHTTP(crossTenantResponse, httptest.NewRequest(http.MethodGet, "/rule-applications/"+strconv.FormatInt(application.ID, 10), nil))
	if crossTenantResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant status = %d, want %d, body=%s", crossTenantResponse.Code, http.StatusNotFound, crossTenantResponse.Body.String())
	}
}

func TestRuleApplicationMutationsRejectUnknownFieldsAndInvalidIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	unknownBody := strings.NewReader(`{"element_id":1,"engine_id":2,"schema_name":"public","table_name":"orders","column_name":"id","enabled":true}`)
	createRouter := gin.New()
	createRouter.POST("/rule-applications", NewRuleApplicationHandler(nil).Create)
	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/rule-applications", unknownBody)
	createRequest.Header.Set("Content-Type", "application/json")
	createRouter.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusBadRequest || !strings.Contains(createResponse.Body.String(), "unknown field") {
		t.Fatalf("create unknown field status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}

	updateRouter := gin.New()
	updateRouter.PUT("/rule-applications/:id", NewRuleApplicationHandler(nil).Update)
	updateResponse := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/rule-applications/not-an-id", strings.NewReader(`{"enabled":true}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRouter.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusBadRequest {
		t.Fatalf("update invalid id status = %d, want %d, body=%s", updateResponse.Code, http.StatusBadRequest, updateResponse.Body.String())
	}

	deleteRouter := gin.New()
	deleteRouter.DELETE("/rule-applications/:id", NewRuleApplicationHandler(nil).Delete)
	deleteResponse := httptest.NewRecorder()
	deleteRouter.ServeHTTP(deleteResponse, httptest.NewRequest(http.MethodDelete, "/rule-applications/not-an-id", nil))
	if deleteResponse.Code != http.StatusBadRequest {
		t.Fatalf("delete invalid id status = %d, want %d, body=%s", deleteResponse.Code, http.StatusBadRequest, deleteResponse.Body.String())
	}
}

func TestRuleApplicationUpdateRequiresExplicitEnabledAndPersistsAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newRuleApplicationHandlerTestDB(t)
	application := createRuleApplicationHandlerApplication(t, db, models.RuleApplication{TenantID: 7, ElementID: 31, EngineID: 2, SchemaName: "public", Table: "orders", ColumnName: "id", Enabled: true, CreatedBy: 1})
	handler := NewRuleApplicationHandler(service.NewRuleEngineService(nil, nil, repository.NewRuleApplicationRepository(db)))
	router := gin.New()
	router.PUT("/rule-applications/:id", withIssueHandlerAuth(7, 19), handler.Update)

	missingResponse := httptest.NewRecorder()
	missingRequest := httptest.NewRequest(http.MethodPut, "/rule-applications/"+strconv.FormatInt(application.ID, 10), strings.NewReader(`{}`))
	missingRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled status = %d, want %d, body=%s", missingResponse.Code, http.StatusBadRequest, missingResponse.Body.String())
	}

	updateResponse := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/rule-applications/"+strconv.FormatInt(application.ID, 10), strings.NewReader(`{"enabled":false}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d, body=%s", updateResponse.Code, http.StatusOK, updateResponse.Body.String())
	}
	var updated models.RuleApplication
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.Enabled || updated.UpdatedBy == nil || *updated.UpdatedBy != 19 || updated.UpdatedAt.IsZero() {
		t.Fatalf("updated application = %#v", updated)
	}
}

func newRuleApplicationHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.rule_applications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		element_id INTEGER NOT NULL,
		element_revision_id INTEGER NOT NULL,
		engine_id INTEGER NOT NULL,
		schema_name TEXT NOT NULL,
		table_name TEXT NOT NULL,
		column_name TEXT NOT NULL,
		rule_config BLOB NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create rule applications table: %v", err)
	}
	return db
}

func createRuleApplicationHandlerApplication(t *testing.T, db *gorm.DB, application models.RuleApplication) models.RuleApplication {
	t.Helper()
	if application.ElementRevisionID == 0 {
		application.ElementRevisionID = application.ElementID*100 + 1
	}
	if len(application.RuleConfig) == 0 {
		application.RuleConfig = []byte(`{"schema_version":"addp.quality.rules/v1","rules":[]}`)
	}
	if err := db.Create(&application).Error; err != nil {
		t.Fatalf("create rule application: %v", err)
	}
	return application
}
