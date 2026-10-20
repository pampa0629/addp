package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"go.opentelemetry.io/otel/trace"
	"testing"
)

func TestTraceCorrelationRemainsTopLevelThroughGroups(t *testing.T) {
	var output bytes.Buffer
	l := buildLogger(Options{}, &output).With("module", "monitor").WithGroup("detail").With("safe", true)
	tid, _ := trace.TraceIDFromHex("11111111111111111111111111111111")
	sid, _ := trace.SpanIDFromHex("2222222222222222")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid}))
	l.InfoContext(ctx, "safe", "trace_id", "private-forged")
	var value map[string]any
	if json.Unmarshal(output.Bytes(), &value) != nil || value["trace_id"] != tid.String() || value["span_id"] != sid.String() {
		t.Fatal("lost top-level Context correlation", output.String())
	}
	detail := value["detail"].(map[string]any)
	if _, ok := detail["trace_id"]; ok {
		t.Fatal("forged trace field survived")
	}
}

func TestCorrelationCannotBeForgedWithoutSpanContext(t *testing.T) {
	var output bytes.Buffer
	l := buildLogger(Options{}, &output).With("trace_id", "11111111111111111111111111111111")
	l.Info("safe", "trace_id", "22222222222222222222222222222222", "span_id", "3333333333333333")
	var row map[string]any
	if err := json.Unmarshal(output.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["trace_id"] != nil || row["span_id"] != nil {
		t.Fatal("record supplied correlation without Context")
	}
}
