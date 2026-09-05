package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStandardBrowserResourceRequestMatcher(t *testing.T) {
	if !isStandardBrowserResourceRequest(newStandardMatcherContext("/api/v1/standard/documents/12/revisions/34/file")) {
		t.Fatal("document download path was rejected")
	}
	for _, path := range []string{
		"/api/v1/standard/documents",
		"/api/v1/standard/documents/12",
		"/api/v1/standard/documents/12/mappings",
		"/api/v1/standard/documents/12/revisions/34",
		"/api/v1/standard/metrics",
	} {
		if isStandardBrowserResourceRequest(newStandardMatcherContext(path)) {
			t.Errorf("ordinary API path accepted: %s", path)
		}
	}
}

func newStandardMatcherContext(path string) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return context
}
