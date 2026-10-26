package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authmiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/service/internal/repository"
	serviceimpl "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQueryDataRefusalsAreNotCacheable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS service`,
		`CREATE TABLE service.query_services (id INTEGER PRIMARY KEY, tenant_id INTEGER, service_name TEXT, status TEXT, public_access BOOLEAN, protocols BLOB)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO service.query_services VALUES (1,7,'public','active',1,?),(2,7,'private','active',0,?),(3,7,'inactive','inactive',1,?),(4,7,'disabled','active',1,?)`,
		[]byte(`{"rest_api":{"enabled":true,"formats":["json","csv","geojson"]}}`), []byte(`{}`), []byte(`{}`), []byte(`{}`)).Error; err != nil {
		t.Fatal(err)
	}
	handler := NewQueryServiceHandler(serviceimpl.NewQueryServiceService(repository.NewQueryServiceRepository(db), nil, nil, ""), nil)
	for _, item := range []struct {
		name, body string
		status     int
		tenant     uint
	}{
		{"missing", `{}`, http.StatusNotFound, 0},
		{"private", `{}`, http.StatusUnauthorized, 0},
		{"private", `{}`, http.StatusForbidden, 8},
		{"inactive", `{}`, http.StatusServiceUnavailable, 0},
		{"disabled", `{}`, http.StatusNotImplemented, 0},
		{"public", `{`, http.StatusBadRequest, 0},
		{"public", `{"format":"invalid"}`, http.StatusBadRequest, 0},
	} {
		t.Run(item.name+item.body+http.StatusText(item.status), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Params = gin.Params{{Key: "serviceName", Value: item.name}}
			context.Request = httptest.NewRequest(http.MethodPost, "/api/query/"+item.name+"/query", strings.NewReader(item.body))
			if item.tenant != 0 {
				if err := authmiddleware.SetAuthContextForGin(context, testTenantUserAuthContext(t, item.tenant)); err != nil {
					t.Fatal(err)
				}
			}
			handler.QueryData(context)
			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != item.status || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("query refusal may be cached or changed: status=%d want=%d headers=%v", response.StatusCode, item.status, response.Header)
			}
		})
	}
}
