package service

import (
	"fmt"
	"strings"
)

type OrchestrationReferenceValidationCode string

const (
	OrchestrationReferenceLookupUnavailable OrchestrationReferenceValidationCode = "lookup_unavailable"
	OrchestrationReferenceSelf              OrchestrationReferenceValidationCode = "self_reference"
	OrchestrationReferenceCycle             OrchestrationReferenceValidationCode = "reference_cycle"
	OrchestrationReferenceNotFound          OrchestrationReferenceValidationCode = "reference_not_found"
)

// OrchestrationReferenceValidationError carries stable recursive-reference context.
type OrchestrationReferenceValidationError struct {
	Code            OrchestrationReferenceValidationCode
	OrchestrationID uint
	Path            []uint
	Cause           error
}

func (e *OrchestrationReferenceValidationError) Error() string {
	switch e.Code {
	case OrchestrationReferenceLookupUnavailable:
		return "orchestration lookup is required"
	case OrchestrationReferenceSelf:
		return fmt.Sprintf("orchestration %d cannot reference itself", e.OrchestrationID)
	case OrchestrationReferenceCycle:
		return fmt.Sprintf("orchestration reference cycle detected: %s", formatOrchestrationReferencePath(e.Path))
	case OrchestrationReferenceNotFound:
		return fmt.Sprintf("referenced orchestration %d not found", e.OrchestrationID)
	default:
		return "orchestration reference is invalid"
	}
}

func (e *OrchestrationReferenceValidationError) Unwrap() error {
	return e.Cause
}

type ScheduleValidationCode string

const (
	ScheduleExpressionInvalid ScheduleValidationCode = "schedule_expression_invalid"
	ScheduleNextRunFailed     ScheduleValidationCode = "schedule_next_run_failed"
)

// ScheduleValidationError carries stable schedule validation context.
type ScheduleValidationError struct {
	Code       ScheduleValidationCode
	Expression string
	Cause      error
}

func (e *ScheduleValidationError) Error() string {
	switch e.Code {
	case ScheduleExpressionInvalid:
		return fmt.Sprintf("invalid orchestration schedule: %v", e.Cause)
	case ScheduleNextRunFailed:
		return fmt.Sprintf("calculate orchestration next_run_at: %v", e.Cause)
	default:
		return "orchestration schedule is invalid"
	}
}

func (e *ScheduleValidationError) Unwrap() error {
	return e.Cause
}

func formatOrchestrationReferencePath(ids []uint) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d", id))
	}
	return strings.Join(parts, " -> ")
}
