package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/taskprovider"
	orchestratori18n "github.com/addp/orchestrator/i18n"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
)

func bindOrchestrationRequest(c *gin.Context, req interface{}) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(req); err != nil {
		respondOrchestrationJSONError(c, err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		respondOrchestrationJSONError(c, err)
		return false
	}
	return true
}

func (h *OrchestrationHandler) validateOrchestrationDefinition(c *gin.Context, req *models.Orchestration) bool {
	if err := models.ValidateSteps(req.Steps); err != nil {
		respondOrchestrationValidationError(c, err)
		return false
	}
	if err := h.taskProviderResolver.ValidateStepTaskReferences(c.Request.Context(), req.TenantID, req.Steps); err != nil {
		respondOrchestrationValidationError(c, err)
		return false
	}
	if err := service.ValidateNoRecursiveOrchestrationReferences(h.orchRepo, req.ID, req.TenantID, req.Steps); err != nil {
		respondOrchestrationValidationError(c, err)
		return false
	}
	if err := service.ApplyOrchestrationSchedule(req, time.Now()); err != nil {
		respondOrchestrationValidationError(c, err)
		return false
	}
	return true
}

func respondOrchestrationJSONError(c *gin.Context, err error) {
	var stepDecodeErr *models.StepDecodeError
	if errors.As(err, &stepDecodeErr) {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.TWithData(c, orchestratori18n.MsgUnsupportedStepField, map[string]interface{}{
			"Field": stepDecodeErr.Field,
		})})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, orchestratori18n.MsgInvalidOrchestrationJSON)})
}

func respondOrchestrationValidationError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	var referenceErr *service.OrchestrationReferenceValidationError
	if errors.As(err, &referenceErr) && referenceErr.Code == service.OrchestrationReferenceLookupUnavailable {
		status = http.StatusInternalServerError
	}
	c.JSON(status, gin.H{"error": localizeOrchestrationValidationError(c, err)})
}

func localizeOrchestrationValidationError(c *gin.Context, err error) string {
	var stepErr *models.StepValidationError
	if errors.As(err, &stepErr) {
		return localizeStepValidationError(c, stepErr)
	}

	var stepTaskErr *service.StepTaskValidationError
	if errors.As(err, &stepTaskErr) {
		return localizeStepTaskValidationError(c, stepTaskErr)
	}

	var referenceErr *service.OrchestrationReferenceValidationError
	if errors.As(err, &referenceErr) {
		return localizeOrchestrationReferenceValidationError(c, referenceErr)
	}

	var scheduleErr *service.ScheduleValidationError
	if errors.As(err, &scheduleErr) {
		return commoni18n.TWithData(c, orchestratori18n.MsgScheduleInvalid, map[string]interface{}{
			"Expression": scheduleErr.Expression,
		})
	}

	return commoni18n.T(c, commoni18n.MsgInvalidParams)
}

func localizeStepValidationError(c *gin.Context, err *models.StepValidationError) string {
	data := map[string]interface{}{
		"StepNumber": err.StepIndex + 1,
		"StepID":     err.StepID,
		"Reference":  err.Reference,
	}
	switch err.Code {
	case models.StepValidationStepsRequired:
		return commoni18n.T(c, orchestratori18n.MsgStepsRequired)
	case models.StepValidationIDRequired,
		models.StepValidationNameRequired,
		models.StepValidationProviderRequired,
		models.StepValidationTaskTypeRequired,
		models.StepValidationTaskIDRequired:
		data["Field"] = localizedStepField(c, err.Code)
		return commoni18n.TWithData(c, orchestratori18n.MsgStepFieldRequired, data)
	case models.StepValidationDuplicateID:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepDuplicateID, data)
	case models.StepValidationDependencyEmpty:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepDependencyEmpty, data)
	case models.StepValidationDependencyUnknown:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepDependencyUnknown, data)
	case models.StepValidationTemplateUnknownStep:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepTemplateUnknown, data)
	case models.StepValidationTemplateSelfReference:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepTemplateSelfReference, data)
	case models.StepValidationTemplateMissingDependency:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepTemplateMissingDependency, data)
	case models.StepValidationCircularDependency:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepCircularDependency, data)
	default:
		return commoni18n.T(c, commoni18n.MsgInvalidParams)
	}
}

func localizedStepField(c *gin.Context, code models.StepValidationCode) string {
	messageID := map[models.StepValidationCode]string{
		models.StepValidationIDRequired:       orchestratori18n.MsgStepFieldID,
		models.StepValidationNameRequired:     orchestratori18n.MsgStepFieldName,
		models.StepValidationProviderRequired: orchestratori18n.MsgStepFieldProvider,
		models.StepValidationTaskTypeRequired: orchestratori18n.MsgStepFieldTaskType,
		models.StepValidationTaskIDRequired:   orchestratori18n.MsgStepFieldTaskID,
	}[code]
	return commoni18n.T(c, messageID)
}

