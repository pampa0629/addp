package exportartifact

import (
	"errors"
	"net/http"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

// WriteProtectionError is the single public contract for both export owners.
func WriteProtectionError(c *gin.Context, err error) bool {
	if !errors.Is(err, ErrProtectionChanged) {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{"error_code": "export_protection_changed", "error": commoni18n.T(c, commoni18n.MsgExportProtectionChanged)})
	return true
}
