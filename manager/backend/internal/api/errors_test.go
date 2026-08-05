package api

import (
	"testing"

	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/gin-gonic/gin"
)

func TestManagerErrorMessagesRegistered(t *testing.T) {
	c := &gin.Context{}
	c.Set("addp_lang", commoni18n.LangZhCN)

	messageIDs := []string{
		manageri18n.MsgInvalidEngineID,
		manageri18n.MsgInvalidEngineIDParam,
		manageri18n.MsgMissingLocator,
		manageri18n.MsgEngineAccessDenied,
		manageri18n.MsgMetaScanRequired,
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
	}

	for _, messageID := range messageIDs {
		if got := commoni18n.T(c, messageID); got == messageID || got == "" {
			t.Fatalf("message %s is not registered, got %q", messageID, got)
		}
	}
}
