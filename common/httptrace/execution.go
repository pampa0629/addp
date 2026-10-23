package httptrace

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/addp/common/execution/observability"
	"github.com/addp/common/logger"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type ExecutionPhase string

const (
	PhaseEnqueue  ExecutionPhase = "enqueue"
	PhaseClaim    ExecutionPhase = "claim"
	PhaseDispatch ExecutionPhase = "dispatch"
	PhaseTerminal ExecutionPhase = "terminal"
)

type ExecutionOutcome string

const (
	OutcomeSuccess   ExecutionOutcome = "success"
	OutcomeFailed    ExecutionOutcome = "failed"
	OutcomeTimeout   ExecutionOutcome = "timeout"
	OutcomeCancelled ExecutionOutcome = "cancelled"
	OutcomeRejected  ExecutionOutcome = "rejected"
)

var invalidExecutionStage atomic.Uint64

// CurrentTraceReference captures only a live local span from this exact runtime.
// Remote SpanContext values alone can never produce a durable association.
func CurrentTraceReference(ctx context.Context) *observability.TraceReference {
	r := current.Load()
	if r == nil || ctx == nil {
		return nil
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return nil
	}
	local, ok := span.(sdk.ReadOnlySpan)
	if !ok {
		return nil
	}
	scope := local.InstrumentationScope().Name
	if scope != "addp.http" && scope != "addp.execution" {
		return nil
	}
	if span.TracerProvider() != r.provider || scope == "addp.http" && local.SpanKind() != trace.SpanKindServer && local.SpanKind() != trace.SpanKindClient {
		return nil
	}
	identity := map[string]string{}
	for _, v := range local.Resource().Attributes() {
		identity[string(v.Key)] = v.Value.AsString()
	}
	if identity["service.name"] != r.module || identity["service.instance.id"] != r.instance || identity["addp.role"] != r.role {
		return nil
	}
	sc := span.SpanContext()
	if !sc.IsValid() || sc.IsRemote() {
		return nil
	}
	reference := &observability.TraceReference{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String(), ModuleName: r.module, Role: r.role, InstanceID: r.instance, StartedAt: local.StartTime().UTC()}
	if reference.Validate() != nil {
		return nil
	}
	return reference
}

type ExecutionStage struct {
	span      trace.Span
	reference *observability.TraceReference
	context   context.Context
	phase     ExecutionPhase
	attempt   int
	once      sync.Once
}

// StartExecutionStage starts a new root while retaining the owner's business
// cancellation/deadline. References must come from the authorized owner snapshot.
func StartExecutionStage(ctx context.Context, phase ExecutionPhase, attempt int, enqueue, latest *observability.TraceReference) (context.Context, *ExecutionStage) {
	noop := &ExecutionStage{}
	r := current.Load()
	if r == nil || r.role != "backend" || ctx == nil {
		return ctx, noop
	}
	var kind trace.SpanKind
	switch phase {
	case PhaseEnqueue:
		kind = trace.SpanKindProducer
	case PhaseClaim:
		kind = trace.SpanKindConsumer
	case PhaseDispatch:
		kind = trace.SpanKindProducer
	case PhaseTerminal:
		kind = trace.SpanKindInternal
	default:
		invalidExecutionStage.Add(1)
		return ctx, noop
	}
	if phase == PhaseEnqueue && attempt != 0 || phase != PhaseEnqueue && attempt < 1 {
		invalidExecutionStage.Add(1)
		return ctx, noop
	}
	references := []*observability.TraceReference{enqueue, latest}
	if phase == PhaseEnqueue {
		references = nil
		if local := CurrentTraceReference(ctx); local != nil {
			// Enqueue's one entry edge must be an actual local HTTP span.
			if s, ok := trace.SpanFromContext(ctx).(sdk.ReadOnlySpan); ok && s.InstrumentationScope().Name == "addp.http" {
				references = append(references, local)
			}
		}
	}
	links := []trace.Link{}
	seen := map[string]observability.TraceReference{}
	for _, ref := range references {
		if ref == nil {
			continue
		}
		if ref.Validate() != nil {
			invalidExecutionStage.Add(1)
			return ctx, noop
		}
		key := ref.TraceID + ":" + ref.SpanID
		if prior, exists := seen[key]; exists {
			if prior.ModuleName != ref.ModuleName || prior.Role != ref.Role || prior.InstanceID != ref.InstanceID || !prior.StartedAt.Equal(ref.StartedAt) {
				invalidExecutionStage.Add(1)
				return ctx, noop
			}
			continue
		}
		seen[key] = *ref
		tid, _ := trace.TraceIDFromHex(ref.TraceID)
		sid, _ := trace.SpanIDFromHex(ref.SpanID)
		links = append(links, trace.Link{SpanContext: trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid}), Attributes: []attribute.KeyValue{
			attribute.String("addp.link.module_name", ref.ModuleName), attribute.String("addp.link.role", ref.Role), attribute.String("addp.link.instance_id", ref.InstanceID), attribute.String("addp.link.started_at", ref.StartedAt.UTC().Format(time.RFC3339Nano)),
		}})
	}
	started := time.Now().UTC()
	child, span := r.provider.Tracer("addp.execution").Start(ctx, "execution."+string(phase), trace.WithNewRoot(), trace.WithSpanKind(kind), trace.WithTimestamp(started), trace.WithLinks(links...), trace.WithAttributes(attribute.String("addp.execution.phase", string(phase)), attribute.Int("addp.execution.attempt", attempt)))
	sc := span.SpanContext()
	reference := &observability.TraceReference{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String(), ModuleName: r.module, Role: r.role, InstanceID: r.instance, StartedAt: started}
	if !span.IsRecording() || reference.Validate() != nil {
		span.End()
		return ctx, noop
	}
	return child, &ExecutionStage{span: span, reference: reference, context: child, phase: phase, attempt: attempt}
}

func (s *ExecutionStage) Reference() *observability.TraceReference {
	if s == nil || s.reference == nil {
		return nil
	}
	value := *s.reference
	return &value
}
func (s *ExecutionStage) End(outcome ExecutionOutcome) {
	s.end("", outcome)
}

// EndExecution emits one fixed checkpoint through the existing structured
// logger. The business UUID is never a Span attribute or deployment identity.
// Disabled/invalid stages and invalid UUIDs cannot create correlated log facts.
func (s *ExecutionStage) EndExecution(executionID string, outcome ExecutionOutcome) {
	s.end(executionID, outcome)
}

func (s *ExecutionStage) end(executionID string, outcome ExecutionOutcome) {
	if s == nil || s.span == nil {
		return
	}
	s.once.Do(func() {
		switch outcome {
		case OutcomeSuccess, OutcomeFailed, OutcomeTimeout, OutcomeCancelled, OutcomeRejected:
			s.span.SetAttributes(attribute.String("addp.execution.outcome", string(outcome)))
			if outcome == OutcomeFailed || outcome == OutcomeTimeout {
				s.span.SetStatus(codes.Error, "")
			}
			if parsed, err := uuid.Parse(executionID); err == nil && parsed != uuid.Nil && parsed.String() == executionID {
				logger.L().InfoContext(s.context, "execution.checkpoint", "execution_id", executionID, "phase", string(s.phase), "attempt", s.attempt, "outcome", string(outcome))
			}
		default:
			invalidExecutionStage.Add(1)
		}
		s.span.End()
	})
}
