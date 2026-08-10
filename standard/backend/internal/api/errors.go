package api

import (
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	"github.com/gin-gonic/gin"
)

func respondError(c *gin.Context, status int, err error) {
	message := err.Error()
	if status >= 500 {
		message = commoni18n.T(c, sysi18n.MsgOperationFailed)
	}
	c.JSON(status, gin.H{"error": message})
}
