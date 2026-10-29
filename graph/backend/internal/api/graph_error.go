package api

import (
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	graphi18n "github.com/addp/graph/i18n"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
)

func graphOperationError(c *gin.Context, err error) {
	status, key := http.StatusInternalServerError, graphi18n.MsgGraphOperationFailed
	code := ""
	switch {
	case errors.Is(err, commonapi.ErrForbidden):
		status, key = 403, graphi18n.MsgUnauthorized
	case errors.Is(err, repository.ErrGraphVersionConflict):
		status, key, code = 409, graphi18n.MsgVersionConflict, "resource_version_conflict"
	case errors.Is(err, repository.ErrGraphPhysicalTargetUnverified):
		status, key, code = 409, graphi18n.MsgPhysicalTargetUnverified, "graph_physical_target_unverified"
	case errors.Is(err, repository.ErrGraphReconciliationIncomplete):
		status, key, code = 409, graphi18n.MsgReconciliationIncomplete, "graph_reconciliation_incomplete"
	case errors.Is(err, repository.ErrGraphAdmissionClosed):
		status, key, code = 409, graphi18n.MsgAdmissionClosed, "graph_admission_closed"
	case errors.Is(err, commonapi.ErrConflict):
		status, key, code = 409, graphi18n.MsgTaskActive, "graph_operation_conflict"
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
	body := gin.H{"error": commoni18n.T(c, key)}
	if code != "" {
		body["error_code"] = code
	}
	c.JSON(status, body)
}
