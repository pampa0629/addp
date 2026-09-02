package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	workbenchi18n "github.com/addp/workbench/i18n"
	"github.com/addp/workbench/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	applications *service.DataApplicationService
}

func NewHandler(applications *service.DataApplicationService) *Handler {
	return &Handler{applications: applications}
}

func actor(c *gin.Context) (int64, int64, bool) {
	tenantID, tenantOK := commonAuth.TenantIDFromGin(c)
	userID := int64(commonAuth.GetUserID(c))
	if !tenantOK || userID <= 0 {
		commonapi.RespondError(c, http.StatusForbidden, commoni18n.T(c, workbenchi18n.MsgInvalidRequest))
		return 0, 0, false
	}
	return tenantID, userID, true
}

func pagination(c *gin.Context) (int, int, bool) {
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	return page, pageSize, pageErr == nil && pageSizeErr == nil && page > 0 && pageSize > 0 && pageSize <= 100
}

func dataApplicationID(c *gin.Context) (string, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil || id == uuid.Nil {
		respondError(c, service.ErrInvalidDataApplication)
		return "", false
	}
	return id.String(), true
}

func descriptorRequest(c *gin.Context) service.DescriptorRequest {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	bearer := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	return service.DescriptorRequest{
		BearerToken: bearer, AcceptLanguage: c.GetHeader("Accept-Language"),
		RequestID: requestidmiddleware.FromGinContext(c),
	}
}

func respondError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	messageKey := workbenchi18n.MsgOperationFailed
	errorCode := ""
	switch {
	case errors.Is(err, service.ErrServiceAccessDenied):
		status, messageKey = http.StatusForbidden, workbenchi18n.MsgServiceAccessDenied
	case errors.Is(err, service.ErrServiceUnavailable):
		status, messageKey = http.StatusServiceUnavailable, workbenchi18n.MsgServiceUnavailable
	case errors.Is(err, service.ErrInvalidDataApplication):
		status, messageKey = http.StatusBadRequest, workbenchi18n.MsgInvalidDataApplication
	case errors.Is(err, service.ErrDataApplicationNotFound):
		status, messageKey = http.StatusNotFound, workbenchi18n.MsgDataApplicationNotFound
	case errors.Is(err, service.ErrDataApplicationVersionConflict):
		status, messageKey, errorCode = http.StatusConflict, workbenchi18n.MsgDataApplicationVersionConflict, "workbench_data_application_version_conflict"
	case errors.Is(err, service.ErrDataApplicationAlreadyPublished):
		status, messageKey, errorCode = http.StatusConflict, workbenchi18n.MsgDataApplicationAlreadyPublished, "workbench_data_application_already_published"
	case errors.Is(err, service.ErrDataApplicationNotPublished):
		status, messageKey, errorCode = http.StatusConflict, workbenchi18n.MsgDataApplicationNotPublished, "workbench_data_application_not_published"
	case errors.Is(err, service.ErrDataApplicationAccessDenied):
		status, messageKey, errorCode = http.StatusForbidden, workbenchi18n.MsgDataApplicationAccessDenied, "workbench_data_application_access_denied"
	case errors.Is(err, service.ErrInvalidResourceGrant):
		status, messageKey, errorCode = http.StatusBadRequest, workbenchi18n.MsgInvalidResourceGrant, "workbench_resource_grant_invalid"
	case errors.Is(err, service.ErrResourceGrantConflict):
		status, messageKey, errorCode = http.StatusConflict, workbenchi18n.MsgResourceGrantConflict, "workbench_resource_grant_conflict"
	}
	body := gin.H{"error": commoni18n.T(c, messageKey)}
	if errorCode != "" {
		body["error_code"] = errorCode
	}
	c.JSON(status, body)
}
