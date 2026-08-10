package api

import (
	"errors"
	"net/http"

	commonAPI "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// respondQualityServiceError maps domain and persistence errors to the stable
// public error contract without exposing SQL, connection, or dependency data.
func respondQualityServiceError(c *gin.Context, err error, notFoundMessage, operationMessage string) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := commoni18n.T(c, operationMessage)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, commonAPI.ErrNotFound):
		status = http.StatusNotFound
		code = "resource_not_found"
		if notFoundMessage != "" {
			message = commoni18n.T(c, notFoundMessage)
		}
	case errors.Is(err, commonAPI.ErrBadRequest):
		status = http.StatusBadRequest
		code = "invalid_request"
		message = commoni18n.T(c, qualityi18n.MsgInvalidRequest)
	case errors.Is(err, commonAPI.ErrConflict):
		status = http.StatusConflict
		code = "resource_conflict"
		message = commoni18n.T(c, qualityi18n.MsgConflict)
	}
	respondQualityError(c, status, code, message)
}

type qualityErrorResponse struct {
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

type qualityMessageResponse struct {
	Message string `json:"message"`
}

func respondQualityError(c *gin.Context, status int, code, message string) {
	c.JSON(status, qualityErrorResponse{Error: message, ErrorCode: code})
}

func respondInvalidRequest(c *gin.Context, detail string) {
	message := commoni18n.T(c, qualityi18n.MsgInvalidRequest)
	if detail != "" {
		message = commoni18n.TWithDetail(c, qualityi18n.MsgInvalidRequest, detail)
	}
	respondQualityError(c, http.StatusBadRequest, "invalid_request", message)
}
