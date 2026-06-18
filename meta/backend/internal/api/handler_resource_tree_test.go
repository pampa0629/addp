package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	commonExecution "github.com/addp/common/execution"
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
	engine := commonModels.Engine{
		ID:         9,
		TenantID:   &tenantID,
		Name:       "Business MinIO",
		EngineType: "s3",
		IsActive:   true,
	}
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(engine); err != nil {
			t.Fatalf("encode engine: %v", err)
		}
	}))

	db := metatest.OpenMetadataDB(t)
	engineSvc := service.NewEngineService(db, systemServer.URL, "secret")
	metadataSvc := service.NewMetadataQueryService(db)
	handler := NewHandler(engineSvc, nil, nil, nil, metadataSvc)

	router := gin.New()
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
	createResourceTreeHandlerTaskExecutionTable(t, db)
	engine := commonModels.Engine{
		ID:         engineID,
		TenantID:   &tenantID,
		Name:       "Business MinIO",
		EngineType: "s3",
		IsActive:   true,
	}
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(engine); err != nil {
			t.Fatalf("encode engine: %v", err)
		}
	}))
	engineSvc := service.NewEngineService(db, systemServer.URL, "")
	root := createResourceTreeHandlerNode(t, db, models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: "service", Name: "Business MinIO", FullName: "", Depth: 0})
	createResourceTreeHandlerNode(t, db, models.MetaNode{TenantID: tenantID, EngineID: engineID, ParentNodeID: &root.ID, NodeType: "bucket", Name: "manager", FullName: "manager", Depth: 1})

	scanSvc := service.NewScanService(db, engineSvc)
	executionSvc := service.NewScanExecutionService(db, scanSvc, engineSvc, nil)
	handler := NewHandler(engineSvc, scanSvc, nil, executionSvc, service.NewMetadataQueryService(db))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(commonAuth.ContextTenantIDKey, tenantID)
		c.Set(commonAuth.ContextUserIDKey, uint(3))
		c.Next()
	})
	router.POST("/resource-tree/:engine_id/refresh", handler.RefreshResourceTreeNode)
	return router, systemServer.Close
}

func createResourceTreeHandlerNode(t *testing.T, db *gorm.DB, node models.MetaNode) models.MetaNode {
	t.Helper()
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	return node
}

func createResourceTreeHandlerTaskExecutionTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			execution_id TEXT NOT NULL UNIQUE,
			module TEXT NOT NULL,
			task_type TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			source_task_id TEXT,
			source_task_name TEXT,
			parent_execution_id TEXT,
			status TEXT NOT NULL,
			progress INTEGER DEFAULT 0,
			current_step TEXT,
			trigger_type TEXT NOT NULL,
			triggered_by INTEGER,
			execution_config JSON,
			error_details JSON,
			metadata JSON,
			execution_time_ms INTEGER,
			rows_affected INTEGER,
			records_read INTEGER,
			records_written INTEGER,
			bytes_read INTEGER,
			bytes_written INTEGER,
			started_at DATETIME,
			completed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			execution_id TEXT NOT NULL UNIQUE,
			module TEXT NOT NULL,
			task_type TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			source_task_id TEXT,
			source_task_name TEXT,
			parent_execution_id TEXT,
			status TEXT NOT NULL,
			progress INTEGER DEFAULT 0,
			current_step TEXT,
			trigger_type TEXT NOT NULL,
			triggered_by INTEGER,
			execution_config JSON,
			error_details JSON,
			metadata JSON,
			execution_time_ms INTEGER,
			rows_affected INTEGER,
			records_read INTEGER,
			records_written INTEGER,
			bytes_read INTEGER,
			bytes_written INTEGER,
			started_at DATETIME,
			completed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create unqualified task_executions table: %v", err)
	}
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
