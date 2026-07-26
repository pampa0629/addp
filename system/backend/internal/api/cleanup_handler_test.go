package api

import (
	"net/http/httptest"
	"testing"

	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func TestCleanupTenantIDRejectsGlobalTenantContext(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	if err := sharedauth.SetAuthContextForGin(ctx, testIAMActorContext("platform")); err != nil {
		t.Fatal(err)
	}

	tenantID, ok := cleanupTenantID(ctx)
	if ok {
		t.Fatal("cleanupTenantID should reject tenant_id=0")
	}
	if tenantID != 0 {
		t.Fatalf("tenantID = %d, want 0", tenantID)
	}
	if recorder.Code != 403 {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestCleanupTenantIDAcceptsTenantContext(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	authContext := testIAMActorContext("tenant")
	*authContext.Context.TenantID = "12"
	if err := sharedauth.SetAuthContextForGin(ctx, authContext); err != nil {
		t.Fatal(err)
	}

	tenantID, ok := cleanupTenantID(ctx)
	if !ok {
		t.Fatal("cleanupTenantID should accept tenant_id=12")
	}
	if tenantID != 12 {
		t.Fatalf("tenantID = %d, want 12", tenantID)
	}
	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestCleanupTenantIDMissingContextReturnsUnauthorized(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	_, ok := cleanupTenantID(ctx)
	if ok {
		t.Fatal("cleanupTenantID should reject missing tenant context")
	}
	if recorder.Code != 401 {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}
