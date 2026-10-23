package httptrace

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/execution/observability"
	"github.com/addp/common/logger"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestExecutionCheckpointUsesActualContextOnceWithoutExportingBusinessIdentity(t *testing.T) {
	runtime, exporter := runtimeForTest(t, "backend")
	var output bytes.Buffer
	logger.Init(logger.Options{Writer: &output})
	t.Cleanup(func() { logger.Init(logger.Options{}) })
	executionID := uuid.NewString()
	ctx := context.WithValue(t.Context(), struct{}{}, "SQL parameters results token user tenant private sentinel")
	_, stage := StartExecutionStage(ctx, PhaseTerminal, 3, nil, nil)
	reference := stage.Reference()
	if reference == nil {
		t.Fatal("actual stage missing")
	}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			stage.EndExecution(executionID, OutcomeSuccess)
		}()
	}
	group.Wait()
	stage.End(OutcomeRejected)
	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("checkpoint count=%d", len(lines))
	}
	var record map[string]interface{}
	if err := json.Unmarshal(lines[0], &record); err != nil {
		t.Fatal(err)
	}
	if record["msg"] != "execution.checkpoint" || record["execution_id"] != executionID || record["phase"] != "terminal" || record["attempt"] != float64(3) || record["outcome"] != "success" || record["trace_id"] != reference.TraceID || record["span_id"] != reference.SpanID {
		t.Fatal("checkpoint did not use its real short-stage context")
	}
	allowed := map[string]bool{"time": true, "level": true, "msg": true, "execution_id": true, "phase": true, "attempt": true, "outcome": true, "trace_id": true, "span_id": true}
	for key := range record {
		if !allowed[key] {
			t.Fatalf("checkpoint exported unexpected body field %s", key)
		}
	}
	if bytes.Contains(output.Bytes(), []byte("private sentinel")) {
		t.Fatal("checkpoint serialized business context")
	}
	flush(t, runtime)
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	for _, span := range exporter.spans {
		payload, _ := json.Marshal(spanAttributes(span))
		if bytes.Contains(payload, []byte(executionID)) || bytes.Contains(payload, []byte("execution_id")) {
			t.Fatal("execution UUID became a Span attribute")
		}
	}
}

func TestExecutionCheckpointRejectsInvalidIdentityOrOutcomeAndDisabledTracing(t *testing.T) {
	runtime, exporter := runtimeForTest(t, "backend")
	var output bytes.Buffer
	logger.Init(logger.Options{Writer: &output})
	t.Cleanup(func() { logger.Init(logger.Options{}) })
	for _, id := range []string{"", uuid.Nil.String(), "SELECT private-value", "ABCDEFAB-CDEF-4ABC-8DEF-ABCDEFABCDEF", "not-a-uuid"} {
		_, stage := StartExecutionStage(t.Context(), PhaseDispatch, 1, nil, nil)
		stage.EndExecution(id, OutcomeRejected)
	}
	_, invalid := StartExecutionStage(t.Context(), PhaseTerminal, 1, nil, nil)
	invalid.EndExecution(uuid.NewString(), ExecutionOutcome("private-invalid-outcome"))
	_, ended := StartExecutionStage(t.Context(), PhaseClaim, 1, nil, nil)
	ended.End(OutcomeSuccess)
	ended.EndExecution(uuid.NewString(), OutcomeSuccess)
	if output.Len() != 0 {
		t.Fatal("invalid or already ended stage emitted a checkpoint")
	}
	flush(t, runtime)
	exporter.mu.Lock()
	endedCount := len(exporter.spans)
	exporter.mu.Unlock()
	if endedCount != 7 {
		t.Fatalf("invalid checkpoint prevented normal Span End: ended=%d", endedCount)
	}
	current.Store(nil)
	remote := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, Remote: true})
	_, disabled := StartExecutionStage(trace.ContextWithRemoteSpanContext(t.Context(), remote), PhaseClaim, 1, nil, nil)
	disabled.EndExecution(uuid.NewString(), OutcomeSuccess)
	if output.Len() != 0 {
		t.Fatal("disabled tracing fabricated a correlated checkpoint")
	}
}

