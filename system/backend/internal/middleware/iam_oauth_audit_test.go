package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type recordingIAMOAuthAuditWriter struct {
	events []iam.AuditEvent
	ctx    context.Context
}

func (writer *recordingIAMOAuthAuditWriter) Write(ctx context.Context, event iam.AuditEvent) error {
	writer.ctx = ctx
	writer.events = append(writer.events, event)
	return nil
}

func TestIAMOAuthFailureAuditMiddlewarePersistsOnlySafeFacts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := &recordingIAMOAuthAuditWriter{}
	auditMiddleware, err := NewIAMOAuthFailureAuditMiddleware(writer)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(auditMiddleware)
	router.POST("/api/v1/system/oauth/token", func(c *gin.Context) {
		SetOAuthSecurityAudit(
			c,
			"oauth.token.failed",
			"denied",
			"addp-cli",
			"refresh_token",
			"",
			"addp.api",
			"invalid_grant",
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/system/oauth/token?code=addp_ac_secret",
		strings.NewReader("client_id=addp-cli&grant_type=refresh_token&scope=addp.api&refresh_token=addp_rt_secret"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || len(writer.events) != 1 {
		t.Fatalf("status = %d events = %#v", response.Code, writer.events)
	}
	event := writer.events[0]
	if event.EventName != "oauth.token.failed" || event.EntityID != event.EventName ||
		event.Result != iam.AuditResultDenied || event.Metadata.HTTPStatus == nil ||
		*event.Metadata.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("event = %#v", event)
	}
	if event.Details["client_id"] != "addp-cli" || event.Details["grant_type"] != "refresh_token" ||
		event.Details["scope"] != "addp.api" || event.Details["error_code"] != "invalid_grant" {
		t.Fatalf("details = %#v", event.Details)
	}
	encoded := strings.Join([]string{
		event.EventName,
		event.EntityID,
		event.Details["client_id"].(string),
		event.Details["grant_type"].(string),
		event.Details["scope"].(string),
		event.Details["error_code"].(string),
	}, " ")
	for _, secret := range []string{"addp_rt_secret", "addp_ac_secret"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("OAuth secret %q entered audit event", secret)
		}
	}
}

func TestIAMOAuthFailureAuditMiddlewareSkipsCommittedTransactionAudit(t *testing.T) {
	writer := &recordingIAMOAuthAuditWriter{}
	auditMiddleware, err := NewIAMOAuthFailureAuditMiddleware(writer)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(auditMiddleware)
	router.POST("/api/v1/system/oauth/token", func(c *gin.Context) {
		MarkOAuthSecurityAuditPersisted(c)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/token", nil))
	if len(writer.events) != 0 {
		t.Fatalf("duplicate OAuth audit events = %#v", writer.events)
	}
}

func TestIAMOAuthFailureAuditMiddlewareRejectsNilWriter(t *testing.T) {
	if _, err := NewIAMOAuthFailureAuditMiddleware(nil); err == nil {
		t.Fatal("NewIAMOAuthFailureAuditMiddleware(nil) succeeded")
	}
}
