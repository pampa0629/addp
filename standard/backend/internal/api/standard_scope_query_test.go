package api

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseOptionalStandardScope(t *testing.T) {
	for _, value := range []string{"", "platform", "tenant_common", "domain"} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("GET", "/?scope_type="+url.QueryEscape(value), nil)
		if got, err := parseOptionalStandardScope(context); err != nil || got != value {
			t.Fatalf("parseOptionalStandardScope(%q) = %q, %v", value, got, err)
		}
	}
	for _, value := range []string{"global", "DOMAIN", " domain"} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("GET", "/?scope_type="+url.QueryEscape(value), nil)
		if _, err := parseOptionalStandardScope(context); err == nil {
			t.Fatalf("parseOptionalStandardScope(%q) should fail", value)
		}
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/?scope_type=domain&scope_type=tenant_common", nil)
	if _, err := parseOptionalStandardScope(context); err == nil {
		t.Fatal("duplicate scope_type should fail")
	}
}
