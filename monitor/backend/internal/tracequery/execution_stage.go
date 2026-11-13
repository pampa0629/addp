package tracequery

import (
	"time"

	"github.com/addp/common/execution/observability"
)

// ExecutionStage contains only the permitted tenant projection of one anchor.
// The reference must come from the currently authorized execution, never input.
type ExecutionStage struct {
	Phase      string
	Attempt    int
	Outcome    string
	Start      time.Time
	End        time.Time
	DurationMS float64
}

// ProjectExecutionStage does not authorize a Trace, its descendants or Links.
// Decode has already checked the full candidate graph at the protocol boundary.
func ProjectExecutionStage(detail Detail, ref observability.TraceReference, slot string) (ExecutionStage, error) {
	if detail.ID != ref.TraceID {
		return ExecutionStage{}, ErrUpstream
	}
	for _, span := range detail.Spans {
		if span.ID != ref.SpanID {
			continue
		}
		if span.Module != ref.ModuleName || span.Instance != ref.InstanceID || span.Role != ref.Role || !span.Start.Equal(ref.StartedAt) || span.Scope != "addp.execution" || span.ParentID != "" {
			return ExecutionStage{}, ErrUpstream
		}
		if slot == "enqueue" && span.Phase != "enqueue" || slot == "latest" && span.Phase == "enqueue" {
			return ExecutionStage{}, ErrUpstream
		}
		return ExecutionStage{span.Phase, span.Attempt, span.Outcome, span.Start, span.End, span.DurationMS}, nil
	}
	// A retained Trace without the exact anchor is a binding failure, not a
	// normally missing sample. No other Span may substitute for it.
	return ExecutionStage{}, ErrUpstream
}
