package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
		`CREATE TABLE service.query_services (id INTEGER PRIMARY KEY)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	handler := NewQueryServiceHandler(
		serviceimpl.NewQueryServiceService(repository.NewQueryServiceRepository(db), nil, nil, ""),
		nil,
	)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/service/query/404", nil)
	context.Params = gin.Params{{Key: "id", Value: "404"}}

	handler.GetService(context)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
	}
}
