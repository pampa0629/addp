package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	authmiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/service/internal/repository"
	serviceimpl "github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQueryServiceGetReturnsNotFoundForMissingResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:query-handler-not-found?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS service`,
		`CREATE TABLE service.query_services (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL)`,
		`INSERT INTO service.query_services (id, tenant_id) VALUES (8, 8)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	handler := NewQueryServiceHandler(
		serviceimpl.NewQueryServiceService(repository.NewQueryServiceRepository(db), nil, nil, ""),
		nil,
	)
	for _, id := range []string{"404", "8"} {
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/service/query/"+id, nil)
		context.Params = gin.Params{{Key: "id", Value: id}}
		if err := authmiddleware.SetAuthContextForGin(context, testTenantUserAuthContext(t, 7)); err != nil {
			t.Fatal(err)
		}
		handler.GetService(context)
		if response.Code != http.StatusNotFound {
			t.Fatalf("id %s status = %d, want %d; body=%s", id, response.Code, http.StatusNotFound, response.Body.String())
		}
	}
}
