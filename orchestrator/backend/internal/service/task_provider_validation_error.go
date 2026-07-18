package service

import "fmt"

// StepTaskValidationCode identifies a stable TaskProvider Step validation failure.
type StepTaskValidationCode string

const (
	StepTaskMissingReference    StepTaskValidationCode = "missing_task_reference"
	StepTaskProviderUnavailable StepTaskValidationCode = "provider_unavailable"
	StepTaskCapabilitiesInvalid StepTaskValidationCode = "capabilities_invalid"
	StepTaskTypeUndeclared      StepTaskValidationCode = "task_type_undeclared"
	StepTaskTypeDeprecated      StepTaskValidationCode = "task_type_deprecated"
	StepTaskParametersInvalid   StepTaskValidationCode = "parameters_invalid"
)

// StepTaskValidationError carries stable Step context from the Service layer
// to the HTTP layer without coupling Service code to Gin or i18n.
type StepTaskValidationError struct {
	Code      StepTaskValidationCode
	StepIndex int
	Provider  string
	TaskType  string
	Cause     error
}

func (e *StepTaskValidationError) Error() string {
	switch e.Code {
	case StepTaskMissingReference:
		return fmt.Sprintf("steps[%d].provider and task_type are required", e.StepIndex)
	case StepTaskProviderUnavailable:
		return fmt.Sprintf("steps[%d] provider %q is not registered: %v", e.StepIndex, e.Provider, e.Cause)
	case StepTaskCapabilitiesInvalid:
		return fmt.Sprintf("steps[%d] provider %q capabilities invalid: %v", e.StepIndex, e.Provider, e.Cause)
	case StepTaskTypeUndeclared:
		return fmt.Sprintf("steps[%d] task_type %q is not declared by provider %q", e.StepIndex, e.TaskType, e.Provider)
	case StepTaskTypeDeprecated:
		return fmt.Sprintf("steps[%d] task_type %q of provider %q is deprecated", e.StepIndex, e.TaskType, e.Provider)
	case StepTaskParametersInvalid:
		return fmt.Sprintf("steps[%d] %v", e.StepIndex, e.Cause)
	default:
		return fmt.Sprintf("steps[%d] task reference is invalid", e.StepIndex)
	}
}

func (e *StepTaskValidationError) Unwrap() error {
	return e.Cause
}
