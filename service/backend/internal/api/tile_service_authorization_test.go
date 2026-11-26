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

func tileProtocolTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { connection.Close() })
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS service`,
		`CREATE TABLE service.tile_services (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, service_name TEXT NOT NULL, title TEXT NOT NULL, protocols JSON, status TEXT, public_access BOOLEAN, created_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE service.tile_service_layers (id INTEGER PRIMARY KEY, service_id INTEGER NOT NULL, layer_name TEXT NOT NULL, title TEXT NOT NULL, layer_type TEXT NOT NULL, layer_config JSON, enabled BOOLEAN, updated_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestTileManagementChecksTenantAndLayerParent(t *testing.T) {
	db := tileProtocolTestDB(t)
	for _, statement := range []string{
		`INSERT INTO service.tile_services (id, tenant_id, service_name, title) VALUES (7, 7, 'ours', 'Ours'), (8, 8, 'theirs', 'Theirs'), (9, 7, 'sibling', 'Sibling')`,
		`INSERT INTO service.tile_service_layers (id, service_id, layer_name, title, layer_type) VALUES (71, 7, 'ours', 'Ours', 'static'), (81, 8, 'theirs', 'Theirs', 'dynamic'), (91, 9, 'sibling', 'Sibling', 'dynamic')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := service.NewTileServiceService(repository.NewTileServiceRepository(db), nil, "")
	handler := NewTileServiceHandler(svc)
	reader := &protocolStaticTileReader{}
	tiles := NewTileEndpointHandler(svc, reader, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if err := authmiddleware.SetAuthContextForGin(c, testTenantUserAuthContext(t, 7)); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	router.GET("/tiles/:serviceName/:layerName/:z/:x/*yformat", tiles.GetXYZTile)
	router.GET("/tile/:id", handler.GetTileService)
	router.PUT("/tile/:id", handler.UpdateTileService)
	router.DELETE("/tile/:id", handler.DeleteTileService)
	router.POST("/tile-layers/:serviceId", handler.AddLayer)
	router.GET("/tile-layers/:serviceId", handler.ListLayers)
	router.GET("/tile-layers/:serviceId/:layerId", handler.GetLayer)
	router.PUT("/tile-layers/:serviceId/:layerId", handler.UpdateLayer)
	router.DELETE("/tile-layers/:serviceId/:layerId", handler.DeleteLayer)

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

	for _, test := range []struct{ method, path, body string }{
		{http.MethodGet, "/tile/8", ""},
		{http.MethodPut, "/tile/8", `{"title":"changed"}`},
		{http.MethodDelete, "/tile/8", ""},
		{http.MethodPost, "/tile-layers/8", `{"layer_name":"new","title":"New","layer_type":"dynamic","layer_config":{"source":{}}}`},
		{http.MethodGet, "/tile-layers/8", ""},
		{http.MethodGet, "/tile-layers/8/81", ""},
		{http.MethodPut, "/tile-layers/8/81", `{"title":"changed"}`},
		{http.MethodDelete, "/tile-layers/8/81", ""},
		{http.MethodGet, "/tile-layers/7/81", ""},
		{http.MethodGet, "/tile-layers/7/91", ""},
		{http.MethodPut, "/tile-layers/7/91", `{"title":"changed"}`},
		{http.MethodDelete, "/tile-layers/7/91", ""},
	} {
		request(test.method, test.path, test.body, http.StatusNotFound)
	}
	request(http.MethodGet, "/tile/7", "", http.StatusOK)
	request(http.MethodPut, "/tile/7", `{"title":"Updated service"}`, http.StatusOK)
	request(http.MethodGet, "/tile-layers/7/71", "", http.StatusOK)
	request(http.MethodPut, "/tile-layers/7/71", `{"title":"Updated"}`, http.StatusOK)
	// Management writes take effect on the next request to the same tile URL.
	const url = "/tiles/ours/ours/3/2/5.mvt"
	request("PUT", "/tile/7", `{"status":"active","protocols":{"xyz":{"enabled":true}}}`, 200)
	request("PUT", "/tile-layers/7/71", `{"enabled":true}`, 200)
	request("GET", url, "", 200)
	request("PUT", "/tile/7", `{"status":"inactive"}`, 200)
	request("GET", url, "", 503)
	request("PUT", "/tile/7", `{"status":"active","protocols":{"xyz":{"enabled":false}}}`, 200)
	request("GET", url, "", 404)
	request("PUT", "/tile/7", `{"protocols":{"xyz":{"enabled":true}}}`, 200)
	request("PUT", "/tile-layers/7/71", `{"enabled":false}`, 200)
	request("GET", url, "", 404)
	request("GET", "/tile-layers/7/71", "", 200)
	request("PUT", "/tile-layers/7/71", `{"enabled":true}`, 200)
	request("GET", url, "", 200)
	if len(reader.reads) != 2 {
		t.Fatalf("storage reads = %d, want 2", len(reader.reads))
	}
	request(http.MethodDelete, "/tile-layers/7/71", "", http.StatusNoContent)
	request(http.MethodGet, "/tile-layers/7/71", "", http.StatusNotFound)
	var foreignTitle string
	if err := db.Table("service.tile_service_layers").Select("title").Where("id = 81").Scan(&foreignTitle).Error; err != nil {
		t.Fatal(err)
	}
	if foreignTitle != "Theirs" {
		t.Fatalf("foreign layer changed to %q", foreignTitle)
	}
	var foreignServiceTitle string
	if err := db.Table("service.tile_services").Select("title").Where("id = 8").Scan(&foreignServiceTitle).Error; err != nil {
		t.Fatal(err)
	}
	if foreignServiceTitle != "Theirs" {
		t.Fatalf("foreign service changed to %q", foreignServiceTitle)
	}
	request(http.MethodDelete, "/tile/7", "", http.StatusNoContent)
	request(http.MethodGet, "/tile/7", "", http.StatusNotFound)
}
