package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	i18nmiddleware "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

func TestIAMUserSelfHandlerContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	email := "alice@example.com"
	locale := "zh-cn"

	t.Run("current profile exposes only stable user and local account fields", func(t *testing.T) {
		service := &fakeIAMUserSelfService{profile: &iam.CurrentUserProfile{
			ID:           42,
			DisplayName:  "Alice",
			PrimaryEmail: &email,
			Locale:       &locale,
			CreatedAt:    now.Add(-time.Hour),
			UpdatedAt:    now,
			LocalAccount: &iam.CurrentLocalAccountProfile{Username: "alice"},
		}}
		router := newIAMUserSelfTestRouter(t, service, true)
		recorder := performIAMJSONRequest(t, router, http.MethodGet, "/api/v1/system/users/me", nil, map[string]string{
			"Authorization": "Bearer addp_at_access",
		})
		if recorder.Code != http.StatusOK || service.profileToken != "addp_at_access" {
			t.Fatalf("profile status=%d token=%q body=%s", recorder.Code, service.profileToken, recorder.Body.String())
		}
		var response map[string]any
		decodeIAMResponse(t, recorder, &response)
		if response["id"] != "42" || response["display_name"] != "Alice" ||
			response["primary_email"] != email || response["locale"] != locale {
			t.Fatalf("profile response = %#v", response)
		}
		localAccount, ok := response["local_account"].(map[string]any)
		if !ok || localAccount["username"] != "alice" {
			t.Fatalf("local account response = %#v", response["local_account"])
		}
		for _, forbiddenField := range []string{
			"user_type", "tenant_id", "authorization_version", "roles", "permissions", "memberships", "status",
		} {
			if _, exists := response[forbiddenField]; exists {
				t.Fatalf("profile response contains forbidden field %q: %#v", forbiddenField, response)
			}
		}
	})

	t.Run("external identity profile has a null local account", func(t *testing.T) {
		service := &fakeIAMUserSelfService{profile: &iam.CurrentUserProfile{
			ID:          43,
			DisplayName: "External User",
			CreatedAt:   now.Add(-time.Hour),
			UpdatedAt:   now,
		}}
		router := newIAMUserSelfTestRouter(t, service, false)
		recorder := performIAMJSONRequest(t, router, http.MethodGet, "/api/v1/system/users/me", nil, map[string]string{
			"Authorization": "Bearer addp_at_external",
		})
		var response IAMCurrentUserResponse
		decodeIAMResponse(t, recorder, &response)
		if recorder.Code != http.StatusOK || response.LocalAccount != nil || response.PrimaryEmail != nil || response.Locale != nil {
			t.Fatalf("external profile status=%d response=%#v", recorder.Code, response)
		}
		assertJSONHasNullField(t, recorder.Body.Bytes(), "primary_email")
		assertJSONHasNullField(t, recorder.Body.Bytes(), "locale")
		assertJSONHasNullField(t, recorder.Body.Bytes(), "local_account")
	})

	t.Run("password request is strict and field errors do not clear session cookies", func(t *testing.T) {
		service := &fakeIAMUserSelfService{}
		router := newIAMUserSelfTestRouter(t, service, false)
		headers := map[string]string{"Authorization": "Bearer addp_at_access"}
		unknown := performIAMJSONRequest(t, router, http.MethodPut, "/api/v1/system/users/me/password", map[string]any{
			"current_password": "old",
			"new_password":     "new",
			"minimum_length":   6,
		}, headers)
		if unknown.Code != http.StatusBadRequest || service.rotationToken != "" {
			t.Fatalf("unknown password field status=%d token=%q", unknown.Code, service.rotationToken)
		}

		service.rotationErr = iam.ErrInvalidCurrentPassword
		invalidCurrent := performIAMJSONRequest(t, router, http.MethodPut, "/api/v1/system/users/me/password", map[string]any{
			"current_password": "wrong",
			"new_password":     "new-password",
		}, headers)
		var invalidResponse IAMErrorResponse
		decodeIAMResponse(t, invalidCurrent, &invalidResponse)
		if invalidCurrent.Code != http.StatusBadRequest || invalidResponse.ErrorCode == nil ||
			*invalidResponse.ErrorCode != "invalid_current_password" || len(invalidCurrent.Result().Cookies()) != 0 {
			t.Fatalf("invalid current password status=%d response=%#v cookies=%#v",
				invalidCurrent.Code, invalidResponse, invalidCurrent.Result().Cookies())
		}

		service.rotationErr = iam.ErrPasswordUnchanged
		unchanged := performIAMJSONRequest(t, router, http.MethodPut, "/api/v1/system/users/me/password", map[string]any{
			"current_password": "same-password",
			"new_password":     "same-password",
		}, headers)
		var unchangedResponse IAMErrorResponse
		decodeIAMResponse(t, unchanged, &unchangedResponse)
		if unchanged.Code != http.StatusBadRequest || unchangedResponse.ErrorCode == nil ||
			*unchangedResponse.ErrorCode != "password_unchanged" || len(unchanged.Result().Cookies()) != 0 {
			t.Fatalf("unchanged password status=%d response=%#v", unchanged.Code, unchangedResponse)
		}
	})

	t.Run("successful password rotation clears all browser cookies", func(t *testing.T) {
		service := &fakeIAMUserSelfService{rotationResult: &iam.PasswordRotationResult{
			ChangedAt:          now,
			RevokedFamilyCount: 3,
		}}
		router := newIAMUserSelfTestRouter(t, service, true)
		request := newIAMJSONRequest(t, http.MethodPut, "/api/v1/system/users/me/password", map[string]any{
			"current_password": "old-password",
			"new_password":     "new-password",
		})
		request.Header.Set("Authorization", "Bearer addp_at_access")
		request.Header.Set(requestidmiddleware.RequestIDHeader, "password-request")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		var response IAMPasswordRotationResponse
		decodeIAMResponse(t, recorder, &response)
		if recorder.Code != http.StatusOK || response.RevokedFamilyCount != 3 || !response.ChangedAt.Equal(now) ||
			service.rotationToken != "addp_at_access" || service.currentPassword != "old-password" ||
			service.newPassword != "new-password" || service.rotationAudit.RequestID == nil ||
			*service.rotationAudit.RequestID != "password-request" {
			t.Fatalf("password rotation status=%d response=%#v service=%#v", recorder.Code, response, service)
		}
		cookies := recorder.Result().Cookies()
		assertIAMClearedCookies(t, cookies)
		for _, cookie := range cookies {
			if !cookie.Secure {
				t.Fatalf("password rotation clear cookie is not secure: %#v", cookie)
			}
		}
	})

	t.Run("invalid bearer does not call self service", func(t *testing.T) {
		service := &fakeIAMUserSelfService{}
		router := newIAMUserSelfTestRouter(t, service, false)
		profile := performIAMJSONRequest(t, router, http.MethodGet, "/api/v1/system/users/me", nil, nil)
		password := performIAMJSONRequest(t, router, http.MethodPut, "/api/v1/system/users/me/password", map[string]any{
			"current_password": "old",
			"new_password":     "new",
		}, nil)
		if profile.Code != http.StatusUnauthorized || password.Code != http.StatusUnauthorized ||
			service.profileToken != "" || service.rotationToken != "" {
			t.Fatalf("invalid bearer profile=%d password=%d service=%#v", profile.Code, password.Code, service)
		}
	})

	t.Run("invalid runtime profile is sanitized", func(t *testing.T) {
		service := &fakeIAMUserSelfService{profile: &iam.CurrentUserProfile{ID: 42}}
		router := newIAMUserSelfTestRouter(t, service, false)
		recorder := performIAMJSONRequest(t, router, http.MethodGet, "/api/v1/system/users/me", nil, map[string]string{
			"Authorization": "Bearer addp_at_access",
		})
		var response IAMErrorResponse
		decodeIAMResponse(t, recorder, &response)
		if recorder.Code != http.StatusInternalServerError || response.Error == "invalid current user profile" {
			t.Fatalf("invalid profile status=%d response=%#v", recorder.Code, response)
		}
	})

}

