package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDomainFilterRejectsMalformedListQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/rules", NewRuleHandler(nil).List)
	router.GET("/plans", NewPlanHandler(nil).List)
	router.GET("/issues", NewIssueHandler(nil).List)
	for _, path := range []string{"/rules", "/plans", "/issues"} {
		for _, query := range []string{"", "-1", "abc", "1.5", "9223372036854775808", "1&owner_domain_id=2"} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?owner_domain_id="+query, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("%s %s: %d", path, query, response.Code)
			}
		}
	}
}

func TestDomainFilterDistinguishesAllPublicAndSpecific(t *testing.T) {
	for _, query := range []string{"", "?owner_domain_id=0", "?owner_domain_id=42"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/"+query, nil)
		owner, err := ownerDomainFilter(c)
		if err != nil {
			t.Fatal(err)
		}
		if query == "" {
			if owner != nil {
				t.Fatal("all filter must be absent")
			}
			continue
		}
		want := int64(0)
		if query == "?owner_domain_id=42" {
			want = 42
		}
		if owner == nil || *owner != want {
			t.Fatalf("query=%s owner=%v", query, owner)
		}
	}
}
