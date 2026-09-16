package api

import (
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuleCRUDVersionAndTenantBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newPlanHandlerTestDB(t)
	h := NewRuleHandler(service.NewRuleService(repository.NewRuleRepository(db), nil))
	r := gin.New()
	r.Use(withIssueHandlerAuth(7, 11))
	r.POST("/rules", h.Create)
	r.GET("/rules/:id", h.Get)
	r.PUT("/rules/:id", h.Update)
	r.DELETE("/rules/:id", h.Delete)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		return response
	}
	first := request(http.MethodPost, "/rules", `{"code":"required","name":"Required","type":"not_null","params":{}}`)
	if first.Code != 201 {
		t.Fatalf("create=%d %s", first.Code, first.Body.String())
	}
	updated := request(http.MethodPut, "/rules/1", `{"code":"required","name":"Renamed","type":"not_null","params":{},"version":1}`)
	if updated.Code != 200 {
		t.Fatalf("update=%d %s", updated.Code, updated.Body.String())
	}
	stale := request(http.MethodPut, "/rules/1", `{"code":"required","name":"Stale","type":"not_null","params":{},"version":1}`)
	if stale.Code != 409 || !strings.Contains(stale.Body.String(), "resource_version_conflict") {
		t.Fatalf("stale=%d %s", stale.Code, stale.Body.String())
	}
	cross := gin.New()
	cross.GET("/rules/:id", withIssueHandlerAuth(8, 12), h.Get)
	response := httptest.NewRecorder()
	cross.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rules/1", nil))
	if response.Code != 404 {
		t.Fatalf("cross tenant=%d %s", response.Code, response.Body.String())
	}
	invalid := request(http.MethodPost, "/rules", `{"code":"bound","name":"Bound","type":"not_null","params":{"table":"t","column":"id"}}`)
	if invalid.Code != 400 {
		t.Fatalf("physical rule accepted: %d", invalid.Code)
	}
	if deleted := request(http.MethodDelete, "/rules/1", `{"version":2}`); deleted.Code != 200 {
		t.Fatalf("delete=%s", deleted.Body.String())
	}
}
func TestPlanCannotWriteEmbeddedRules(t *testing.T) {
	r := gin.New()
	r.POST("/plans", NewPlanHandler(nil).Create)
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/plans", strings.NewReader(`{"code":"x","rules":{"schema_version":"addp.quality.plan-rules/v1","rules":[]}}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(response, req)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "unknown field") {
		t.Fatalf("legacy write accepted=%s", response.Body.String())
	}
}
