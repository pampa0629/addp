package runtimelog

import (
	"strings"
	"testing"
)

func TestTraceFieldsAreCanonicalAndNeverDerivedFromMessage(t *testing.T) {
	o := Options{Module: "monitor", Role: "backend", InstanceID: "startup"}
	good := Normalize(o, "stdout", []byte(`{"trace_id":"11111111111111111111111111111111","span_id":"2222222222222222","message":"safe"}`), 1, false)
	if good.TraceID == "" || good.SpanID == "" {
		t.Fatal("lost trace fields")
	}
	for _, input := range []string{`{"trace_id":"00000000000000000000000000000000","span_id":"2222222222222222"}`, `{"trace_id":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","span_id":"2222222222222222"}`, `{"message":"trace_id=11111111111111111111111111111111"}`} {
		got := Normalize(o, "stdout", []byte(input), 2, false)
		if got.TraceID != "" || got.SpanID != "" {
			t.Fatal("accepted untrusted field grammar")
		}
	}
	if strings.Contains(good.Message, "trace_id") {
		t.Fatal("duplicate correlation field remained in message")
	}
}