func stageRef(traceID, spanID string) *observability.TraceReference {
	return &observability.TraceReference{TraceID: strings.Repeat(traceID, 32), SpanID: strings.Repeat(spanID, 16), ModuleName: "orchestrator", Role: "backend", InstanceID: "prior-process", StartedAt: time.Now().UTC().Add(-time.Hour)}
}
func spanAttributes(span sdk.ReadOnlySpan) map[string]any {
	out := map[string]any{}
	for _, a := range span.Attributes() {
		out[string(a.Key)] = a.Value.AsInterface()
	}
	return out
}
func TestExecutionStagesNewRootsBoundedLinksAndActualEntry(t *testing.T) {
	r, exporter := runtimeForTest(t, "backend")
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), struct{}{}, "safe-value"))
	defer cancel()
	entryCtx, httpSpan := r.provider.Tracer("addp.http").Start(ctx, "POST /tasks", trace.WithSpanKind(trace.SpanKindServer))
	entryRef := CurrentTraceReference(entryCtx)
	if entryRef == nil {
		t.Fatal("actual entry not captured")
	}
	enqueueCtx, enqueue := StartExecutionStage(entryCtx, PhaseEnqueue, 0, stageRef("a", "b"), nil)
	if enqueue.Reference() == nil || enqueue.Reference().TraceID == entryRef.TraceID {
		t.Fatal("enqueue inherited HTTP parent")
	}
	frozen := enqueue.Reference()
	changed := enqueue.Reference()
	changed.SpanID = "1111111111111111"
	if enqueue.Reference().SpanID != frozen.SpanID {
		t.Fatal("reference aliases")
	}
	enqueue.End(OutcomeSuccess)
	enqueue.End(OutcomeFailed)
	httpSpan.End()
	if CurrentTraceReference(entryCtx) != nil {
		t.Fatal("ended entry captured")
	}
	phases := []ExecutionPhase{PhaseClaim, PhaseDispatch, PhaseTerminal}
	kinds := map[ExecutionPhase]trace.SpanKind{PhaseEnqueue: trace.SpanKindProducer, PhaseClaim: trace.SpanKindConsumer, PhaseDispatch: trace.SpanKindProducer, PhaseTerminal: trace.SpanKindInternal}
	latest := frozen
	for _, phase := range phases {
		phaseCtx, stage := StartExecutionStage(enqueueCtx, phase, 2, frozen, latest)
		if stage.Reference() == nil || stage.Reference().TraceID == frozen.TraceID {
			t.Fatal("stage not new trace")
		}
		if phaseCtx.Value(struct{}{}) != "safe-value" {
			t.Fatal("context value lost")
		}
		latest = stage.Reference()
		stage.End(OutcomeSuccess)
	}
	cancel()
	select {
	case <-enqueueCtx.Done():
	default:
		t.Fatal("owner cancellation lost")
	}
	flush(t, r)
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	executionCount := 0
	for _, span := range exporter.spans {
		if span.InstrumentationScope().Name != "addp.execution" {
			continue
		}
		executionCount++
		attrs := spanAttributes(span)
		phase := ExecutionPhase(attrs["addp.execution.phase"].(string))
		if span.Parent().IsValid() || span.SpanKind() != kinds[phase] || span.Name() != "execution."+string(phase) {
			t.Fatal("stage contract")
		}
		if len(attrs) != 3 || attrs["addp.execution.outcome"] != "success" {
			t.Fatal("arbitrary attributes or non-idempotent end", attrs)
		}
		expected := 2
		if phase == PhaseEnqueue || phase == PhaseClaim {
			expected = 1
		}
		if len(span.Links()) != expected {
			t.Fatalf("phase %s links=%d", phase, len(span.Links()))
		}
		for _, link := range span.Links() {
			if len(link.Attributes) != 4 || link.SpanContext.TraceID() == span.SpanContext().TraceID() {
				t.Fatal("unsafe link")
			}
		}
		if phase == PhaseEnqueue && span.Links()[0].SpanContext.TraceID().String() != entryRef.TraceID {
			t.Fatal("payload entry accepted")
		}
	}
	if executionCount != 4 {
		t.Fatal("missing stages", executionCount)
	}
}
func TestExecutionStageOutcomesAndInvalidInputSoft(t *testing.T) {
	r, exporter := runtimeForTest(t, "backend")
	for _, outcome := range []ExecutionOutcome{OutcomeSuccess, OutcomeFailed, OutcomeTimeout, OutcomeCancelled, OutcomeRejected, "secret-error"} {
		_, stage := StartExecutionStage(context.Background(), PhaseTerminal, 1, nil, nil)
		stage.End(outcome)
	}
	before := invalidExecutionStage.Load()
	for _, test := range []struct {
		phase   ExecutionPhase
		attempt int
	}{{PhaseEnqueue, 1}, {PhaseClaim, 0}, {"payload", 1}} {
		ctx := context.Background()
		got, stage := StartExecutionStage(ctx, test.phase, test.attempt, nil, nil)
		if got != ctx || stage.Reference() != nil {
			t.Fatal("invalid stage generated fact")
		}
		stage.End(OutcomeSuccess)
	}
	invalid := stageRef("1", "2")
	invalid.InstanceID = "secret\nvalue"
	if _, stage := StartExecutionStage(context.Background(), PhaseClaim, 1, invalid, nil); stage.Reference() != nil {
		t.Fatal("invalid persisted reference accepted")
	}
	if invalidExecutionStage.Load() != before+4 {
		t.Fatal("invalid stage not counted")
	}
	flush(t, r)
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	if len(exporter.spans) != 6 {
		t.Fatal("invalid input exported extra stage")
	}
	for _, span := range exporter.spans {
		attrs := spanAttributes(span)
		outcome, _ := attrs["addp.execution.outcome"].(string)
		wantError := outcome == "failed" || outcome == "timeout"
		if (span.Status().Code == codes.Error) != wantError || span.Status().Description != "" {
			t.Fatal("outcome classification")
		}
		raw, _ := json.Marshal(attrs)
		if strings.Contains(string(raw), "secret") {
			t.Fatal("raw outcome leaked")
		}
	}
}
func TestExecutionEntryRejectsRemoteForeignOrWrongScope(t *testing.T) {
	r, _ := runtimeForTest(t, "backend")
	remote := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, Remote: true})
	if CurrentTraceReference(trace.ContextWithRemoteSpanContext(context.Background(), remote)) != nil {
		t.Fatal("remote manufactured ref")
	}
	for _, scope := range []string{"foreign", "addp.execution"} {
		ctx, span := r.provider.Tracer(scope).Start(context.Background(), "safe")
		_, stage := StartExecutionStage(ctx, PhaseEnqueue, 0, nil, nil)
		stage.End(OutcomeSuccess)
		if scope == "foreign" && CurrentTraceReference(ctx) != nil {
			t.Fatal("foreign scope captured")
		}
		span.End()
	}
	old := current.Swap(nil)
	defer current.Store(old)
	ctx := context.Background()
	got, stage := StartExecutionStage(ctx, PhaseClaim, 1, nil, nil)
	if got != ctx || stage.Reference() != nil {
		t.Fatal("disabled trace blocks business")
	}
	stage.End(OutcomeSuccess)
}

