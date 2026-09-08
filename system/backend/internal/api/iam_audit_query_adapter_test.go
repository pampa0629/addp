package api

import (
	"net/http/httptest"
	"testing"

	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

func TestAuditQueryFromRequestIncludesEntityFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(
		"GET",
		"/platform/audit/events?entity_type=cleanup_task&entity_id=cleanup-1&principal_type=user",
		nil,
	)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	query, err := auditQueryFromRequest(context, false)
	if err != nil {
		t.Fatalf("parse audit query: %v", err)
	}
	if query.EntityType != "cleanup_task" || query.EntityID != "cleanup-1" {
		t.Fatalf("audit entity filters = %q/%q", query.EntityType, query.EntityID)
	}
	if query.PrincipalType == nil || *query.PrincipalType != iam.PrincipalTypeUser {
		t.Fatalf("audit principal type = %#v", query.PrincipalType)
	}
}
