package tracequery

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/execution/observability"
)

func TestExecutionStageProjectionExactAnchorOnly(t *testing.T) {
	d, err := Decode(strings.Repeat("1", 32), executionFixture())
	if err != nil {
		t.Fatal(err)
	}
	s := d.Spans[0]
	ref := observability.TraceReference{TraceID: d.ID, SpanID: s.ID, ModuleName: s.Module, Role: s.Role, InstanceID: s.Instance, StartedAt: s.Start}
	// Other spans and causal references may be valid platform data but are
	// never tenant execution-ownership proofs or public stage fields.
	d.Spans = append(d.Spans, Span{ID: "2222222222222222", ParentID: s.ID, Module: "private-child", Route: "/private-route"})
	v, err := ProjectExecutionStage(d, ref, "latest")
	if err != nil || v.Attempt != 2 || v.Phase != "terminal" || v.Outcome != "failed" || !v.Start.Equal(s.Start) {
		t.Fatal(v, err)
	}
	raw, _ := json.Marshal(v)
	for _, secret := range []string{d.ID, s.ID, s.Instance, "private-child", "private-route", "Links", "ParentID"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("private trace field escaped: %s", secret)
		}
	}
	for name, mutate := range map[string]func(*observability.TraceReference){
		"trace":    func(r *observability.TraceReference) { r.TraceID = strings.Repeat("2", 32) },
		"span":     func(r *observability.TraceReference) { r.SpanID = strings.Repeat("3", 16) },
		"module":   func(r *observability.TraceReference) { r.ModuleName = "develop" },
		"role":     func(r *observability.TraceReference) { r.Role = "ingress" },
		"instance": func(r *observability.TraceReference) { r.InstanceID = "other" },
		"start":    func(r *observability.TraceReference) { r.StartedAt = r.StartedAt.Add(time.Nanosecond) },
	} {
		t.Run(name, func(t *testing.T) {
			wrong := ref
			mutate(&wrong)
			if _, err := ProjectExecutionStage(d, wrong, "latest"); !errors.Is(err, ErrUpstream) {
				t.Fatal("wrong binding accepted", err)
			}
		})
	}
	for _, mutate := range []func(*Span){
		func(s *Span) { s.Scope = "addp.http" },
		func(s *Span) { s.ParentID = "2222222222222222" },
	} {
		wrong := d
		wrong.Spans = append([]Span(nil), d.Spans...)
		mutate(&wrong.Spans[0])
		if _, err := ProjectExecutionStage(wrong, ref, "latest"); !errors.Is(err, ErrUpstream) {
			t.Fatal("non execution root accepted", err)
		}
	}
	if _, err := ProjectExecutionStage(d, ref, "enqueue"); !errors.Is(err, ErrUpstream) {
		t.Fatal("terminal substituted for enqueue", err)
	}
	d.Spans[0].Phase, d.Spans[0].Attempt = "enqueue", 0
	if _, err := ProjectExecutionStage(d, ref, "latest"); !errors.Is(err, ErrUpstream) {
		t.Fatal("enqueue substituted for latest", err)
	}
}