func TestExecutionCaptureRequiresOwnedProviderAndHTTPKind(t *testing.T) {
	r, _ := runtimeForTest(t, "backend")
	foreign := sdk.NewTracerProvider(sdk.WithResource(resource.NewSchemaless(attribute.String("service.name", r.module), attribute.String("service.instance.id", r.instance), attribute.String("addp.role", r.role))))
	defer foreign.Shutdown(context.Background())
	ctx, span := foreign.Tracer("addp.http").Start(context.Background(), "POST /tasks", trace.WithSpanKind(trace.SpanKindServer))
	if CurrentTraceReference(ctx) != nil {
		t.Fatal("equal resource on foreign provider manufactured reference")
	}
	span.End()
	ctx, span = r.provider.Tracer("addp.http").Start(context.Background(), "not HTTP", trace.WithSpanKind(trace.SpanKindInternal))
	if CurrentTraceReference(ctx) != nil {
		t.Fatal("non HTTP kind manufactured entry")
	}
	span.End()
	first := stageRef("a", "b")
	second := *first
	second.InstanceID = "other-process"
	if _, stage := StartExecutionStage(context.Background(), PhaseClaim, 1, first, &second); stage.Reference() != nil {
		t.Fatal("conflicting duplicate link identity accepted")
	}
	old := r.role
	r.role = "ingress"
	if _, stage := StartExecutionStage(context.Background(), PhaseClaim, 1, nil, nil); stage.Reference() != nil {
		t.Fatal("stage widened ingress deployment")
	}
	r.role = old
}

func TestDeploymentIdentityExportIgnoresStandardOTELResourceEnvironment(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "private.sentinel=DO_NOT_EXPORT,service.name=attacker,service.instance.id=attacker-instance,addp.role=worker")
	t.Setenv("OTEL_SERVICE_NAME", "attacker-service")
	r, exporter := runtimeForTest(t, "backend")
	ctx, span := r.provider.Tracer("addp.http").Start(context.Background(), "GET /safe", trace.WithSpanKind(trace.SpanKindServer))
	if CurrentTraceReference(ctx) == nil {
		t.Fatal("environment displaced actual registered identity")
	}
	local := span.(sdk.ReadOnlySpan)
	original := local.Resource()
	hasSentinel := false
	for _, a := range original.Attributes() {
		if a.Key == "private.sentinel" {
			hasSentinel = true
		}
	}
	if !hasSentinel {
		t.Fatal("test did not exercise SDK environment merge")
	}
	_, stage := StartExecutionStage(ctx, PhaseEnqueue, 0, nil, nil)
	stage.End(OutcomeSuccess)
	span.End()
	flush(t, r)
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	if len(exporter.spans) != 2 {
		t.Fatal("missing HTTP/execution export")
	}
	for _, exported := range exporter.spans {
		values := map[string]string{}
		for _, a := range exported.Resource().Attributes() {
			values[string(a.Key)] = a.Value.AsString()
		}
		if len(values) != 3 || values["service.name"] != "monitor" || values["service.instance.id"] != "test-instance" || values["addp.role"] != "backend" {
			t.Fatal("unsafe wire resource", values)
		}
		raw, _ := json.Marshal(exported.Resource().Attributes())
		if strings.Contains(string(raw), "DO_NOT_EXPORT") || strings.Contains(string(raw), "attacker") {
			t.Fatal("environment leaked to export")
		}
	}
	if local.Resource() != original {
		t.Fatal("wire projection mutated live SDK span")
	}
}
