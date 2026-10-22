package observability

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func reference() TraceReference {
	return TraceReference{"11111111111111111111111111111111", "2222222222222222", "orchestrator", "backend", "process-1", time.Date(2026, 10, 23, 1, 2, 3, 123, time.UTC)}
}
func TestReferenceStrictRoundTrip(t *testing.T) {
	ref := reference()
	raw, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseTraceReference(raw)
	if err != nil || *parsed != ref {
		t.Fatal("round trip", parsed, err)
	}
	for _, mutate := range []func(*TraceReference){
		func(r *TraceReference) { r.TraceID = strings.Repeat("0", 32) }, func(r *TraceReference) { r.SpanID = strings.Repeat("0", 16) },
		func(r *TraceReference) { r.TraceID = strings.Repeat("A", 32) }, func(r *TraceReference) { r.SpanID = "22" },
		func(r *TraceReference) { r.ModuleName = "Bad" },
		func(r *TraceReference) { r.ModuleName = strings.Repeat("a", 51) },
		func(r *TraceReference) { r.InstanceID = "process.identity" }, func(r *TraceReference) { r.Role = "worker" },
		func(r *TraceReference) { r.InstanceID = "secret space" }, func(r *TraceReference) { r.InstanceID = strings.Repeat("x", 101) },
		func(r *TraceReference) { r.StartedAt = time.Time{} }, func(r *TraceReference) { r.StartedAt = r.StartedAt.In(time.FixedZone("bad", 3600)) },
	} {
		bad := ref
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatalf("accepted %#v", bad)
		}
	}
}
func TestReferenceRejectsAmbiguousOrUnboundedJSON(t *testing.T) {
	raw, _ := json.Marshal(reference())
	valid := string(raw)
	cases := []string{
		`{}`, `null`, `[]`, valid + ` {}`, strings.Replace(valid, `"trace_id":`, `"trace_id":"11111111111111111111111111111111","trace_id":`, 1),
		strings.Replace(valid, `"role":"backend"`, `"role":true`, 1), strings.Replace(valid, `"role":"backend"`, `"role":"backend","tenant_id":"secret"`, 1),
		strings.Replace(valid, `"span_id":"2222222222222222",`, "", 1), strings.Replace(valid, `000000123Z`, `000000123+00:00`, 1),
		strings.Replace(valid, `000000123Z`, `0000001230Z`, 1), strings.Repeat("x", MaxReferenceBytes+1),
	}
	for _, bad := range cases {
		if _, err := ParseTraceReference([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	bad := append([]byte(nil), raw...)
	bad[10] = 0xff
	if _, err := ParseTraceReference(bad); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
}
