package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type requestAuditWriterStub struct {
	events []iam.AuditEvent
}

func (w *requestAuditWriterStub) Write(_ context.Context, event iam.AuditEvent) error {
	w.events = append(w.events, event)
	return nil
}

func TestLoggerMiddlewarePersistsOnlyStructuredOAuthSecurityAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := &requestAuditWriterStub{}
	router := gin.New()
	router.Use(LoggerMiddleware(writer))
	router.POST("/api/v1/system/oauth/token", func(c *gin.Context) {
		SetOAuthSecurityAudit(c, "oauth.token.failed", "failed", "addp-cli", "refresh_token", "", "addp.api", "invalid_grant")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
	})
	form := url.Values{
		"grant_type": {"refresh_token"}, "client_id": {"addp-cli"},
		"refresh_token": {"addp_rt_secret"}, "code_verifier": {"pkce-secret"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/token?code=addp_ac_secret", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(httptest.NewRecorder(), request)

	if len(writer.events) != 1 {
		t.Fatalf("audit event count = %d, want 1", len(writer.events))
	}
	event := writer.events[0]
	if event.EventName != "oauth.token.failed" || event.EntityType != "oauth_security_event" ||
		event.Details["client_id"] != "addp-cli" || event.Details["error_code"] != "invalid_grant" {
		t.Fatalf("OAuth audit event = %#v", event)
	}
	encoded := strings.Join([]string{event.EntityID, event.EventName}, " ")
	for key, value := range event.Details {
		encoded += key + "=" + value.(string)
	}
	for _, secret := range []string{"addp_rt_secret", "addp_ac_secret", "pkce-secret"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("OAuth secret %q persisted in %#v", secret, event)
		}
	}
}

func TestLoggerMiddlewareUsesStableOAuthFailureEventBeforeHandler(t *testing.T) {
	writer := &requestAuditWriterStub{}
	router := gin.New()
	router.Use(LoggerMiddleware(writer))
	router.POST("/api/v1/system/oauth/authorizations", func(c *gin.Context) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication_required"})
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(
		http.MethodPost, "/api/v1/system/oauth/authorizations", strings.NewReader(`{"request_id":"secret"}`),
	))
	if len(writer.events) != 1 || writer.events[0].EventName != "oauth.authorization.failed" ||
		writer.events[0].Details["error_code"] != "authentication_required" {
		t.Fatalf("OAuth failure audit = %#v", writer.events)
	}
}

func TestLoggerMiddlewareSkipsOAuthAlreadyPersistedByTargetIAM(t *testing.T) {
	writer := &requestAuditWriterStub{}
	router := gin.New()
	router.Use(LoggerMiddleware(writer))
	router.POST("/api/v1/system/oauth/token", func(c *gin.Context) {
		MarkOAuthSecurityAuditPersisted(c)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/system/oauth/token", nil))
	if len(writer.events) != 0 {
		t.Fatalf("duplicate OAuth audit events = %#v", writer.events)
	}
}

func TestLoggerMiddlewareWritesGenericRequestWithoutBodyOrQuery(t *testing.T) {
	writer := &requestAuditWriterStub{}
	router := gin.New()
	router.Use(LoggerMiddleware(writer))
	router.POST("/api/v1/system/example", func(c *gin.Context) { c.Status(http.StatusCreated) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(
		http.MethodPost, "/api/v1/system/example?password=secret", strings.NewReader(`{"token":"secret"}`),
	))
	if len(writer.events) != 1 || writer.events[0].EventName != "http.request.completed" ||
		writer.events[0].Result != iam.AuditResultSucceeded || len(writer.events[0].Details) != 1 {
		t.Fatalf("generic audit event = %#v", writer.events)
	}
}
