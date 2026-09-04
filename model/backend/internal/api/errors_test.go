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
		{name: "unavailable", err: apperrors.Unavailable("standard_service_unavailable", i18n.MsgStandardUnavailable), status: http.StatusServiceUnavailable, code: "standard_service_unavailable"},
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

func TestUniqueConflictResponsesAreLocalizedWithStableCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		code      string
		messageID string
		zh        string
		en        string
	}{
		{code: "entity_attribute_column_conflict", messageID: i18n.MsgAttributeColumnConflict, zh: "实体属性列名已存在", en: "Entity attribute column name already exists"},
		{code: "logical_field_column_conflict", messageID: i18n.MsgFieldColumnConflict, zh: "逻辑表字段列名已存在", en: "Logical table field column name already exists"},
		{code: "entity_relation_conflict", messageID: i18n.MsgRelationConflict, zh: "相同的实体关系已存在", en: "The same entity relation already exists"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			responseForLanguage := func(language string) (int, gin.H) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("GET", "/", nil)
				c.Request.Header.Set("Accept-Language", language)
				commoni18n.I18nMiddleware()(c)
				return serviceErrorResponse(c, apperrors.Conflict(tt.code, tt.messageID))
			}

			zhStatus, zhResponse := responseForLanguage("zh-CN")
			enStatus, enResponse := responseForLanguage("en")
			if zhStatus != http.StatusConflict || enStatus != http.StatusConflict ||
				zhResponse["error_code"] != tt.code || enResponse["error_code"] != tt.code ||
				zhResponse["error"] != tt.zh || enResponse["error"] != tt.en {
				t.Fatalf("unexpected localized conflict responses: zh=%d %#v en=%d %#v", zhStatus, zhResponse, enStatus, enResponse)
			}
		})
	}
}

func TestLogicalTableDeleteConflictsAreLocalizedWithStableCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		code      string
		messageID string
		zh        string
		en        string
	}{
		{code: "materialization_group_member_conflict", messageID: i18n.MsgTableMaterializationGroupMember, zh: "逻辑表仍属于物化组，请先将其移出物化组", en: "The logical table still belongs to a materialization group; remove it from the group first"},
		{code: "logical_table_materialization_configured", messageID: i18n.MsgTableMaterializationConfigured, zh: "逻辑表仍保留物化目标配置，请先显式清空配置", en: "The logical table still has a materialization target configuration; clear it explicitly first"},
		{code: "logical_table_materialization_batch_active", messageID: i18n.MsgTableMaterializationBatchActive, zh: "逻辑表仍有未终结的物化批次，不能删除", en: "The logical table still has a non-terminal materialization batch and cannot be deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			responseForLanguage := func(language string) (int, gin.H) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("DELETE", "/api/v1/model/logical-tables/1", nil)
				c.Request.Header.Set("Accept-Language", language)
				commoni18n.I18nMiddleware()(c)
				return serviceErrorResponse(c, apperrors.Conflict(tt.code, tt.messageID))
			}

			zhStatus, zhResponse := responseForLanguage("zh-CN")
			enStatus, enResponse := responseForLanguage("en")
			if zhStatus != http.StatusConflict || enStatus != http.StatusConflict ||
				zhResponse["error_code"] != tt.code || enResponse["error_code"] != tt.code ||
				zhResponse["error"] != tt.zh || enResponse["error"] != tt.en {
				t.Fatalf("unexpected localized delete conflict responses: zh=%d %#v en=%d %#v", zhStatus, zhResponse, enStatus, enResponse)
			}
		})
	}
}

func TestApprovalValidationResponsesAreLocalizedWithStableCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		code      string
		messageID string
		zh        string
		en        string
	}{
		{code: "entity_approval_attributes_required", messageID: i18n.MsgEntityAttributesRequired, zh: "实体至少需要一个属性才能审批", en: "The entity must have at least one attribute before approval"},
		{code: "entity_approval_primary_key_required", messageID: i18n.MsgEntityPrimaryKeyRequired, zh: "实体至少需要一个主键属性才能审批", en: "The entity must have at least one primary key attribute before approval"},
		{code: "logical_table_approval_fields_required", messageID: i18n.MsgTableFieldsRequired, zh: "逻辑表至少需要一个字段才能审批", en: "The logical table must have at least one field before approval"},
		{code: "logical_table_approval_primary_key_required", messageID: i18n.MsgTablePrimaryKeyRequired, zh: "逻辑表至少需要一个主键字段才能审批", en: "The logical table must have at least one primary key field before approval"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			responseForLanguage := func(language string) (int, gin.H) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("GET", "/", nil)
				c.Request.Header.Set("Accept-Language", language)
				commoni18n.I18nMiddleware()(c)
				return serviceErrorResponse(c, apperrors.Validation(tt.code, tt.messageID))
			}

			zhStatus, zhResponse := responseForLanguage("zh-CN")
			enStatus, enResponse := responseForLanguage("en")
			if zhStatus != http.StatusBadRequest || enStatus != http.StatusBadRequest ||
				zhResponse["error_code"] != tt.code || enResponse["error_code"] != tt.code ||
				zhResponse["error"] != tt.zh || enResponse["error"] != tt.en {
				t.Fatalf("unexpected localized validation responses: zh=%d %#v en=%d %#v", zhStatus, zhResponse, enStatus, enResponse)
			}
		})
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
