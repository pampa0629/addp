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
	errorCode := ""
	response := gin.H{}
	var referencedError *service.StandardResourceReferencedError
	switch {
	case errors.As(err, &referencedError):
		status = http.StatusConflict
		message = commoni18n.T(c, sysi18n.MsgStandardResourceReferenced)
		errorCode = "standard_resource_referenced"
		useGenericMessage = false
		if referencedError.Impact != nil {
			response["reference_count"] = referencedError.Impact.ReferenceCount
			response["reference_summary"] = referencedError.Impact.Summary
			response["reference_sample"] = referencedError.Impact.Sample
			response["reference_sample_truncated"] = referencedError.Impact.SampleTruncated
		}
	case errors.Is(err, service.ErrModelReferenceGuardUnavailable):
		status = http.StatusServiceUnavailable
		message = commoni18n.T(c, sysi18n.MsgModelReferenceGuardUnavailable)
		errorCode = "model_reference_guard_unavailable"
		useGenericMessage = false
	case errors.Is(err, repository.ErrMetricDependencyCycle):
		message = commoni18n.T(c, sysi18n.MsgMetricDependencyCycle)
	case errors.Is(err, service.ErrInvalidHierarchyLevelNumber):
		message = commoni18n.T(c, sysi18n.MsgInvalidHierarchyLevelNumber)
	case errors.Is(err, service.ErrDomainParentCycle):
		message = commoni18n.T(c, sysi18n.MsgDomainParentCycle)
	case errors.Is(err, service.ErrDomainReferenced):
		message = commoni18n.T(c, sysi18n.MsgDomainReferenced)
	case errors.Is(err, service.ErrClassificationReferenced):
		message = commoni18n.T(c, sysi18n.MsgClassificationReferenced)
	case errors.Is(err, service.ErrMetricCategoryReferenced):
		message = commoni18n.T(c, sysi18n.MsgMetricCategoryReferenced)
	case errors.Is(err, service.ErrMeasurementCategoryReferenced):
		message = commoni18n.T(c, sysi18n.MsgMeasurementCategoryReferenced)
	case errors.Is(err, service.ErrUnitReferenced):
		message = commoni18n.T(c, sysi18n.MsgUnitReferenced)
	case errors.Is(err, service.ErrCodeSetReferenced):
		message = commoni18n.T(c, sysi18n.MsgCodeSetReferenced)
	case errors.Is(err, service.ErrCodeItemReferenced):
		message = commoni18n.T(c, sysi18n.MsgCodeItemReferenced)
	case errors.Is(err, service.ErrMetricReferenced):
		message = commoni18n.T(c, sysi18n.MsgMetricReferenced)
	case errors.Is(err, service.ErrClassificationParentCycle):
		message = commoni18n.T(c, sysi18n.MsgClassificationParentCycle)
	case errors.Is(err, service.ErrMetricCategoryParentCycle):
		message = commoni18n.T(c, sysi18n.MsgMetricCategoryParentCycle)
	case errors.Is(err, service.ErrSystemCategoryImmutable):
		message = commoni18n.T(c, sysi18n.MsgSystemCategoryImmutable)
	case errors.Is(err, service.ErrSystemUnitImmutable):
		message = commoni18n.T(c, sysi18n.MsgSystemUnitImmutable)
	case errors.Is(err, repository.ErrInvalidTenantReference):
		message = commoni18n.T(c, sysi18n.MsgInvalidResourceReference)
	case errors.Is(err, service.ErrInvalidStandardRevision):
		message = commoni18n.T(c, sysi18n.MsgInvalidStandardRevision)
		useGenericMessage = false
	case errors.Is(err, service.ErrInvalidRevisionTransition):
		status = http.StatusConflict
		message = commoni18n.T(c, sysi18n.MsgInvalidRevisionTransition)
		useGenericMessage = false
	case errors.Is(err, service.ErrEffectiveIntervalConflict):
		status = http.StatusConflict
		message = commoni18n.T(c, sysi18n.MsgEffectiveIntervalConflict)
		useGenericMessage = false
	case errors.Is(err, service.ErrDraftRevisionExists):
		status = http.StatusConflict
		message = commoni18n.T(c, sysi18n.MsgDraftRevisionExists)
		useGenericMessage = false
	case errors.Is(err, service.ErrPublishedRevisionRequired):
		message = commoni18n.T(c, sysi18n.MsgPublishedRevisionRequired)
		useGenericMessage = false
	case errors.Is(err, service.ErrPlatformCodeSetImmutable):
		status = http.StatusConflict
		message = commoni18n.T(c, sysi18n.MsgPlatformCodeSetImmutable)
		useGenericMessage = false
	case errors.Is(err, repository.ErrVersionConflict):
		status = http.StatusConflict
		message = commoni18n.T(c, sysi18n.MsgVersionConflict)
		useGenericMessage = false
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
	response["error"] = message
	if errorCode != "" {
		response["error_code"] = errorCode
	}
	c.JSON(status, response)
}

func respondDocumentFileError(c *gin.Context, err error) {
	if errors.Is(err, repository.ErrVersionConflict) {
		respondError(c, http.StatusConflict, err)
		return
	}
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
