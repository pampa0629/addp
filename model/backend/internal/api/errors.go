package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/gin-gonic/gin"
)

func localizedErrorResponse(c *gin.Context, messageID, code string) gin.H {
	return errorResponseWithCode(commoni18n.T(c, messageID), code)
}

func invalidParamsResponse(c *gin.Context) gin.H {
	return localizedErrorResponse(c, commoni18n.MsgInvalidParams, "invalid_request")
}

func operationFailedResponse(c *gin.Context) gin.H {
	return localizedErrorResponse(c, modeli18n.MsgOperationFailed, "model_operation_failed")
}

func errorResponseWithCode(message, code string) gin.H {
	return gin.H{
		"error":      message,
		"error_code": code,
	}
}

// serviceErrorResponse is the single API boundary for service-layer errors.
// Domain errors carry only stable codes and i18n IDs; raw service/database text is never exposed.
func serviceErrorResponse(c *gin.Context, err error) (int, gin.H) {
	if domainErr, ok := apperrors.As(err); ok {
		messageID := domainErr.MessageID
		if messageID == "" {
			messageID = modeli18n.MsgOperationFailed
		}
		status := http.StatusInternalServerError
		switch domainErr.Kind {
		case apperrors.KindValidation:
			status = http.StatusBadRequest
		case apperrors.KindNotFound:
			status = http.StatusNotFound
		case apperrors.KindConflict:
			status = http.StatusConflict
		case apperrors.KindForbidden:
			status = http.StatusForbidden
		case apperrors.KindUnavailable:
			status = http.StatusServiceUnavailable
		}
		return status, localizedErrorResponse(c, messageID, domainErr.Code)
	}
	if errors.Is(err, commonapi.ErrNotFound) {
		return http.StatusNotFound, localizedErrorResponse(c, modeli18n.MsgResourceNotFound, "resource_not_found")
	}
	if errors.Is(err, commonapi.ErrConflict) {
		return http.StatusConflict, localizedErrorResponse(c, modeli18n.MsgResourceConflict, "resource_conflict")
	}
	return http.StatusInternalServerError, operationFailedResponse(c)
}

func writeServiceError(c *gin.Context, err error) {
	status, response := serviceErrorResponse(c, err)
	c.JSON(status, response)
}
