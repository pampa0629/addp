package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestDataProfileHandlerRejectsMissingTrustedContextAndIdentityInput(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"current requires canonical context", "GET", "/data-profiles/current?locator=item", "", http.StatusUnauthorized},
		{"enqueue requires canonical context", "POST", "/data-profile-executions", `{"locator":"item"}`, http.StatusUnauthorized},
		{"actor cannot be supplied", "POST", "/data-profile-executions", `{"locator":"item","actor_principal_id":99}`, http.StatusBadRequest},
		{"membership cannot be supplied", "POST", "/data-profile-executions", `{"locator":"item","actor_tenant_membership_id":99}`, http.StatusBadRequest},
		{"version cannot be supplied", "POST", "/data-profile-executions", `{"locator":"item","issued_authorization_version":99}`, http.StatusBadRequest},
		{"token cannot be supplied", "POST", "/data-profile-executions", `{"locator":"item","token":"secret"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewDataProfileHandler(service.NewDataProfileService(nil, nil, nil, nil))
			notified := false
			h.SetExecutionEnqueueNotifier(func() { notified = true })
			router := gin.New()
			// Legacy scalar context facts must not substitute for AuthContext.
			router.Use(func(c *gin.Context) { c.Set("tenant_id", uint(7)); c.Set("user_id", uint(9)) })
			router.GET("/data-profiles/current", h.GetCurrent)
			router.POST("/data-profile-executions", h.CreateExecution)
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want || notified {
				t.Fatalf("status=%d want=%d notified=%v body=%s", response.Code, tc.want, notified, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "secret") {
				t.Fatal("rejected credential leaked in response")
			}
		})
	}
}

func TestProfileResultErrorDoesNotExposeSourceOrCredential(t *testing.T) {
	for _, lang := range []string{commoni18n.LangZhCN, commoni18n.LangEn} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Set("addp_lang", lang)
		err := fmt.Errorf("private source D credential addp_at_secret: %w", commonClient.ErrManagerPreviewReadDenied)
		handleDataProfileError(c, err, "unused")
		body := response.Body.String()
		if response.Code != http.StatusForbidden || !strings.Contains(body, "source_authorization_required") || strings.Contains(body, "addp_at_secret") || strings.Contains(body, "private source D") || strings.Contains(body, "manager.error.") {
			t.Fatalf("unsafe error response: %d %s", response.Code, body)
		}
	}
}

func TestDataProfileHandlerMapsActorErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{service.ErrDataProfileActorExpired, http.StatusUnauthorized},
		{service.ErrDataProfileActorRequired, http.StatusForbidden},
		{service.ErrDataProfileSourceAuthorizationRequired, http.StatusForbidden},
		{commonClient.ErrManagerPreviewCredentialRejected, http.StatusUnauthorized},
		{commonClient.ErrManagerPreviewReadDenied, http.StatusForbidden},
		{commonClient.ErrManagerPreviewReadUnavailable, http.StatusServiceUnavailable},
	} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		handleDataProfileError(c, tc.err, "unused")
		if response.Code != tc.status {
			t.Fatalf("status=%d want=%d", response.Code, tc.status)
		}
	}
}
