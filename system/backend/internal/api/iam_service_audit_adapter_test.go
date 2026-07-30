package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type capturingServiceAuditWriter struct {
	event iam.AuditEvent
}

func (writer *capturingServiceAuditWriter) Write(_ context.Context, event iam.AuditEvent) error {
	writer.event = event
	return nil
}

func TestIAMServiceAuditOverridesCallerIdentityAndTenant(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	writer := &capturingServiceAuditWriter{}
	handler, err := NewIAMInternalAuditHandler(writer)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, testIAMServiceActorContext("tenant", "addp-meta")); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	router.POST("/audit", handler.CreateService)

	request := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewBufferString(`{
		"principal_id":"999","principal_type":"user","context_type":"platform","tenant_id":"999",
		"event_name":"meta.scan.completed","result":"succeeded","risk_level":"low",
		"module_name":"meta","entity_type":"scan","entity_id":"scan-1","details":{}
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if writer.event.Metadata.PrincipalID == nil || *writer.event.Metadata.PrincipalID != 41 ||
		writer.event.Metadata.PrincipalType == nil || *writer.event.Metadata.PrincipalType != iam.PrincipalTypeServicePrincipal ||
		writer.event.Metadata.ContextType == nil || *writer.event.Metadata.ContextType != iam.ContextTypeTenant ||
		writer.event.Metadata.TenantID == nil || *writer.event.Metadata.TenantID != 3 {
		t.Fatalf("audit identity metadata = %#v", writer.event.Metadata)
	}
}
