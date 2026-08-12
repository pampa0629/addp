package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/gin-gonic/gin"
)

func TestErrorResponseWithCode(t *testing.T) {
	response := errorResponseWithCode("invalid", "invalid_request")
	if response["error"] != "invalid" || response["error_code"] != "invalid_request" {
		t.Fatalf("unexpected error response: %#v", response)
	}
}

func TestServiceErrorResponseMapsDomainErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Accept-Language", "en")

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "validation", err: apperrors.Validation("logical_field_invalid", i18n.MsgValidationFailed), status: http.StatusBadRequest, code: "logical_field_invalid"},
		{name: "not found", err: apperrors.NotFound("entity_not_found", i18n.MsgEntityNotFound), status: http.StatusNotFound, code: "entity_not_found"},
		{name: "conflict", err: apperrors.Conflict("entity_code_conflict", i18n.MsgEntityCodeConflict), status: http.StatusConflict, code: "entity_code_conflict"},
		{name: "internal", err: errors.New("database unavailable"), status: http.StatusInternalServerError, code: "model_operation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, response := serviceErrorResponse(c, tt.err)
			if status != tt.status || response["error_code"] != tt.code || response["error"] == "" {
				t.Fatalf("status = %d, response = %#v", status, response)
			}
		})
	}
}

func TestServiceErrorResponseLocalizesDomainError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	err := apperrors.Conflict("entity_code_conflict", i18n.MsgEntityCodeConflict)

	responseForLanguage := func(language string) gin.H {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Accept-Language", language)
		commoni18n.I18nMiddleware()(c)
		_, response := serviceErrorResponse(c, err)
		return response
	}

	zhResponse := responseForLanguage("zh-CN")
	enResponse := responseForLanguage("en")
	if zhResponse["error"] != "实体编码已存在" {
		t.Fatalf("unexpected Chinese response: %#v", zhResponse)
	}
	if enResponse["error"] != "Entity code already exists" {
		t.Fatalf("unexpected English response: %#v", enResponse)
	}
	if zhResponse["error_code"] != enResponse["error_code"] {
		t.Fatalf("localized responses use different codes: zh=%#v en=%#v", zhResponse, enResponse)
	}
}

func TestLocalizedErrorResponsesUseStableCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Accept-Language", "en")

	tests := []struct {
		name string
		got  gin.H
		code string
	}{
		{name: "invalid request", got: invalidParamsResponse(c), code: "invalid_request"},
		{name: "operation", got: operationFailedResponse(c), code: "model_operation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got["error_code"] != tt.code || tt.got["error"] == "" {
				t.Fatalf("unexpected response: %#v", tt.got)
			}
		})
	}
}
