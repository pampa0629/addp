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
