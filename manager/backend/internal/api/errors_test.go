package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestEngineUnavailableUsesStableTransientErrorContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("addp_lang", commoni18n.LangZhCN)

	engineUnavailable(c)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error_code"] != "engine_unavailable" || body["error_type"] != "transient" || body["error"] == "" {
		t.Fatalf("response = %#v", body)
	}
}

func TestProtectionRequiredUsesStableFailClosedErrorContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("addp_lang", commoni18n.LangZhCN)

	protectionRequired(c)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error_code"] != "security_protection_required" || body["error"] == "" {
		t.Fatalf("response = %#v", body)
	}
}

func TestDataProfileProtectionErrorUsesStableFailClosedContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("addp_lang", commoni18n.LangZhCN)

	handleDataProfileError(c, service.ErrDataProfileProtectionRequired, manageri18n.MsgDataProfileQueryFailed)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error_code"] != "security_protection_required" || body["error"] == "" {
		t.Fatalf("response = %#v", body)
	}
}

func TestManagerErrorMessagesRegistered(t *testing.T) {
	c := &gin.Context{}
	c.Set("addp_lang", commoni18n.LangZhCN)

	messageIDs := []string{
		manageri18n.MsgInvalidEngineID,
		manageri18n.MsgInvalidEngineIDParam,
		manageri18n.MsgMissingLocator,
		manageri18n.MsgEngineAccessDenied,
		manageri18n.MsgEngineUnavailable,
		manageri18n.MsgMetaScanRequired,
		manageri18n.MsgProtectionRequired,
		manageri18n.MsgPreviewFailed,
		manageri18n.MsgMissingEngineIDOrStorageRef,
		manageri18n.MsgSearchKeywordTooShort,
		manageri18n.MsgSchemaRequired,
		manageri18n.MsgTableRequired,
		manageri18n.MsgSchemaAndTableRequired,
		manageri18n.MsgSystemClientUnavailable,
		manageri18n.MsgSystemClientNotInitialized,
		manageri18n.MsgEngineNotFound,
		manageri18n.MsgFeatureNotFound,
		manageri18n.MsgFeatureInvalidGeometry,
		manageri18n.MsgInvalidZParam,
		manageri18n.MsgInvalidXParam,
		manageri18n.MsgInvalidYParam,
		manageri18n.MsgInvalidSRIDParam,
		manageri18n.MsgInvalidGeoJSON,
		manageri18n.MsgQueryFailed,
		manageri18n.MsgQueryCountFailed,
		manageri18n.MsgQueryExtentFailed,
		manageri18n.MsgParseFormFailed,
		manageri18n.MsgFileRequired,
		manageri18n.MsgReadFileFailed,
		manageri18n.MsgLocatorEngineMismatch,
		manageri18n.MsgItemRefreshRequiresLocator,
		manageri18n.MsgItemRefreshTargetRequired,
		manageri18n.MsgMissingParam,
		manageri18n.MsgInvalidParam,
		manageri18n.MsgHybridSearchNotConfigured,
		manageri18n.MsgHybridSearchFailed,
		manageri18n.MsgMissingQuery,
		manageri18n.MsgSearchHistoryUnavailable,
		manageri18n.MsgUnauthorized,
		manageri18n.MsgLoadHistoryFailed,
		manageri18n.MsgInvalidHistoryID,
		manageri18n.MsgDeleteHistoryFailed,
		manageri18n.MsgClearHistoryFailed,
		manageri18n.MsgInvalidRequestBody,
		manageri18n.MsgQuickViewRecordNotFound,
		manageri18n.MsgQuickViewInvalidMode,
		manageri18n.MsgQuickViewGeometryMissing,
		manageri18n.MsgImportTableNameRequired,
		manageri18n.MsgImportZipRequired,
		manageri18n.MsgImportUnsupportedFormat,
		manageri18n.MsgImportZipMissingShp,
		manageri18n.MsgImportZipBasenameMismatch,
		manageri18n.MsgImportZipMissingRequired,
		manageri18n.MsgInvalidModel3DTilesResultID,
		manageri18n.MsgModel3DTilesResultNotFound,
		manageri18n.MsgDeleteModel3DTilesFailed,
		manageri18n.MsgModel3DTilesResultDeleted,
		manageri18n.MsgExistingResultActionRequired,
		manageri18n.MsgEmbeddingConfigurationLoadFailed,
		manageri18n.MsgEmbeddingConfigurationUpdateFailed,
		manageri18n.MsgEmbeddingConfigurationVersionConflict,
		manageri18n.MsgPPTXPDFServiceUnavailable,
		manageri18n.MsgInvalidPPTXPDFResultID,
		manageri18n.MsgPPTXPDFResultNotReady,
		manageri18n.MsgPPTXPDFObjectNotFound,
		manageri18n.MsgPPTXPDFResolveFailed,
		manageri18n.MsgPPTXPDFExecutionFailed,
		manageri18n.MsgInvalidPPTXPDFTaskID,
		manageri18n.MsgPPTXPDFTaskNotFound,
		manageri18n.MsgPPTXPDFResultNotFound,
		manageri18n.MsgPPTXPDFDeleteFailed,
		manageri18n.MsgPPTXPDFTaskDeleted,
		manageri18n.MsgPPTXPDFResultDeleted,
	}

	for _, messageID := range messageIDs {
		if got := commoni18n.T(c, messageID); got == messageID || got == "" {
			t.Fatalf("message %s is not registered, got %q", messageID, got)
		}
	}
}
