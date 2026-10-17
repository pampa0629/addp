package api

import (
	"errors"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	graphi18n "github.com/addp/graph/i18n"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
)

func graphOperationError(c *gin.Context, err error) {
	status, key := http.StatusInternalServerError, graphi18n.MsgGraphOperationFailed
	switch {
	case errors.Is(err, service.ErrGraphProtectionRequired):
		status, key = http.StatusForbidden, graphi18n.MsgProtectionRequired
	case errors.Is(err, service.ErrGraphTargetInvalid):
		status, key = http.StatusBadRequest, graphi18n.MsgTargetInvalid
	case errors.Is(err, gorm.ErrDuplicatedKey):
		status, key = http.StatusConflict, graphi18n.MsgTargetConflict
	case errors.Is(err, plugin.ErrGraphSearchIndexConflict):
		status, key = http.StatusConflict, graphi18n.MsgIndexConflict
	case errors.Is(err, plugin.ErrGraphSearchLimit):
		status, key = http.StatusBadRequest, graphi18n.MsgSearchLimit
	case errors.Is(err, plugin.ErrGraphTraversalLimit):
		status, key = http.StatusBadRequest, graphi18n.MsgTraversalLimit
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, key = http.StatusNotFound, graphi18n.MsgNotFound
	}
	_ = c.Error(err)
	c.JSON(status, gin.H{"error": commoni18n.T(c, key)})
}
