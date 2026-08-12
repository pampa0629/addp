package api

import (
	"errors"
	"net/http"

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
	message := commoni18n.T(c, sysi18n.MsgOperationFailed)
	useGenericMessage := status >= 500
	switch {
	case errors.Is(err, repository.ErrMetricDependencyCycle):
		message = commoni18n.T(c, sysi18n.MsgMetricDependencyCycle)
	case errors.Is(err, service.ErrInvalidHierarchyLevelNumber):
		message = commoni18n.T(c, sysi18n.MsgInvalidHierarchyLevelNumber)
	case errors.Is(err, service.ErrInvalidCodeSetType):
		message = commoni18n.T(c, sysi18n.MsgInvalidCodeSetType)
	case errors.Is(err, service.ErrDomainParentCycle):
		message = commoni18n.T(c, sysi18n.MsgDomainParentCycle)
	case errors.Is(err, service.ErrClassificationParentCycle):
		message = commoni18n.T(c, sysi18n.MsgClassificationParentCycle)
	case errors.Is(err, service.ErrMetricCategoryParentCycle):
		message = commoni18n.T(c, sysi18n.MsgMetricCategoryParentCycle)
	case errors.Is(err, service.ErrSystemCategoryImmutable):
		message = commoni18n.T(c, sysi18n.MsgSystemCategoryImmutable)
	case errors.Is(err, service.ErrSystemUnitImmutable):
		message = commoni18n.T(c, sysi18n.MsgSystemUnitImmutable)
	case errors.Is(err, service.ErrSystemCodeSetImmutable):
		message = commoni18n.T(c, sysi18n.MsgSystemCodeSetImmutable)
	case errors.Is(err, repository.ErrInvalidTenantReference):
		message = commoni18n.T(c, sysi18n.MsgInvalidResourceReference)
	case errors.Is(err, commonapi.ErrNotFound):
		message = commoni18n.T(c, sysi18n.MsgResourceNotFound)
	case errors.Is(err, commonapi.ErrConflict):
		message = commoni18n.T(c, sysi18n.MsgResourceConflict)
	case errors.Is(err, service.ErrDocumentStorageUnavailable):
		message = commoni18n.T(c, sysi18n.MsgDocumentStorageUnavailable)
		useGenericMessage = false
	case errors.Is(err, service.ErrDocumentFileTooLarge):
		message = commoni18n.T(c, sysi18n.MsgDocumentFileTooLarge)
		useGenericMessage = false
	case errors.Is(err, service.ErrDocumentFileUpload):
		message = commoni18n.T(c, sysi18n.MsgDocumentFileUploadFailed)
		useGenericMessage = false
	case errors.Is(err, service.ErrDocumentFileNameInvalid):
		message = commoni18n.T(c, sysi18n.MsgDocumentFileNameInvalid)
		useGenericMessage = false
	case errors.Is(err, service.ErrDocumentFileDownload):
		message = commoni18n.T(c, sysi18n.MsgDocumentFileDownloadFailed)
		useGenericMessage = false
	case errors.Is(err, service.ErrDocumentFileCleanup):
		message = commoni18n.T(c, sysi18n.MsgDocumentFileCleanupFailed)
		useGenericMessage = false
	case status == http.StatusBadRequest:
		message = commoni18n.T(c, sysi18n.MsgInvalidParams)
	case status == http.StatusNotFound:
		message = commoni18n.T(c, sysi18n.MsgResourceNotFound)
	case status == http.StatusConflict:
		message = commoni18n.T(c, sysi18n.MsgResourceConflict)
	}
	if useGenericMessage {
		message = commoni18n.T(c, sysi18n.MsgOperationFailed)
	}
	c.JSON(status, gin.H{"error": message})
}

func respondDocumentFileError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, service.ErrDocumentFileTooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, service.ErrDocumentStorageUnavailable):
		status = http.StatusServiceUnavailable
	case errors.Is(err, commonapi.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, service.ErrDocumentFileUpload):
		status = http.StatusBadGateway
	case errors.Is(err, service.ErrDocumentFileNameInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, service.ErrDocumentFileDownload):
		status = http.StatusBadGateway
	}
	respondError(c, status, err)
}
