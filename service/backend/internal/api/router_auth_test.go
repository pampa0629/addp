package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOptionalSystemAuthIgnoresQueryToken(t *testing.T) {
	systemCalls := 0
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		systemCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer systemServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/resource", optionalSystemAuth(systemServer.URL), func(c *gin.Context) {
		if header := c.GetHeader("Authorization"); header != "" {
			t.Fatalf("query token was copied into Authorization header: %q", header)
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/resource?token=user-token", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || systemCalls != 0 {
		t.Fatalf("status = %d, system calls = %d", response.Code, systemCalls)
	}
}
