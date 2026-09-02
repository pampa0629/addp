package api

import (
	"net/http"

	commonAPI "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/gin-gonic/gin"
)

func managerError(c *gin.Context, status int, messageID string) {
	commonAPI.ErrorResponse(c, status, commoni18n.T(c, messageID))
}

func managerErrorWithDetail(c *gin.Context, status int, messageID, detail string) {
	commonAPI.ErrorResponse(c, status, commoni18n.TWithDetail(c, messageID, detail))
}

func invalidEngineID(c *gin.Context) {
	managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidEngineID)
}

func missingLocator(c *gin.Context) {
	managerError(c, http.StatusBadRequest, manageri18n.MsgMissingLocator)
}

func accessDeniedToEngine(c *gin.Context) {
	managerError(c, http.StatusForbidden, manageri18n.MsgEngineAccessDenied)
}

func engineUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error":      commoni18n.T(c, manageri18n.MsgEngineUnavailable),
		"error_code": "engine_unavailable",
		"error_type": "transient",
	})
}

func protectionRequired(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{
		"error":      commoni18n.T(c, manageri18n.MsgProtectionRequired),
		"error_code": "security_protection_required",
	})
}
