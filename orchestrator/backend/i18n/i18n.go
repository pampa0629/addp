package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

const (
	MsgInvalidOrchestrationJSON               = "orchestrator.error.invalid_orchestration_json"
	MsgTenantContextRequired                  = "orchestrator.error.tenant_context_required"
	MsgUnsupportedStepField                   = "orchestrator.error.unsupported_step_field"
	MsgStepsRequired                          = "orchestrator.error.steps_required"
	MsgStepFieldRequired                      = "orchestrator.error.step_field_required"
	MsgStepDuplicateID                        = "orchestrator.error.step_duplicate_id"
	MsgStepDependencyEmpty                    = "orchestrator.error.step_dependency_empty"
	MsgStepDependencyUnknown                  = "orchestrator.error.step_dependency_unknown"
	MsgStepTemplateUnknown                    = "orchestrator.error.step_template_unknown"
	MsgStepTemplateSelfReference              = "orchestrator.error.step_template_self_reference"
	MsgStepTemplateMissingDependency          = "orchestrator.error.step_template_missing_dependency"
	MsgStepCircularDependency                 = "orchestrator.error.step_circular_dependency"
	MsgOrchestrationReferenceSelf             = "orchestrator.error.orchestration_reference_self"
	MsgOrchestrationReferenceCycle            = "orchestrator.error.orchestration_reference_cycle"
	MsgOrchestrationReferenceNotFound         = "orchestrator.error.orchestration_reference_not_found"
	MsgOrchestrationReferenceValidationFailed = "orchestrator.error.orchestration_reference_validation_failed"
	MsgScheduleInvalid                        = "orchestrator.error.schedule_invalid"
	MsgStepFieldID                            = "orchestrator.step_field.id"
	MsgStepFieldName                          = "orchestrator.step_field.name"
	MsgStepFieldProvider                      = "orchestrator.step_field.provider"
	MsgStepFieldTaskType                      = "orchestrator.step_field.task_type"
	MsgStepFieldTaskID                        = "orchestrator.step_field.task_id"
	MsgStepTaskMissingReference               = "orchestrator.error.step_task_missing_reference"
	MsgStepTaskProviderUnavailable            = "orchestrator.error.step_task_provider_unavailable"
	MsgStepTaskCapabilitiesInvalid            = "orchestrator.error.step_task_capabilities_invalid"
	MsgStepTaskTypeUndeclared                 = "orchestrator.error.step_task_type_undeclared"
	MsgStepTaskTypeDeprecated                 = "orchestrator.error.step_task_type_deprecated"
	MsgStepParameterSchemaInvalid             = "orchestrator.error.step_parameter_schema_invalid"
	MsgStepParameterInvalid                   = "orchestrator.error.step_parameter_invalid"
	MsgStepParameterRequired                  = "orchestrator.error.step_parameter_required"
	MsgStepParameterEnum                      = "orchestrator.error.step_parameter_enum"
	MsgStepParameterType                      = "orchestrator.error.step_parameter_type"
	MsgStepParameterAdditional                = "orchestrator.error.step_parameter_additional"
	MsgStepParameterMinimum                   = "orchestrator.error.step_parameter_minimum"
	MsgStepParameterMaximum                   = "orchestrator.error.step_parameter_maximum"
	MsgStepParameterMinItems                  = "orchestrator.error.step_parameter_min_items"
	MsgStepParameterMaxItems                  = "orchestrator.error.step_parameter_max_items"
	MsgStepParameterMinLength                 = "orchestrator.error.step_parameter_min_length"
	MsgStepParameterMaxLength                 = "orchestrator.error.step_parameter_max_length"
	MsgParameterTypeObject                    = "orchestrator.parameter_type.object"
	MsgParameterTypeArray                     = "orchestrator.parameter_type.array"
	MsgParameterTypeString                    = "orchestrator.parameter_type.string"
	MsgParameterTypeNumber                    = "orchestrator.parameter_type.number"
	MsgParameterTypeInteger                   = "orchestrator.parameter_type.integer"
	MsgParameterTypeBoolean                   = "orchestrator.parameter_type.boolean"
	MsgParameterTypeNull                      = "orchestrator.parameter_type.null"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