func localizeStepTaskValidationError(c *gin.Context, stepErr *service.StepTaskValidationError) string {
	data := map[string]interface{}{
		"StepNumber": stepErr.StepIndex + 1,
		"Provider":   stepErr.Provider,
		"TaskType":   stepErr.TaskType,
	}
	switch stepErr.Code {
	case service.StepTaskMissingReference:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepTaskMissingReference, data)
	case service.StepTaskProviderUnavailable:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepTaskProviderUnavailable, data)
	case service.StepTaskCapabilitiesInvalid:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepTaskCapabilitiesInvalid, data)
	case service.StepTaskTypeUndeclared:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepTaskTypeUndeclared, data)
	case service.StepTaskTypeDeprecated:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepTaskTypeDeprecated, data)
	case service.StepTaskParametersInvalid:
		return localizeParameterValidationError(c, stepErr, data)
	default:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterInvalid, data)
	}
}

func localizeParameterValidationError(c *gin.Context, stepErr *service.StepTaskValidationError, data map[string]interface{}) string {
	var bindingErr *service.OutputBindingValidationError
	if errors.As(stepErr.Cause, &bindingErr) {
		data["Path"] = bindingErr.ParameterPath
		data["SourceStep"] = bindingErr.StepID
		data["OutputPath"] = bindingErr.OutputPath
		data["SourceType"] = localizedParameterType(c, bindingErr.SourceType)
		data["TargetType"] = localizedParameterType(c, bindingErr.TargetType)
		switch bindingErr.Code {
		case service.OutputBindingInvalidFormat:
			return commoni18n.TWithData(c, orchestratori18n.MsgOutputBindingInvalidFormat, data)
		case service.OutputBindingUnknownStep:
			return commoni18n.TWithData(c, orchestratori18n.MsgOutputBindingUnknownStep, data)
		case service.OutputBindingUndeclared:
			return commoni18n.TWithData(c, orchestratori18n.MsgOutputBindingUndeclared, data)
		case service.OutputBindingUnknownTarget:
			return commoni18n.TWithData(c, orchestratori18n.MsgOutputBindingUnknownTarget, data)
		case service.OutputBindingTypeMismatch:
			return commoni18n.TWithData(c, orchestratori18n.MsgOutputBindingTypeMismatch, data)
		}
	}
	var parameterErr *taskprovider.ParameterValidationError
	if !errors.As(stepErr.Cause, &parameterErr) {
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterInvalid, data)
	}

	data["Path"] = parameterErr.Path
	data["Limit"] = parameterErr.Limit
	switch parameterErr.Rule {
	case taskprovider.ParameterRuleSchemaRequired, taskprovider.ParameterRuleSchemaType:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterSchemaInvalid, data)
	case taskprovider.ParameterRuleRequired:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterRequired, data)
	case taskprovider.ParameterRuleEnum:
		encoded, _ := json.Marshal(parameterErr.Allowed)
		data["Allowed"] = string(encoded)
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterEnum, data)
	case taskprovider.ParameterRuleType:
		data["Expected"] = localizedParameterType(c, parameterErr.Expected)
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterType, data)
	case taskprovider.ParameterRuleAdditionalProperty:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterAdditional, data)
	case taskprovider.ParameterRuleMinimum:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterMinimum, data)
	case taskprovider.ParameterRuleMaximum:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterMaximum, data)
	case taskprovider.ParameterRuleMinItems:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterMinItems, data)
	case taskprovider.ParameterRuleMaxItems:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterMaxItems, data)
	case taskprovider.ParameterRuleMinLength:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterMinLength, data)
	case taskprovider.ParameterRuleMaxLength:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterMaxLength, data)
	default:
		return commoni18n.TWithData(c, orchestratori18n.MsgStepParameterInvalid, data)
	}
}

func localizedParameterType(c *gin.Context, parameterType string) string {
	messageID := map[string]string{
		"object":  orchestratori18n.MsgParameterTypeObject,
		"array":   orchestratori18n.MsgParameterTypeArray,
		"string":  orchestratori18n.MsgParameterTypeString,
		"number":  orchestratori18n.MsgParameterTypeNumber,
		"integer": orchestratori18n.MsgParameterTypeInteger,
		"boolean": orchestratori18n.MsgParameterTypeBoolean,
		"null":    orchestratori18n.MsgParameterTypeNull,
	}[parameterType]
	if messageID == "" {
		return parameterType
	}
	return commoni18n.T(c, messageID)
}

func localizeOrchestrationReferenceValidationError(c *gin.Context, err *service.OrchestrationReferenceValidationError) string {
	data := map[string]interface{}{
		"OrchestrationID": err.OrchestrationID,
		"Path":            orchestrationReferencePath(err.Path),
	}
	switch err.Code {
	case service.OrchestrationReferenceSelf:
		return commoni18n.TWithData(c, orchestratori18n.MsgOrchestrationReferenceSelf, data)
	case service.OrchestrationReferenceCycle:
		return commoni18n.TWithData(c, orchestratori18n.MsgOrchestrationReferenceCycle, data)
	case service.OrchestrationReferenceNotFound:
		return commoni18n.TWithData(c, orchestratori18n.MsgOrchestrationReferenceNotFound, data)
	default:
		return commoni18n.T(c, orchestratori18n.MsgOrchestrationReferenceValidationFailed)
	}
}

func orchestrationReferencePath(path []uint) string {
	parts := make([]string, 0, len(path))
	for _, id := range path {
		if id > 0 {
			parts = append(parts, strconv.FormatUint(uint64(id), 10))
		}
	}
	return strings.Join(parts, " -> ")
}
