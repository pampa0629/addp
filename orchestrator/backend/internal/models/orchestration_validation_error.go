package models

import "fmt"

type StepDecodeErrorCode string

const StepDecodeUnsupportedField StepDecodeErrorCode = "unsupported_field"

// StepDecodeError identifies a stable orchestration Step JSON decoding failure.
type StepDecodeError struct {
	Code  StepDecodeErrorCode
	Field string
}

func (e *StepDecodeError) Error() string {
	return fmt.Sprintf("unsupported orchestration step field %q", e.Field)
}

type StepValidationCode string

const (
	StepValidationStepsRequired             StepValidationCode = "steps_required"
	StepValidationIDRequired                StepValidationCode = "step_id_required"
	StepValidationDuplicateID               StepValidationCode = "duplicate_step_id"
	StepValidationNameRequired              StepValidationCode = "step_name_required"
	StepValidationProviderRequired          StepValidationCode = "step_provider_required"
	StepValidationTaskTypeRequired          StepValidationCode = "step_task_type_required"
	StepValidationTaskIDRequired            StepValidationCode = "step_task_id_required"
	StepValidationDependencyEmpty           StepValidationCode = "dependency_empty"
	StepValidationDependencyUnknown         StepValidationCode = "dependency_unknown"
	StepValidationTemplateUnknownStep       StepValidationCode = "template_unknown_step"
	StepValidationTemplateSelfReference     StepValidationCode = "template_self_reference"
	StepValidationTemplateMissingDependency StepValidationCode = "template_missing_dependency"
	StepValidationCircularDependency        StepValidationCode = "circular_dependency"
)

// StepValidationError carries stable Step definition context to the API layer.
type StepValidationError struct {
	Code      StepValidationCode
	StepIndex int
	StepID    string
	Reference string
}

func (e *StepValidationError) Error() string {
	switch e.Code {
	case StepValidationStepsRequired:
		return "steps is required"
	case StepValidationIDRequired:
		return fmt.Sprintf("steps[%d].id is required", e.StepIndex)
	case StepValidationDuplicateID:
		return fmt.Sprintf("duplicate step id %q", e.StepID)
	case StepValidationNameRequired:
		return fmt.Sprintf("steps[%d].name is required", e.StepIndex)
	case StepValidationProviderRequired:
		return fmt.Sprintf("steps[%d].provider is required", e.StepIndex)
	case StepValidationTaskTypeRequired:
		return fmt.Sprintf("steps[%d].task_type is required", e.StepIndex)
	case StepValidationTaskIDRequired:
		return fmt.Sprintf("steps[%d].task_id is required", e.StepIndex)
	case StepValidationDependencyEmpty:
		return fmt.Sprintf("steps[%d].depends_on contains empty step id", e.StepIndex)
	case StepValidationDependencyUnknown:
		return fmt.Sprintf("steps[%d].depends_on references unknown step %q", e.StepIndex, e.Reference)
	case StepValidationTemplateUnknownStep:
		return fmt.Sprintf("steps[%d].parameters references unknown step %q", e.StepIndex, e.Reference)
	case StepValidationTemplateSelfReference:
		return fmt.Sprintf("steps[%d].parameters cannot reference itself", e.StepIndex)
	case StepValidationTemplateMissingDependency:
		return fmt.Sprintf("steps[%d].parameters references step %q but depends_on does not include it", e.StepIndex, e.Reference)
	case StepValidationCircularDependency:
		return fmt.Sprintf("steps contains circular dependency at %q", e.StepID)
	default:
		return "orchestration steps are invalid"
	}
}