type fakeIAMUserSelfService struct {
	profileToken string
	profile      *iam.CurrentUserProfile
	profileErr   error

	rotationToken   string
	currentPassword string
	newPassword     string
	rotationAudit   iam.AuditMetadata
	rotationResult  *iam.PasswordRotationResult
	rotationErr     error
}

func (service *fakeIAMUserSelfService) ResolveCurrentUserProfile(
	_ context.Context,
	accessToken string,
) (*iam.CurrentUserProfile, error) {
	service.profileToken = accessToken
	return service.profile, service.profileErr
}

func (service *fakeIAMUserSelfService) RotateCurrentPassword(
	_ context.Context,
	accessToken string,
	currentPassword string,
	newPassword string,
	audit iam.AuditMetadata,
) (*iam.PasswordRotationResult, error) {
	service.rotationToken = accessToken
	service.currentPassword = currentPassword
	service.newPassword = newPassword
	service.rotationAudit = audit
	return service.rotationResult, service.rotationErr
}

func newIAMUserSelfTestRouter(
	t *testing.T,
	service *fakeIAMUserSelfService,
	secureCookies bool,
) *gin.Engine {
	t.Helper()
	handler, err := NewIAMUserSelfHandler(service, secureCookies, []string{"manager", "standard"})
	if err != nil {
		t.Fatalf("create IAM user self handler: %v", err)
	}
	router := gin.New()
	router.Use(requestidmiddleware.RequestIDMiddleware())
	router.Use(i18nmiddleware.I18nMiddleware())
	router.GET("/api/v1/system/users/me", handler.Me)
	router.PUT("/api/v1/system/users/me/password", handler.ChangePassword)
	return router
}

func assertJSONHasNullField(t *testing.T, data []byte, field string) {
	t.Helper()
	var response map[string]json.RawMessage
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode JSON fields: %v", err)
	}
	value, exists := response[field]
	if !exists || string(value) != "null" {
		t.Fatalf("field %s = %s", field, value)
	}
}
