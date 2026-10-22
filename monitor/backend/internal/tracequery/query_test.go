package tracequery

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"strings"
	"testing"
	"time"
)

func text(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}
func fixture() *collector.ExportTraceServiceRequest {
	id, _ := hex.DecodeString(strings.Repeat("1", 32))
	now := uint64(time.Now().Add(-time.Minute).UnixNano())
	span := func(n, parent byte, kind tracepb.Span_SpanKind) *tracepb.Span {
		p := []byte(nil)
		if parent != 0 {
			p = bytes.Repeat([]byte{parent}, 8)
		}
		return &tracepb.Span{TraceId: id, SpanId: bytes.Repeat([]byte{n}, 8), ParentSpanId: p, Name: "private raw /secret?token=private", Kind: kind, StartTimeUnixNano: now, EndTimeUnixNano: now + 1000, Attributes: []*commonpb.KeyValue{text("http.request.method", "GET"), text("http.route", "/items/:id"), text("url.full", "https://private/?secret=value")}}
	}
	batch := func(module, instance string, spans ...*tracepb.Span) *tracepb.ResourceSpans {
		return &tracepb.ResourceSpans{Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{text("service.name", module), text("service.instance.id", instance), text("addp.role", "backend")}}, ScopeSpans: []*tracepb.ScopeSpans{{Scope: &commonpb.InstrumentationScope{Name: "addp.http"}, Spans: spans}}}
	}
	return &collector.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{batch("monitor", "one", span(1, 0, tracepb.Span_SPAN_KIND_SERVER), span(2, 1, tracepb.Span_SPAN_KIND_CLIENT)), batch("system", "two", span(3, 2, tracepb.Span_SPAN_KIND_SERVER))}}
}
func TestDecodeUsesOnlyMatchedSpansAndSafeProjection(t *testing.T) {
	raw := fixture()
	d, e := Decode(strings.Repeat("1", 32), raw)
	if e != nil || d.Partial || len(d.Edges) != 1 || d.Edges[0].FromModule != "monitor" || d.Edges[0].ToModule != "system" {
		t.Fatal(d, e)
	}
	encoded, _ := json.Marshal(d)
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "secret") {
		t.Fatal("raw trace fields escaped projection")
	}
	raw.ResourceSpans[1].ScopeSpans[0].Spans[0].ParentSpanId = bytes.Repeat([]byte{4}, 8)
	d, e = Decode(d.ID, raw)
	if e != nil || !d.Partial || len(d.Edges) != 0 {
		t.Fatal("inferred edge from missing parent", d, e)
	}
}
func TestDecodeRejectsForeignTraceCyclesDuplicatesAndBudget(t *testing.T) {
	for _, mutation := range []func(*collector.ExportTraceServiceRequest){
		func(r *collector.ExportTraceServiceRequest) {
			r.ResourceSpans[0].ScopeSpans[0].Spans[0].TraceId = bytes.Repeat([]byte{2}, 16)
		},
		func(r *collector.ExportTraceServiceRequest) {
			r.ResourceSpans[0].ScopeSpans[0].Spans[0].ParentSpanId = bytes.Repeat([]byte{2}, 8)
		},
		func(r *collector.ExportTraceServiceRequest) {
			r.ResourceSpans[1].ScopeSpans[0].Spans[0].SpanId = bytes.Repeat([]byte{1}, 8)
		},
		func(r *collector.ExportTraceServiceRequest) { r.ResourceSpans[0].ScopeSpans[0].Scope.Name = "foreign" },
	} {
		r := fixture()
		mutation(r)
		if _, e := Decode(strings.Repeat("1", 32), r); !errors.Is(e, ErrUpstream) {
			t.Fatal("accepted malformed span graph", e)
		}
	}
}

func TestTempoSearchIDsNormalizeAtTheProtocolBoundary(t *testing.T) {
	for _, value := range []string{"1", "e61da62207386a80ffc6da15b292022", strings.Repeat("1", 32)} {
		id, err := tempoTraceID(value)
		if err != nil || len(id) != 32 || !strings.HasSuffix(id, value) {
			t.Fatal(id, err)
		}
	}
	for _, value := range []string{"", "0", "PRIVATE", strings.Repeat("1", 33)} {
		if _, err := tempoTraceID(value); err == nil {
			t.Fatal("accepted invalid Tempo ID")
		}
	}
}

func TestMissingServerDoesNotBecomeACompleteDependencyGraph(t *testing.T) {
	raw := fixture()
	raw.ResourceSpans = raw.ResourceSpans[:1]
	result, e := Decode(strings.Repeat("1", 32), raw)
	if e != nil || !result.Partial || len(result.Edges) != 0 {
		t.Fatal("missing server was treated as complete", result, e)
	}
}

