package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTenantFilterIDFromContextUsesAuthenticatedTenant(t *testing.T) {
	t.Parallel()

	c := newTenantFilterTestContext("/search?tenant_id=2")
	setTenantAuthContextForTest(c, 1, 8)

	tenantID := tenantFilterIDFromContext(c)
	if tenantID == nil || *tenantID != 1 {
		t.Fatalf("tenantID = %v, want 1", tenantID)
	}
}

func TestTenantFilterIDFromContextDoesNotAcceptQueryTenantWithoutAuthContext(t *testing.T) {
	t.Parallel()

	c := newTenantFilterTestContext("/search?tenant_id=2")

	if tenantID := tenantFilterIDFromContext(c); tenantID != nil {
		t.Fatalf("tenantID = %v, want nil", tenantID)
	}
}

func newTenantFilterTestContext(target string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c
}
