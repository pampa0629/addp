package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestAccessLogOnlyCapturesSafeAPIConsumerRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"username":"operator","password":"body-password","code":"123456","challenge":"body-challenge","access_token":"body-token"}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/system/login?search=visible&password=query-password&code=654321&challenge=query-challenge&access_token=query-token&client_secret=query-secret",
		strings.NewReader(body),
	)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	context.Set("api_consumer_info", &client.APIConsumerCredentialValidationResponse{APIConsumerID: 42})
	context.Set("api_credential_prefix", "addp_api_pre")

	middleware := &AccessLoggerMiddleware{db: &gorm.DB{}}
	entry := middleware.buildAccessLog(context, http.StatusCreated, 17)
	if entry == nil {
		t.Fatal("API consumer request did not produce an access log entry")
	}
	if entry.APIConsumerID == nil || *entry.APIConsumerID != 42 || entry.CredentialPrefix != "addp_api_pre" {
		t.Fatalf("API consumer facts = %#v", entry)
	}
	if !strings.Contains(entry.RequestParams, `"search":["visible"]`) {
		t.Fatalf("safe query parameter missing: %s", entry.RequestParams)
	}
	for _, secret := range []string{
		"body-password", "123456", "body-challenge", "body-token",
		"query-password", "654321", "query-challenge", "query-token", "query-secret",
	} {
		if strings.Contains(entry.RequestParams, secret) {
			t.Fatalf("access log contains sensitive value %q: %s", secret, entry.RequestParams)
		}
	}
	if strings.Contains(entry.RequestParams, `"body"`) || !strings.Contains(entry.RequestParams, "[REDACTED]") {
		t.Fatalf("request parameter policy not applied: %s", entry.RequestParams)
	}
	remainingBody, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read untouched request body: %v", err)
	}
	if string(remainingBody) != body {
		t.Fatalf("request body was consumed or changed: %s", remainingBody)
	}
}

func TestAccessLogRejectsBrowserAndPublicRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/system/auth/mfa-verifications", strings.NewReader(`{"code":"123456"}`))

	middleware := &AccessLoggerMiddleware{db: &gorm.DB{}}
	if entry := middleware.buildAccessLog(context, http.StatusOK, 1); entry != nil {
		t.Fatalf("non-API-Key request produced Gateway access log: %#v", entry)
	}
}

func TestAPICredentialLogPrefixIsBounded(t *testing.T) {
	if got := apiCredentialLogPrefix("short"); got != "" {
		t.Fatalf("short prefix = %q", got)
	}
	if got := apiCredentialLogPrefix("addp_api_0123456789abcdef"); got != "addp_api_012" {
		t.Fatalf("bounded prefix = %q", got)
	}
}