func executionFixture() *collector.ExportTraceServiceRequest {
	raw := fixture()
	raw.ResourceSpans = raw.ResourceSpans[:1]
	scope := raw.ResourceSpans[0].ScopeSpans[0]
	scope.Scope.Name = "addp.execution"
	scope.Spans = scope.Spans[:1]
	span := scope.Spans[0]
	span.Kind = tracepb.Span_SPAN_KIND_INTERNAL
	span.Attributes = []*commonpb.KeyValue{text("addp.execution.phase", "terminal"), {Key: "addp.execution.attempt", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 2}}}, text("addp.execution.outcome", "failed")}
	span.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR}
	span.Links = []*tracepb.Span_Link{{TraceId: bytes.Repeat([]byte{10}, 16), SpanId: bytes.Repeat([]byte{11}, 8), Attributes: []*commonpb.KeyValue{text("addp.link.module_name", "system"), text("addp.link.role", "backend"), text("addp.link.instance_id", "two"), text("addp.link.started_at", time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339Nano))}}}
	return raw
}
func TestExecutionRootProjectsOnlyCausalLinksAndFinitePhase(t *testing.T) {
	raw := executionFixture()
	result, err := Decode(strings.Repeat("1", 32), raw)
	if err != nil || result.Partial || len(result.Edges) != 0 || len(result.Spans) != 1 || len(result.Spans[0].Links) != 1 {
		t.Fatal(result, err)
	}
	span := result.Spans[0]
	if span.Scope != "addp.execution" || span.Phase != "terminal" || span.Attempt != 2 || span.Outcome != "failed" || !span.Failed || span.Links[0].RuntimeInstanceID != 0 {
		t.Fatal("execution identity or untrusted System ID", span)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "secret") {
		t.Fatal("private fields escaped", string(encoded))
	}
	for _, outcome := range []string{"success", "timeout", "cancelled", "rejected"} {
		span := raw.ResourceSpans[0].ScopeSpans[0].Spans[0]
		span.Attributes[2] = text("addp.execution.outcome", outcome)
		span.Status.Code = tracepb.Status_STATUS_CODE_UNSET
		if outcome == "timeout" {
			span.Status.Code = tracepb.Status_STATUS_CODE_ERROR
		}
		if _, err := Decode(result.ID, raw); err != nil {
			t.Fatal(outcome, err)
		}
	}
	for phase, kind := range map[string]tracepb.Span_SpanKind{"enqueue": tracepb.Span_SPAN_KIND_PRODUCER, "claim": tracepb.Span_SPAN_KIND_CONSUMER, "dispatch": tracepb.Span_SPAN_KIND_PRODUCER} {
		raw := executionFixture()
		span := raw.ResourceSpans[0].ScopeSpans[0].Spans[0]
		span.Attributes[0] = text("addp.execution.phase", phase)
		span.Kind = kind
		if phase == "enqueue" {
			span.Attributes[1].Value.Value = &commonpb.AnyValue_IntValue{IntValue: 0}
		}
		if _, err := Decode(result.ID, raw); err != nil {
			t.Fatal(phase, err)
		}
	}
}
func TestExecutionRejectsUnsafeScopeKindsAttributesAndLinks(t *testing.T) {
	for name, mutate := range map[string]func(*tracepb.ScopeSpans, *tracepb.Span){
		"http link": func(scope *tracepb.ScopeSpans, span *tracepb.Span) { scope.Scope.Name = "addp.http" },
		"kind":      func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.Kind = tracepb.Span_SPAN_KIND_SERVER },
		"parent":    func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.ParentSpanId = bytes.Repeat([]byte{2}, 8) },
		"role":      func(scope *tracepb.ScopeSpans, span *tracepb.Span) {},
		"phase": func(_ *tracepb.ScopeSpans, span *tracepb.Span) {
			span.Attributes[0] = text("addp.execution.phase", "poll")
		},
		"attempt zero": func(_ *tracepb.ScopeSpans, span *tracepb.Span) {
			span.Attributes[1].Value.Value = &commonpb.AnyValue_IntValue{IntValue: 0}
		},
		"attempt type": func(_ *tracepb.ScopeSpans, span *tracepb.Span) {
			span.Attributes[1] = text("addp.execution.attempt", "2")
		},
		"unknown outcome": func(_ *tracepb.ScopeSpans, span *tracepb.Span) {
			span.Attributes[2] = text("addp.execution.outcome", "secret")
		},
		"wrong error":         func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.Status.Code = tracepb.Status_STATUS_CODE_UNSET },
		"duplicate attribute": func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.Attributes[1] = span.Attributes[0] },
		"sensitive attribute": func(_ *tracepb.ScopeSpans, span *tracepb.Span) {
			span.Attributes = append(span.Attributes, text("db.statement", "secret"))
		},
		"dropped attribute": func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.DroppedAttributesCount = 1 },
		"events":            func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.Events = []*tracepb.Span_Event{{Name: "secret"}} },
		"same trace link":   func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.Links[0].TraceId = span.TraceId },
		"duplicate link":    func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.Links = append(span.Links, span.Links[0]) },
		"three links": func(_ *tracepb.ScopeSpans, span *tracepb.Span) {
			span.Links = append(span.Links, span.Links[0], span.Links[0])
		},
		"invalid ID": func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.Links[0].SpanId = make([]byte, 8) },
		"unknown link attribute": func(_ *tracepb.ScopeSpans, span *tracepb.Span) {
			span.Links[0].Attributes[3] = text("execution_id", "secret")
		},
		"duplicate link attribute": func(_ *tracepb.ScopeSpans, span *tracepb.Span) {
			span.Links[0].Attributes[3] = span.Links[0].Attributes[0]
		},
		"non UTC link": func(_ *tracepb.ScopeSpans, span *tracepb.Span) {
			span.Links[0].Attributes[3] = text("addp.link.started_at", "2026-10-20T00:00:00+00:00")
		},
		"link tracestate": func(_ *tracepb.ScopeSpans, span *tracepb.Span) { span.Links[0].TraceState = "private=secret" },
	} {
		t.Run(name, func(t *testing.T) {
			raw := executionFixture()
			scope := raw.ResourceSpans[0].ScopeSpans[0]
			mutate(scope, scope.Spans[0])
			if name == "role" {
				raw.ResourceSpans[0].Resource.Attributes[2] = text("addp.role", "ingress")
			}
			if _, err := Decode(strings.Repeat("1", 32), raw); !errors.Is(err, ErrUpstream) {
				t.Fatal("accepted unsafe stage", err)
			}
		})
	}
}
