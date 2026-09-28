package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authmiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/service/internal/repository"
	"github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGraphAndRegisteredManagementHideOtherTenants(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:service-management-tenant-api?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection.SetMaxOpenConns(1)
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS service`,
		`CREATE TABLE service.graph_query_services (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, service_name TEXT NOT NULL, title TEXT NOT NULL, config_type TEXT NOT NULL, data_config JSON, updated_at DATETIME)`,
		`CREATE TABLE service.registered_services (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, service_name TEXT NOT NULL, title TEXT NOT NULL, service_type TEXT NOT NULL, endpoint_url TEXT NOT NULL, auth_config JSON, metadata JSON, updated_at DATETIME)`,
		`CREATE TABLE service.registered_service_layers (id INTEGER PRIMARY KEY, service_id INTEGER NOT NULL)`,
		`INSERT INTO service.graph_query_services (id, tenant_id, service_name, title, config_type) VALUES (7, 7, 'ours_graph', 'Ours', 'shape'), (8, 8, 'their_graph', 'Theirs', 'shape')`,
		`INSERT INTO service.registered_services (id, tenant_id, service_name, title, service_type, endpoint_url) VALUES (7, 7, 'ours_registered', 'Ours', 'rest', 'https://example.com/ours'), (8, 8, 'their_registered', 'Theirs', 'rest', 'https://example.com/theirs')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if err := authmiddleware.SetAuthContextForGin(c, testTenantUserAuthContext(t, 7)); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	graph := NewGraphQueryHandler(service.NewGraphQueryServiceService(repository.NewGraphQueryServiceRepository(db), ""), nil)
	registered := NewRegisteredServiceHandler(service.NewRegisteredServiceService(repository.NewRegisteredServiceRepository(db), ""))
	router.GET("/graph/:id", graph.GetService)
	router.PUT("/graph/:id", graph.UpdateService)
	router.DELETE("/graph/:id", graph.DeleteService)
	router.GET("/registered/:id", registered.GetService)
	router.PUT("/registered/:id", registered.UpdateService)
	router.DELETE("/registered/:id", registered.DeleteService)
	router.POST("/registered/:id/refresh", registered.RefreshMetadata)
	router.POST("/registered/:id/health", registered.HealthCheck)
	request := func(method, path, body string, want int) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s = %d, want %d; body=%s", method, path, response.Code, want, response.Body.String())
		}
	}
	for _, path := range []string{"/graph/8", "/registered/8"} {
		request(http.MethodGet, path, "", http.StatusNotFound)
		request(http.MethodPut, path, `{"title":"changed"}`, http.StatusNotFound)
		request(http.MethodDelete, path, "", http.StatusNotFound)
	}
	request(http.MethodPost, "/registered/8/refresh", `{"force":false}`, http.StatusNotFound)
	request(http.MethodPost, "/registered/8/health", "", http.StatusNotFound)
	request(http.MethodGet, "/graph/7", "", http.StatusOK)
	request(http.MethodGet, "/registered/7", "", http.StatusOK)
	request(http.MethodPut, "/graph/7", `{"title":"Updated"}`, http.StatusOK)
	request(http.MethodPut, "/registered/7", `{"title":"Updated"}`, http.StatusOK)
	for _, table := range []string{"graph_query_services", "registered_services"} {
		var title string
		if err := db.Table("service." + table).Select("title").Where("id = 8").Scan(&title).Error; err != nil {
			t.Fatal(err)
		}
		if title != "Theirs" {
			t.Fatalf("other tenant %s changed to %q", table, title)
		}
	}
}
