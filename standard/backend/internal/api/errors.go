package api

import (
	"errors"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

func respondError(c *gin.Context, status int, err error) {
	if mapped := commonapi.MapErrorToHTTPStatus(err); mapped < 500 {
		status = mapped
	}
	message := err.Error()
	switch {
	case errors.Is(err, repository.ErrMetricDependencyCycle):
		message = commoni18n.T(c, sysi18n.MsgMetricDependencyCycle)
	case errors.Is(err, service.ErrInvalidHierarchyLevelNumber):
		message = commoni18n.T(c, sysi18n.MsgInvalidHierarchyLevelNumber)
	case errors.Is(err, commonapi.ErrNotFound):
		message = commoni18n.T(c, sysi18n.MsgResourceNotFound)
	case errors.Is(err, commonapi.ErrConflict):
		message = commoni18n.T(c, sysi18n.MsgResourceConflict)
	}
	if status >= 500 {
		message = commoni18n.T(c, sysi18n.MsgOperationFailed)
	}
	c.JSON(status, gin.H{"error": message})
}
