package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonmodels "github.com/addp/common/models"
	servicemodels "github.com/addp/service/internal/models"
	"github.com/gin-gonic/gin"
)

type capturingQueryExecutionAuditWriter struct {
	tenantID uint
	event    *commonmodels.AuditLogCreateRequest
}

func (writer *capturingQueryExecutionAuditWriter) WriteQueryExecutionAudit(
	_ context.Context,
	tenantID uint,
	event *commonmodels.AuditLogCreateRequest,
) error {
	writer.tenantID = tenantID
	writer.event = event
	return nil
}

func TestQueryExecutionAuditContainsFactsWithoutQueryValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/query/outdoor/query", strings.NewReader(`{"filter":{"field":"name","op":"eq","value":"secret-body-value"}}`))
	context.Status(http.StatusOK)

	writer := &capturingQueryExecutionAuditWriter{}
	handler := &QueryServiceHandler{executionAuditWriter: writer}
	handler.writeQueryExecutionAudit(context, &queryExecutionAuditState{
		service: &servicemodels.QueryService{ID: 42, TenantID: 7},
		request: &servicemodels.QueryExecutionRequest{
			Filter: &servicemodels.QueryFilter{Field: "name", Op: "eq", Value: "secret-filter-value"},
			Page:   servicemodels.QueryPageRequest{Limit: 100, Cursor: "secret-cursor"}, Format: "csv",
		},
		intent: "export", result: "succeeded", serviceVersion: "version-1", rowCount: 12, hasMore: false,
	})
	if writer.tenantID != 7 || writer.event == nil {
		t.Fatalf("audit write = tenant %d event %#v", writer.tenantID, writer.event)
	}
	if writer.event.EventName != "service.query.exported" || writer.event.Result != "succeeded" || writer.event.RequestID == nil {
		t.Fatalf("unexpected audit identity: %#v", writer.event)
	}
	encoded, err := json.Marshal(writer.event)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(encoded)
	for _, secret := range []string{"secret-body-value", "secret-filter-value", "secret-cursor"} {
		if strings.Contains(payload, secret) {
			t.Fatalf("audit event leaked %q: %s", secret, payload)
		}
	}
	for _, required := range []string{"query_shape_fingerprint", "service_version", "returned_count", "has_more", "source_principal_type"} {
		if !strings.Contains(payload, required) {
			t.Fatalf("audit event missing %q: %s", required, payload)
		}
	}
}
