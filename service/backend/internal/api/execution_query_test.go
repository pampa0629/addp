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

func TestExecutionQueryManagementRejectsInvalidAndCrossTenantRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	for _, statement := range []string{`ATTACH DATABASE ':memory:' AS service`, `CREATE TABLE service.query_services (id INTEGER PRIMARY KEY, tenant_id INTEGER, config_type TEXT)`, `INSERT INTO service.query_services VALUES (1,8,'analytical'),(2,7,'sql')`} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	handler := NewQueryServiceHandler(serviceimpl.NewQueryServiceService(repository.NewQueryServiceRepository(db), nil, nil, ""), &serviceimpl.QueryExecutorService{})
	for _, tc := range []struct {
		id, body      string
		authenticated bool
		status        int
	}{
		{"1", `{}`, false, 401}, {"1", `{}`, true, 404}, {"99", `{}`, true, 404}, {"2", `{}`, true, 400},
		{"bad", `{}`, true, 400}, {"0", `{}`, true, 400}, {"1", `{"sql":"SELECT 1"}`, true, 400}, {"1", `{} {}`, true, 400},
	} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/query/"+tc.id+"/execution-query", strings.NewReader(tc.body))
		c.Params = gin.Params{{Key: "id", Value: tc.id}}
		if tc.authenticated {
			if err := authmiddleware.SetAuthContextForGin(c, testTenantUserAuthContext(t, 7)); err != nil {
				t.Fatal(err)
			}
		}
		handler.PreviewExecutionQuery(c)
		if response.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("preview can be cached")
		}
	}
}
