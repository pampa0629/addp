package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type fakeIAMDelegationService struct {
	input  *iam.IssueDelegatedAccessTokenInput
	issued *iam.IssuedDelegatedAccessToken
	err    error
}

func (service *fakeIAMDelegationService) IssueDelegatedAccessToken(
	_ context.Context,
	input iam.IssueDelegatedAccessTokenInput,
) (*iam.IssuedDelegatedAccessToken, error) {
	service.input = &input
	return service.issued, service.err
}

func TestIAMDelegationHandlerContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	t.Run("issues one bounded delegated token", func(t *testing.T) {
		service := &fakeIAMDelegationService{issued: &iam.IssuedDelegatedAccessToken{
			AccessToken: "addp_dat_issued",
			TokenType:   "Bearer",
			ExpiresAt:   now.Add(2 * time.Minute),
			Audience:    "develop",
			Scopes:      []string{"workflow.run"},
			AgentRunID:  "run-1",
			ToolCallID:  "call-1",
		}}
		router := newIAMDelegationTestRouter(t, service, now)
		request := newIAMJSONRequest(t, http.MethodPost, "/api/v1/system/auth/delegations", map[string]any{
			"audience":     "develop",
			"scopes":       []string{"workflow.run"},
			"agent_run_id": "run-1",
			"tool_call_id": "call-1",
		})
		request.Header.Set("Authorization", "Bearer addp_at_source")
		request.Header.Set(requestidmiddleware.RequestIDHeader, "delegation-request")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusCreated {
			t.Fatalf("delegation status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var response IAMDelegationResponse
		decodeIAMResponse(t, recorder, &response)
		if response.AccessToken != "addp_dat_issued" || response.TokenType != "Bearer" ||
			response.ExpiresIn != 120 || response.Audience != "develop" ||
			len(response.Scopes) != 1 || response.Scopes[0] != "workflow.run" {
			t.Fatalf("delegation response=%#v", response)
		}
		if service.input == nil || service.input.SourceAccessToken != "addp_at_source" ||
			service.input.Audit.RequestID == nil || *service.input.Audit.RequestID != "delegation-request" ||
			service.input.Audit.HTTPStatus == nil || *service.input.Audit.HTTPStatus != http.StatusCreated {
			t.Fatalf("delegation input=%#v", service.input)
		}
	})

	t.Run("rejects unknown JSON fields before service", func(t *testing.T) {
		service := &fakeIAMDelegationService{}
		router := newIAMDelegationTestRouter(t, service, now)
		recorder := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/delegations", map[string]any{
			"audience":     "develop",
			"scopes":       []string{"workflow.run"},
			"agent_run_id": "run-1",
			"tool_call_id": "call-1",
			"principal_id": "99",
		}, map[string]string{"Authorization": "Bearer addp_at_source"})
		assertIAMDelegationError(t, recorder, http.StatusBadRequest, "invalid_delegation_request")
		if service.input != nil {
			t.Fatalf("service received invalid request: %#v", service.input)
		}
	})

	t.Run("maps stable authorization errors", func(t *testing.T) {
		tests := []struct {
			name   string
			err    error
			status int
			code   string
		}{
			{name: "unauthorized", err: commonapi.ErrUnauthorized, status: http.StatusUnauthorized, code: "authentication_required"},
			{name: "forbidden", err: iam.ErrDelegationPermissionDenied, status: http.StatusForbidden, code: "permission_denied"},
			{name: "conflict", err: iam.ErrDelegationConflict, status: http.StatusConflict, code: "delegation_conflict"},
			{name: "internal", err: errors.New("database unavailable"), status: http.StatusInternalServerError, code: "delegation_internal_error"},
		}
		for _, testCase := range tests {
			t.Run(testCase.name, func(t *testing.T) {
				service := &fakeIAMDelegationService{err: testCase.err}
				router := newIAMDelegationTestRouter(t, service, now)
				recorder := performIAMJSONRequest(t, router, http.MethodPost, "/api/v1/system/auth/delegations", map[string]any{
					"audience":     "develop",
					"scopes":       []string{"workflow.run"},
					"agent_run_id": "run-1",
					"tool_call_id": "call-1",
				}, map[string]string{"Authorization": "Bearer addp_at_source"})
				assertIAMDelegationError(t, recorder, testCase.status, testCase.code)
			})
		}
	})
}

func newIAMDelegationTestRouter(
	t *testing.T,
	service iamDelegationService,
	now time.Time,
) *gin.Engine {
	t.Helper()
	handler, err := NewIAMDelegationHandler(service, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(requestidmiddleware.RequestIDMiddleware())
	router.Use(i18nmiddleware.I18nMiddleware())
	router.POST("/api/v1/system/auth/delegations", handler.CreateDelegation)
	return router
}

func assertIAMDelegationError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var response IAMErrorResponse
	decodeIAMResponse(t, recorder, &response)
	if recorder.Code != status || response.Error == "" || response.ErrorCode == nil || *response.ErrorCode != code {
		t.Fatalf("delegation error status=%d response=%#v", recorder.Code, response)
	}
}
