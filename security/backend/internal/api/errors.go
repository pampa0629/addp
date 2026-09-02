package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	securityi18n "github.com/addp/security/i18n"
	"github.com/addp/security/internal/repository"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

var errBadRequest = commonapi.ErrBadRequest

func respondError(c *gin.Context, err error) {
	status := commonapi.MapErrorToHTTPStatus(err)
	key := securityi18n.MsgOperationFailed
	switch {
	case errors.Is(err, repository.ErrVersionConflict):
		status, key = http.StatusConflict, securityi18n.MsgVersionConflict
	case errors.Is(err, commonapi.ErrBadRequest):
		status, key = http.StatusBadRequest, securityi18n.MsgInvalidRequest
	case errors.Is(err, commonapi.ErrNotFound):
		status, key = http.StatusNotFound, securityi18n.MsgNotFound
	case errors.Is(err, commonapi.ErrConflict):
		status, key = http.StatusConflict, securityi18n.MsgConflict
	case errors.Is(err, service.ErrProjectionCursorConflict):
		status, key = http.StatusConflict, securityi18n.MsgProjectionCursorConflict
	case errors.Is(err, service.ErrNoSupportedFindingsReleaseUnavailable):
		status, key = http.StatusConflict, securityi18n.MsgNoSupportedFindingsReleaseUnavailable
	}
	response := gin.H{"error": commoni18n.T(c, key)}
	if errors.Is(err, service.ErrProjectionCursorConflict) {
		response["error_code"] = "protection_projection_cursor_conflict"
	}
	if errors.Is(err, service.ErrNoSupportedFindingsReleaseUnavailable) {
		response["error_code"] = "no_supported_findings_release_unavailable"
	}
	c.JSON(status, response)
}
