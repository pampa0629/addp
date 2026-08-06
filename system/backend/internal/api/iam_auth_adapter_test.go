package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
)

func TestIAMAuthHandlerContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	t.Run("login session response sets deadline-bound cookies and audit metadata", func(t *testing.T) {
		runtime := &fakeIAMAuthRuntime{loginResult: &iam.ContextSelectionResult{
			NextAction: iam.ContextSelectionNextActionSessionIssued,
			Session:    testIAMBrowserSession(now),
		}}
		router := newIAMAuthTestRouter(t, runtime, now, true)
		request := newIAMJSONRequest(t, http.MethodPost, "/api/v1/system/login", map[string]any{
			"username": "alice",
			"password": "secret",
		})
		request.Header.Set(requestidmiddleware.RequestIDHeader, "iam-login-request")
		request.Header.Set("User-Agent", "iam-test-agent")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("login status = %d body=%s", recorder.Code, recorder.Body.String())
		}
		var response IAMBrowserLoginResponse
		decodeIAMResponse(t, recorder, &response)
		if response.NextAction != "session_issued" || response.Session == nil || response.Selection != nil ||
			response.Session.AccessToken != "addp_at_access" || response.Session.TokenType != "Bearer" ||
			response.Session.ExpiresIn != 900 {
			t.Fatalf("login response = %#v", response)
		}
		if runtime.loginInput == nil || runtime.loginInput.Username != "alice" ||
			runtime.loginInput.Audit.RequestID == nil || *runtime.loginInput.Audit.RequestID != "iam-login-request" ||
			runtime.loginInput.Audit.HTTPMethod == nil || *runtime.loginInput.Audit.HTTPMethod != http.MethodPost ||
			runtime.loginInput.Audit.ResourcePath == nil || *runtime.loginInput.Audit.ResourcePath != "/api/v1/system/login" ||
			runtime.loginInput.Audit.UserAgent == nil || *runtime.loginInput.Audit.UserAgent != "iam-test-agent" {
			t.Fatalf("login input = %#v", runtime.loginInput)
		}
		assertIAMSessionCookies(t, recorder.Result().Cookies(), true, 30*24*60*60, 600)
	})

	t.Run("role assignment conflict uses a stable domain error", func(t *testing.T) {
		router := gin.New()
		router.Use(i18nmiddleware.I18nMiddleware())
		router.GET("/conflict", func(c *gin.Context) {
			respondIAMError(c, iam.ErrTenantRoleAssignmentAlreadyExists)
		})
		request := httptest.NewRequest(http.MethodGet, "/conflict", nil)
		request.Header.Set("Accept-Language", "zh-cn")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		var response IAMErrorResponse
		decodeIAMResponse(t, recorder, &response)
		if recorder.Code != http.StatusConflict || response.ErrorCode == nil ||
			*response.ErrorCode != "role_assignment_already_exists" ||
			response.Error != "该成员已在此授权范围拥有该角色，无需重复分配。" {
			t.Fatalf("role assignment conflict status=%d response=%#v", recorder.Code, response)
		}
	})

	t.Run("role assignment principal type conflict is localized with a stable domain error", func(t *testing.T) {
		for _, test := range []struct {
			name     string
			language string
			message  string
		}{
			{name: "zh-cn", language: "zh-cn", message: "该角色不能分配给此类型的成员。"},
			{name: "en", language: "en", message: "The role cannot be assigned to this member type."},
		} {
			t.Run(test.name, func(t *testing.T) {
				router := gin.New()
				router.Use(i18nmiddleware.I18nMiddleware())
				router.GET("/conflict", func(c *gin.Context) {
					respondIAMError(c, iam.ErrTenantRoleAssignmentPrincipalTypeNotAllowed)
				})
				request := httptest.NewRequest(http.MethodGet, "/conflict", nil)
				request.Header.Set("Accept-Language", test.language)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)

				var response IAMErrorResponse
				decodeIAMResponse(t, recorder, &response)
				if recorder.Code != http.StatusConflict || response.ErrorCode == nil ||
					*response.ErrorCode != "role_assignment_principal_type_not_allowed" || response.Error != test.message {
					t.Fatalf("role assignment principal type conflict status=%d response=%#v", recorder.Code, response)
				}
			})
		}
	})

	t.Run("multi-context login returns only a selection challenge", func(t *testing.T) {
		membershipID := int64(22)
		tenantID := int64(11)
		runtime := &fakeIAMAuthRuntime{loginResult: &iam.ContextSelectionResult{
			NextAction: iam.ContextSelectionNextActionSelectContext,
			Challenge: &iam.ContextSelectionChallenge{
				SelectionTicket: "addp_cst_selection",
				ExpiresAt:       now.Add(5 * time.Minute),
				Contexts: []iam.AvailableContext{
					{Type: iam.ContextTypePlatform},
					{
						Type:               iam.ContextTypeTenant,
						TenantID:           &tenantID,
						TenantMembershipID: &membershipID,
						TenantCode:         "tenant-a",
						TenantName:         "Tenant A",
					},
				},
			},
		}}
		router := newIAMAuthTestRouter(t, runtime, now, false)
		recorder := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/login", map[string]any{
			"username": "alice",
			"password": "secret",
		}, nil)

		var response IAMBrowserLoginResponse
		decodeIAMResponse(t, recorder, &response)
		if recorder.Code != http.StatusOK || response.NextAction != "select_context" ||
			response.Session != nil || response.Selection == nil || len(response.Selection.Contexts) != 2 ||
			response.Selection.Contexts[0].TenantID != nil ||
			response.Selection.Contexts[1].TenantID == nil || *response.Selection.Contexts[1].TenantID != "11" ||
			response.Selection.Contexts[1].TenantMembershipID == nil ||
			*response.Selection.Contexts[1].TenantMembershipID != "22" {
			t.Fatalf("selection login status=%d response=%#v", recorder.Code, response)
		}
		if len(recorder.Result().Cookies()) != 0 {
			t.Fatalf("selection login cookies = %#v", recorder.Result().Cookies())
		}
	})

	t.Run("MFA login challenge is completed before session issuance", func(t *testing.T) {
		runtime := &fakeIAMAuthRuntime{
			loginResult: &iam.ContextSelectionResult{
				NextAction: iam.ContextSelectionNextActionVerifyMFA,
				MFA: &iam.IssuedMFAChallenge{
					ChallengeToken: "addp_mfc_challenge", ExpiresAt: now.Add(5 * time.Minute),
				},
			},
			mfaResult: &iam.ContextSelectionResult{
				NextAction: iam.ContextSelectionNextActionSessionIssued,
				Session:    testIAMBrowserSession(now),
			},
		}
		router := newIAMAuthTestRouter(t, runtime, now, false)
		login := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/login", map[string]any{
			"username": "platform-admin", "password": "secret",
		}, nil)
		var loginResponse IAMBrowserLoginResponse
		decodeIAMResponse(t, login, &loginResponse)
		if login.Code != http.StatusOK || loginResponse.NextAction != "verify_mfa" ||
			loginResponse.MFA == nil || loginResponse.MFA.Method != "totp" ||
			loginResponse.Session != nil || loginResponse.Selection != nil || len(login.Result().Cookies()) != 0 {
			t.Fatalf("MFA login status=%d response=%#v cookies=%#v", login.Code, loginResponse, login.Result().Cookies())
		}

		verification := performIAMJSONRequest(
			t,
			router,
			http.MethodPost,
			"/api/v1/system/auth/mfa-verifications",
			map[string]any{"challenge_token": "addp_mfc_challenge", "code": "123456"},
			nil,
		)
		var verificationResponse IAMBrowserLoginResponse
		decodeIAMResponse(t, verification, &verificationResponse)
		if verification.Code != http.StatusOK || verificationResponse.NextAction != "session_issued" ||
			verificationResponse.Session == nil || runtime.mfaInput == nil ||
			runtime.mfaInput.ChallengeToken != "addp_mfc_challenge" || runtime.mfaInput.Code != "123456" {
			t.Fatalf("MFA verification status=%d response=%#v input=%#v",
				verification.Code, verificationResponse, runtime.mfaInput)
		}
		assertIAMSessionCookies(t, verification.Result().Cookies(), false, 30*24*60*60, 600)
	})

	t.Run("context selection rejects unknown fields and non-canonical IDs", func(t *testing.T) {
		runtime := &fakeIAMAuthRuntime{selectionSession: testIAMBrowserSession(now)}
		router := newIAMAuthTestRouter(t, runtime, now, false)
		unknown := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/context-selections", map[string]any{
			"selection_ticket":     "addp_cst_selection",
			"context_type":         "tenant",
			"tenant_membership_id": "22",
			"tenant_id":            "11",
		}, nil)
		if unknown.Code != http.StatusBadRequest || runtime.selectionInput != nil {
			t.Fatalf("unknown-field selection status=%d input=%#v", unknown.Code, runtime.selectionInput)
		}

		nonCanonical := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/context-selections", map[string]any{
			"selection_ticket":     "addp_cst_selection",
			"context_type":         "tenant",
			"tenant_membership_id": "022",
		}, nil)
		if nonCanonical.Code != http.StatusBadRequest || runtime.selectionInput != nil {
			t.Fatalf("non-canonical selection status=%d input=%#v", nonCanonical.Code, runtime.selectionInput)
		}

		valid := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/context-selections", map[string]any{
			"selection_ticket":     "addp_cst_selection",
			"context_type":         "tenant",
			"tenant_membership_id": "22",
		}, nil)
		if valid.Code != http.StatusOK || runtime.selectionInput == nil ||
			runtime.selectionInput.Choice.TenantMembershipID == nil ||
			*runtime.selectionInput.Choice.TenantMembershipID != 22 {
			t.Fatalf("valid selection status=%d input=%#v body=%s", valid.Code, runtime.selectionInput, valid.Body.String())
		}
	})

	t.Run("context and options require bearer and preserve option semantics", func(t *testing.T) {
		membershipID := int64(22)
		tenantID := int64(11)
		runtime := &fakeIAMAuthRuntime{
			authContext: &commonauth.AuthContext{SchemaVersion: commonauth.AuthContextSchemaVersion},
			contextOptions: []iam.BrowserContextOption{
				{
					AvailableContext: iam.AvailableContext{Type: iam.ContextTypePlatform},
					RequiresStepUp:   true,
				},
				{
					AvailableContext: iam.AvailableContext{
						Type:               iam.ContextTypeTenant,
						TenantID:           &tenantID,
						TenantMembershipID: &membershipID,
						TenantCode:         "tenant-a",
						TenantName:         "Tenant A",
					},
					Current: true,
				},
			},
		}
		router := newIAMAuthTestRouter(t, runtime, now, false)
		unauthorized := performIAMJSONRequest(t, router, http.MethodGet, "/api/v1/system/auth/context", nil, nil)
		if unauthorized.Code != http.StatusUnauthorized {
			t.Fatalf("context without bearer status = %d", unauthorized.Code)
		}
		headers := map[string]string{"Authorization": "Bearer addp_at_access"}
		contextRecorder := performIAMJSONRequest(t, router, http.MethodGet, "/api/v1/system/auth/context", nil, headers)
		if contextRecorder.Code != http.StatusOK || runtime.authContextToken != "addp_at_access" {
			t.Fatalf("context status=%d token=%q", contextRecorder.Code, runtime.authContextToken)
		}
		resourceHeaders := map[string]string{"Authorization": "Bearer addp_rat_manager"}
		resourceRecorder := performIAMJSONRequest(
			t,
			router,
			http.MethodGet,
			"/api/v1/system/auth/context",
			nil,
			resourceHeaders,
		)
		if resourceRecorder.Code != http.StatusOK || runtime.authContextToken != "addp_rat_manager" {
			t.Fatalf("resource context status=%d token=%q", resourceRecorder.Code, runtime.authContextToken)
		}
		delegatedHeaders := map[string]string{"Authorization": "Bearer addp_dat_workflow"}
		delegatedRecorder := performIAMJSONRequest(
			t,
			router,
			http.MethodGet,
			"/api/v1/system/auth/context",
			nil,
			delegatedHeaders,
		)
		if delegatedRecorder.Code != http.StatusOK || runtime.authContextToken != "addp_dat_workflow" {
			t.Fatalf("delegated context status=%d token=%q", delegatedRecorder.Code, runtime.authContextToken)
		}
		optionsRecorder := performIAMJSONRequest(t, router, http.MethodGet, "/api/v1/system/auth/context-options", nil, headers)
		var response IAMContextOptionsResponse
		decodeIAMResponse(t, optionsRecorder, &response)
		if optionsRecorder.Code != http.StatusOK || runtime.contextOptionsToken != "addp_at_access" ||
			len(response.Contexts) != 2 || !response.Contexts[0].RequiresStepUp ||
			!response.Contexts[1].Current || response.Contexts[1].TenantID == nil ||
			*response.Contexts[1].TenantID != "11" {
			t.Fatalf("context options status=%d response=%#v", optionsRecorder.Code, response)
		}
	})

	t.Run("switch maps step-up without setting replacement cookies", func(t *testing.T) {
		runtime := &fakeIAMAuthRuntime{switchErr: iam.ErrStepUpRequired}
		router := newIAMAuthTestRouter(t, runtime, now, false)
		headers := map[string]string{
			"Authorization": "Bearer addp_at_access",
			"Cookie":        iamRefreshCookieName + "=addp_rt_refresh",
		}
		recorder := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/context-switches", map[string]any{
			"context_type": "platform",
		}, headers)
		var response IAMErrorResponse
		decodeIAMResponse(t, recorder, &response)
		if recorder.Code != http.StatusForbidden || response.ErrorCode == nil ||
			*response.ErrorCode != "step_up_required" || len(recorder.Result().Cookies()) != 0 ||
			runtime.switchInput == nil || runtime.switchInput.AccessToken != "addp_at_access" ||
			runtime.switchInput.RefreshToken != "addp_rt_refresh" {
			t.Fatalf("step-up switch status=%d response=%#v input=%#v cookies=%#v",
				recorder.Code, response, runtime.switchInput, recorder.Result().Cookies())
		}
	})

	t.Run("refresh clears only invalid sessions", func(t *testing.T) {
		headers := map[string]string{"Cookie": iamRefreshCookieName + "=addp_rt_refresh"}

		missingRuntime := &fakeIAMAuthRuntime{}
		missingRouter := newIAMAuthTestRouter(t, missingRuntime, now, false)
		missing := performIAMJSONRequest(t, missingRouter, http.MethodPost, "/api/v1/system/refresh", nil, nil)
		if missing.Code != http.StatusUnauthorized {
			t.Fatalf("missing refresh status = %d", missing.Code)
		}
		assertIAMClearedCookies(t, missing.Result().Cookies())

		unauthorizedRuntime := &fakeIAMAuthRuntime{refreshErr: commonapi.ErrUnauthorized}
		unauthorizedRouter := newIAMAuthTestRouter(t, unauthorizedRuntime, now, false)
		unauthorized := performIAMJSONRequest(t, unauthorizedRouter, http.MethodPost, "/api/v1/system/refresh", nil, headers)
		if unauthorized.Code != http.StatusUnauthorized {
			t.Fatalf("invalid refresh status = %d", unauthorized.Code)
		}
		assertIAMClearedCookies(t, unauthorized.Result().Cookies())

		conflictRuntime := &fakeIAMAuthRuntime{refreshErr: iam.ErrRefreshTokenRotationConflict}
		conflictRouter := newIAMAuthTestRouter(t, conflictRuntime, now, false)
		conflict := performIAMJSONRequest(t, conflictRouter, http.MethodPost, "/api/v1/system/refresh", nil, headers)
		if conflict.Code != http.StatusConflict || len(conflict.Result().Cookies()) != 0 {
			t.Fatalf("conflict refresh status=%d cookies=%#v", conflict.Code, conflict.Result().Cookies())
		}

		successRuntime := &fakeIAMAuthRuntime{refreshSession: testIAMBrowserSession(now)}
		successRouter := newIAMAuthTestRouter(t, successRuntime, now, false)
		success := performIAMJSONRequest(t, successRouter, http.MethodPost, "/api/v1/system/refresh", nil, headers)
		if success.Code != http.StatusOK || successRuntime.refreshInput == nil ||
			successRuntime.refreshInput.RefreshToken != "addp_rt_refresh" {
			t.Fatalf("successful refresh status=%d input=%#v", success.Code, successRuntime.refreshInput)
		}
		assertIAMSessionCookies(t, success.Result().Cookies(), false, 30*24*60*60, 600)
	})

	t.Run("logout always clears cookies but only first success is 204", func(t *testing.T) {
		headers := map[string]string{
			"Authorization": "Bearer addp_at_access",
			"Cookie":        iamRefreshCookieName + "=addp_rt_refresh",
		}
		successRuntime := &fakeIAMAuthRuntime{}
		successRouter := newIAMAuthTestRouter(t, successRuntime, now, false)
		success := performIAMJSONRequest(t, successRouter, http.MethodPost, "/api/v1/system/logout", nil, headers)
		if success.Code != http.StatusNoContent || successRuntime.logoutInput == nil ||
			successRuntime.logoutInput.AccessToken != "addp_at_access" ||
			successRuntime.logoutInput.RefreshToken != "addp_rt_refresh" {
			t.Fatalf("successful logout status=%d input=%#v", success.Code, successRuntime.logoutInput)
		}
		assertIAMClearedCookies(t, success.Result().Cookies())

		repeatRuntime := &fakeIAMAuthRuntime{logoutErr: commonapi.ErrUnauthorized}
		repeatRouter := newIAMAuthTestRouter(t, repeatRuntime, now, false)
		repeat := performIAMJSONRequest(t, repeatRouter, http.MethodPost, "/api/v1/system/logout", nil, headers)
		if repeat.Code != http.StatusUnauthorized {
			t.Fatalf("repeated logout status = %d", repeat.Code)
		}
		assertIAMClearedCookies(t, repeat.Result().Cookies())
	})

	t.Run("internal errors are not exposed", func(t *testing.T) {
		runtime := &fakeIAMAuthRuntime{loginErr: errors.New("secret database failure")}
		router := newIAMAuthTestRouter(t, runtime, now, false)
		recorder := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/login", map[string]any{
			"username": "alice",
			"password": "secret",
		}, nil)
		if recorder.Code != http.StatusInternalServerError || bytes.Contains(recorder.Body.Bytes(), []byte("secret database")) {
			t.Fatalf("internal error status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	})
}

type fakeIAMAuthRuntime struct {
	loginInput  *iam.LoginLocalBrowserInput
	loginResult *iam.ContextSelectionResult
	loginErr    error
	mfaInput    *iam.VerifyMFAChallengeInput
	mfaResult   *iam.ContextSelectionResult
	mfaErr      error

	selectionInput   *iam.ConsumeContextSelectionInput
	selectionSession *iam.IssuedBrowserSession
	selectionErr     error

	authContextToken string
	authContext      *commonauth.AuthContext
	authContextErr   error

	contextOptionsToken string
	contextOptions      []iam.BrowserContextOption
	contextOptionsErr   error

	switchInput   *iam.SwitchBrowserContextInput
	switchSession *iam.IssuedBrowserSession
	switchErr     error

	refreshInput   *iam.RotateBrowserRefreshTokenInput
	refreshSession *iam.IssuedBrowserSession
	refreshErr     error

	logoutInput *iam.LogoutBrowserSessionInput
	logoutErr   error
}

func (runtime *fakeIAMAuthRuntime) LoginLocalBrowser(
	_ context.Context,
	input iam.LoginLocalBrowserInput,
) (*iam.ContextSelectionResult, error) {
	runtime.loginInput = &input
	return runtime.loginResult, runtime.loginErr
}

func (runtime *fakeIAMAuthRuntime) VerifyLocalBrowserMFA(
	_ context.Context,
	input iam.VerifyMFAChallengeInput,
) (*iam.ContextSelectionResult, error) {
	runtime.mfaInput = &input
	return runtime.mfaResult, runtime.mfaErr
}

func (runtime *fakeIAMAuthRuntime) ConsumeContextSelection(
	_ context.Context,
	input iam.ConsumeContextSelectionInput,
) (*iam.IssuedBrowserSession, error) {
	runtime.selectionInput = &input
	return runtime.selectionSession, runtime.selectionErr
}

func (runtime *fakeIAMAuthRuntime) ResolveAuthContext(
	_ context.Context,
	accessToken string,
) (*commonauth.AuthContext, error) {
	runtime.authContextToken = accessToken
	return runtime.authContext, runtime.authContextErr
}

func (runtime *fakeIAMAuthRuntime) ListBrowserContextOptions(
	_ context.Context,
	accessToken string,
) ([]iam.BrowserContextOption, error) {
	runtime.contextOptionsToken = accessToken
	return runtime.contextOptions, runtime.contextOptionsErr
}

func (runtime *fakeIAMAuthRuntime) SwitchBrowserContext(
	_ context.Context,
	input iam.SwitchBrowserContextInput,
) (*iam.IssuedBrowserSession, error) {
	runtime.switchInput = &input
	return runtime.switchSession, runtime.switchErr
}

func (runtime *fakeIAMAuthRuntime) RotateBrowserRefreshToken(
	_ context.Context,
	input iam.RotateBrowserRefreshTokenInput,
) (*iam.IssuedBrowserSession, error) {
	runtime.refreshInput = &input
	return runtime.refreshSession, runtime.refreshErr
}

func (runtime *fakeIAMAuthRuntime) LogoutBrowserSession(
	_ context.Context,
	input iam.LogoutBrowserSessionInput,
) error {
	runtime.logoutInput = &input
	return runtime.logoutErr
}

func newIAMAuthTestRouter(
	t *testing.T,
	runtime *fakeIAMAuthRuntime,
	now time.Time,
	secureCookies bool,
) *gin.Engine {
	t.Helper()
	handler, err := NewIAMAuthHandler(
		runtime,
		runtime,
		runtime,
		runtime,
		runtime,
		runtime,
		runtime,
		IAMAuthHandlerConfig{
			SecureCookies:        secureCookies,
			ResourceTicketOwners: []string{"manager", "standard"},
			Now:                  func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatalf("create IAM auth handler: %v", err)
	}
	router := gin.New()
	router.Use(requestidmiddleware.RequestIDMiddleware())
	router.Use(i18nmiddleware.I18nMiddleware())
	router.POST("/api/v1/system/login", handler.Login)
	router.POST("/api/v1/system/auth/mfa-verifications", handler.VerifyMFA)
	router.POST("/api/v1/system/auth/context-selections", handler.ConsumeContextSelection)
	router.GET("/api/v1/system/auth/context", handler.Context)
	router.GET("/api/v1/system/auth/context-options", handler.ContextOptions)
	router.POST("/api/v1/system/auth/context-switches", handler.SwitchContext)
	router.POST("/api/v1/system/refresh", handler.Refresh)
	router.POST("/api/v1/system/logout", handler.Logout)
	return router
}

func testIAMBrowserSession(now time.Time) *iam.IssuedBrowserSession {
	tenantID := int64(11)
	membershipID := int64(22)
	return &iam.IssuedBrowserSession{
		FamilyID: 1,
		Context: iam.ResolvedSessionContext{
			Type:               iam.ContextTypeTenant,
			TenantID:           &tenantID,
			TenantMembershipID: &membershipID,
		},
		AccessToken:  "addp_at_access",
		RefreshToken: "addp_rt_refresh",
		ResourceAccessTickets: map[string]string{
			"manager":  "addp_rat_manager",
			"standard": "addp_rat_standard",
		},
		AccessTokenExpiresAt:        now.Add(15 * time.Minute),
		RefreshTokenFamilyExpiresAt: now.Add(30 * 24 * time.Hour),
		ResourceTicketExpiresAt:     now.Add(10 * time.Minute),
	}
}

func performIAMJSONRequest(
	t *testing.T,
	router http.Handler,
	method string,
	path string,
	body any,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := newIAMJSONRequest(t, method, path, body)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func newIAMJSONRequest(t *testing.T, method string, path string, body any) *http.Request {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func decodeIAMResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
}

func assertIAMSessionCookies(
	t *testing.T,
	cookies []*http.Cookie,
	secure bool,
	refreshMaxAge int,
	resourceMaxAge int,
) {
	t.Helper()
	if len(cookies) != 3 {
		t.Fatalf("session cookies = %#v", cookies)
	}
	seen := map[string]bool{}
	for _, cookie := range cookies {
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Secure != secure {
			t.Fatalf("session cookie flags = %#v", cookie)
		}
		key := cookie.Name + "|" + cookie.Path
		seen[key] = true
		if cookie.Name == iamRefreshCookieName && cookie.MaxAge != refreshMaxAge {
			t.Fatalf("refresh cookie max age = %d", cookie.MaxAge)
		}
		if cookie.Name == models.BrowserResourceAccessTicketCookieName && cookie.MaxAge != resourceMaxAge {
			t.Fatalf("resource cookie max age = %d", cookie.MaxAge)
		}
	}
	for _, key := range []string{
		iamRefreshCookieName + "|/api/v1/system",
		models.BrowserResourceAccessTicketCookieName + "|/api/v1/manager",
		models.BrowserResourceAccessTicketCookieName + "|/api/v1/standard",
	} {
		if !seen[key] {
			t.Fatalf("missing session cookie %s from %#v", key, cookies)
		}
	}
}

func assertIAMClearedCookies(t *testing.T, cookies []*http.Cookie) {
	t.Helper()
	if len(cookies) != 3 {
		t.Fatalf("cleared cookies = %#v", cookies)
	}
	for _, cookie := range cookies {
		if cookie.MaxAge >= 0 || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("cookie was not cleared securely: %#v", cookie)
		}
	}
}
