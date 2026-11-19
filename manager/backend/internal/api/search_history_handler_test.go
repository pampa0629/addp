package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestSearchHistoryRoutesExcludeOtherTenantsAndUsers(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	if err := db.Exec(`CREATE TABLE manager.search_histories (
		id integer PRIMARY KEY, tenant_id integer NOT NULL, user_id integer NOT NULL,
		query text NOT NULL, created_at datetime, updated_at datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO manager.search_histories(id,tenant_id,user_id,query)
		VALUES (1,7,31,'own'), (2,8,31,'other tenant'), (3,7,32,'other user')`).Error; err != nil {
		t.Fatal(err)
	}
	h := NewSearchHandler(nil, service.NewSearchHistoryService(repository.NewSearchHistoryRepository(db)), nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		switch c.GetHeader("Authorization") {
		case "Bearer current":
			setTenantAuthContextForTest(c, 7, 31)
		case "Bearer other-tenant":
			setTenantAuthContextForTest(c, 8, 31)
		case "Bearer other-user":
			setTenantAuthContextForTest(c, 7, 32)
		}
	})
	r.GET("/search/history", h.ListHistory)
	r.DELETE("/search/history/:id", h.DeleteHistoryItem)
	r.DELETE("/search/history", h.ClearHistory)
	request := func(method, path, token string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		if response.Code != status {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, response.Code, status, response.Body)
		}
		return response
	}
	list := func(token string) []string {
		t.Helper()
		response := request(http.MethodGet, "/search/history?tenant_id=8&user_id=32", token, http.StatusOK)
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("personal search history must not be cached")
		}
		var body struct {
			Data struct {
				Items []struct {
					Query string `json:"query"`
				} `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		var queries []string
		for _, item := range body.Data.Items {
			queries = append(queries, item.Query)
		}
		return queries
	}
	requireQuery := func(token, term string) {
		t.Helper()
		if got := list(token); len(got) != 1 || got[0] != term {
			t.Fatalf("history for %s = %v, want [%s]", token, got, term)
		}
	}

	request(http.MethodGet, "/search/history?tenant_id=7&user_id=31", "", http.StatusUnauthorized)
	requireQuery("current", "own")
	request(http.MethodDelete, "/search/history/2?tenant_id=8", "current", http.StatusNoContent)
	request(http.MethodDelete, "/search/history/3?user_id=32", "current", http.StatusNoContent)
	request(http.MethodDelete, "/search/history?tenant_id=8&user_id=32", "current", http.StatusNoContent)
	if got := list("current"); len(got) != 0 {
		t.Fatalf("own history was not cleared: %v", got)
	}
	requireQuery("other-tenant", "other tenant")
	requireQuery("other-user", "other user")
}
