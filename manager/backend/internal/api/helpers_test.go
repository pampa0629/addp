package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTenantFilterIDFromContextUsesAuthenticatedTenant(t *testing.T) {
	t.Parallel()

	c := newTenantFilterTestContext("/search?tenant_id=2", 1)

	tenantID := tenantFilterIDFromContext(c)
	if tenantID == nil || *tenantID != 1 {
		t.Fatalf("tenantID = %v, want 1", tenantID)
	}
}

func TestTenantFilterIDFromContextAllowsSuperAdminQueryTenant(t *testing.T) {
	t.Parallel()

	c := newTenantFilterTestContext("/search?tenant_id=2", 0)

	tenantID := tenantFilterIDFromContext(c)
	if tenantID == nil || *tenantID != 2 {
		t.Fatalf("tenantID = %v, want 2", tenantID)
	}
}

func TestTenantFilterIDFromContextReturnsNilForSuperAdminWithoutQueryTenant(t *testing.T) {
	t.Parallel()

	c := newTenantFilterTestContext("/search", 0)

	if tenantID := tenantFilterIDFromContext(c); tenantID != nil {
		t.Fatalf("tenantID = %v, want nil", tenantID)
	}
}

func newTenantFilterTestContext(target string, authTenantID uint) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	c.Set("tenant_id", authTenantID)
	return c
}
