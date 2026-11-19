package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSearchHistoryIdentityUsesAuthenticatedTenantAndUser(t *testing.T) {
	t.Parallel()

	c := newSearchHistoryTestContext("/search/history?tenant_id=2&user_id=42")
	setTenantAuthContextForTest(c, 1, 8)

	tenantID, userID := searchHistoryIdentity(c)
	if tenantID != 1 || userID != 8 {
		t.Fatalf("tenant/user = %d/%d, want 1/8", tenantID, userID)
	}
}

func TestSearchHistoryIdentityDoesNotAcceptQueryWithoutAuthContext(t *testing.T) {
	t.Parallel()

	c := newSearchHistoryTestContext("/search/history?tenant_id=2&user_id=42")

	if tenantID, userID := searchHistoryIdentity(c); tenantID != 0 || userID != 0 {
		t.Fatalf("tenant/user = %d/%d, want no identity", tenantID, userID)
	}
}

func newSearchHistoryTestContext(target string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c
}
