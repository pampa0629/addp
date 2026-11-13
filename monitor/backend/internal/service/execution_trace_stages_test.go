package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/addp/monitor/internal/tracequery"
)

func stageExecution(t *testing.T) (*execution.TaskExecution, *execution.TraceReference, *execution.TraceReference) {
	t.Helper()
	item := &execution.TaskExecution{ExecutionID: "51e367c3-8663-4302-a8a4-2a31d5c1c3df", Module: execution.ModuleDevelop, TaskType: execution.TaskTypeQuery, ExecutionBoundary: execution.ExecutionBoundaryBounded, Attempt: 3}
	start := time.Now().UTC().Add(-time.Minute)
	enqueue := &execution.TraceReference{TraceID: strings.Repeat("1", 32), SpanID: strings.Repeat("2", 16), ModuleName: item.Module, Role: "backend", InstanceID: "private-instance", StartedAt: start}
	latest := *enqueue
	latest.TraceID, latest.SpanID, latest.StartedAt = strings.Repeat("3", 32), strings.Repeat("4", 16), start.Add(time.Second)
	setStageReferences(t, item, enqueue, &latest)
	return item, enqueue, &latest
}

func setStageReferences(t *testing.T, item *execution.TaskExecution, enqueue, latest *execution.TraceReference) {
	t.Helper()
	item.ExecutionConfig, item.Metadata = nil, nil
	if err := execution.FreezeEnqueueTrace(item, enqueue); err != nil {
		t.Fatal(err)
	}
	var err error
	item.Metadata, err = execution.LatestTraceMetadata(nil, latest)
	if err != nil {
		t.Fatal(err)
	}
}

func stageDetail(ref *execution.TraceReference, phase string) tracequery.Detail {
	kind, attempt := "producer", 0
	if phase != "enqueue" {
		kind, attempt = "internal", 1 // old attempt is deliberately observable.
	}
	return tracequery.Detail{ID: ref.TraceID, Spans: []tracequery.Span{{ID: ref.SpanID, Module: ref.ModuleName, Role: ref.Role, Instance: ref.InstanceID, Scope: "addp.execution", Kind: kind, Phase: phase, Attempt: attempt, Outcome: "success", Start: ref.StartedAt, End: ref.StartedAt.Add(time.Millisecond), DurationMS: 1}}}
}

func stageLoader(item *execution.TaskExecution) ExecutionTraceLoader {
	return func(context.Context) (*execution.TaskExecution, error) { return item, nil }
}

func TestExecutionStagesSafeProjectionAndSharedDeadline(t *testing.T) {
	item, enqueue, latest := stageExecution(t)
	var deadline time.Time
	calls := 0
	s := NewTraceService(nil, sampleBackend{detail: func(ctx context.Context, id string) (tracequery.Detail, error) {
		calls++
		at, _ := ctx.Deadline()
		if !at.Equal(deadline) {
			t.Fatal("Tempo reset Owner deadline")
		}
		ref, phase := enqueue, "enqueue"
		if id == latest.TraceID {
			ref, phase = latest, "terminal"
		}
		d := stageDetail(ref, phase)
		d.Spans = append(d.Spans, tracequery.Span{ID: "5555555555555555", Module: "unauthorized-child", Instance: "hidden-instance", Route: "/secret"})
		d.Spans[0].Links = []tracequery.Link{{TraceID: "hidden-link", ModuleName: "unauthorized-child", InstanceID: "hidden-instance"}}
		return d, nil
	}})
	load := func(ctx context.Context) (*execution.TaskExecution, error) {
		var ok bool
		deadline, ok = ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second || len(s.slots) != 1 || len(s.upstream) != 1 {
			t.Fatal("Owner load escaped shared admission/deadline/upstream")
		}
		return item, nil
	}
	out, err := s.ExecutionStages(t.Context(), "user", load)
	if err != nil || !out.Supported || len(out.Stages) != 2 || calls != 2 || out.Stages[0].Attempt == nil || *out.Stages[0].Attempt != 0 || *out.Stages[1].Attempt != 1 {
		t.Fatal(out, calls, err)
	}
	if out.Stages[0].State != "available" || out.Stages[1].State != "available" || len(s.slots) != 0 || len(s.upstream) != 0 {
		t.Fatal("wrong states or leaked capacity", out)
	}
	raw, _ := json.Marshal(out)
	for _, private := range []string{enqueue.TraceID, enqueue.SpanID, latest.TraceID, "private-instance", "unauthorized-child", "hidden", "secret", "trace_id", "span_id"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("private trace field escaped %s", private)
		}
	}
}

func TestExecutionStagesAvailabilityAndIndependentWindows(t *testing.T) {
	for _, mode := range []string{"unsupported", "continuous", "absent", "missing", "old_enqueue", "future", "old_latest"} {
		t.Run(mode, func(t *testing.T) {
			item, enqueue, latest := stageExecution(t)
			expected, calls := []string{"available", "available"}, 0
			switch mode {
			case "unsupported":
				item.Module = "manager"
			case "continuous":
				item.ExecutionBoundary = execution.ExecutionBoundaryContinuous
			case "absent":
				enqueue, latest = nil, nil
				expected = []string{"not_recorded", "not_recorded"}
			case "missing":
				expected = []string{"not_found", "not_found"}
			case "old_enqueue":
				enqueue.StartedAt = time.Now().UTC().Add(-73 * time.Hour)
				expected[0] = "outside_window"
			case "future":
				enqueue.StartedAt = time.Now().UTC().Add(time.Minute)
				expected[0] = "outside_window"
			case "old_latest":
				latest.StartedAt = time.Now().UTC().Add(-73 * time.Hour)
				expected[1] = "outside_window"
			}
			setStageReferences(t, item, enqueue, latest)
			s := NewTraceService(nil, stageWindowBackend{read: func(ctx context.Context, id string, start, end time.Time) (tracequery.Detail, error) {
				calls++
				ref, phase := enqueue, "enqueue"
				if id == latest.TraceID {
					ref, phase = latest, "terminal"
				}
				if !start.Equal(ref.StartedAt) || !start.Before(end) || end.Sub(start) > time.Hour || end.After(time.Now()) {
					t.Fatal("wrong anchor window")
				}
				if mode == "missing" {
					return tracequery.Detail{}, tracequery.ErrNotFound
				}
				return stageDetail(ref, phase), nil
			}})
			out, err := s.ExecutionStages(t.Context(), "user", stageLoader(item))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "unsupported" || mode == "continuous" {
				if out.Supported || len(out.Stages) != 0 || calls != 0 {
					t.Fatal(out, calls)
				}
				return
			}
			for i, want := range expected {
				got := out.Stages[i]
				if got.State != want {
					t.Fatal(out)
				}
				if want != "available" && (got.Attempt != nil || got.Start != nil || got.End != nil || got.DurationMS != nil || got.Phase != "" || got.Outcome != "") {
					t.Fatal("unavailable state has sample fields", got)
				}
			}
		})
	}
}

type stageWindowBackend struct {
	read func(context.Context, string, time.Time, time.Time) (tracequery.Detail, error)
}

func (b stageWindowBackend) Search(context.Context, client.RuntimeInstanceReference, time.Time, time.Time, int, *tracequery.HTTPFailureFilter) ([]string, error) {
	panic("stage query cannot search")
}
func (b stageWindowBackend) Trace(ctx context.Context, id string, start, end time.Time) (tracequery.Detail, error) {
	return b.read(ctx, id, start, end)
}

func TestExecutionStagesFailClosedAndDeduplicate(t *testing.T) {
	for _, mode := range []string{"owner", "malformed", "wrong_owner", "mismatch", "upstream", "same_reference", "budget_spans", "budget_bytes", "budget_instances", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			item, enqueue, latest := stageExecution(t)
			if mode == "same_reference" {
				setStageReferences(t, item, enqueue, enqueue)
			}
			if mode == "malformed" {
				item.Metadata = models.JSONMap{execution.ObservabilityKey: "private malformed"}
			}
			if mode == "wrong_owner" {
				latest.ModuleName = "system"
				setStageReferences(t, item, enqueue, latest)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			ownerErr := errors.New("owner denied")
			s := NewTraceService(nil, sampleBackend{detail: func(_ context.Context, id string) (tracequery.Detail, error) {
				calls++
				if mode == "upstream" {
					return tracequery.Detail{}, tracequery.ErrUpstream
				}
				ref, phase := enqueue, "enqueue"
				if id == latest.TraceID {
					ref, phase = latest, "terminal"
				}
				d := stageDetail(ref, phase)
				switch mode {
				case "mismatch":
					d.Spans[0].Start = d.Spans[0].Start.Add(time.Nanosecond)
				case "budget_spans":
					for range 500 {
						d.Spans = append(d.Spans, tracequery.Span{Instance: "one"})
					}
				case "budget_bytes":
					d.Spans[0].Route = strings.Repeat("x", 1100000)
				case "budget_instances":
					for i := range 51 {
						d.Spans = append(d.Spans, tracequery.Span{Instance: fmt.Sprintf("%s-%d", id, i)})
					}
				case "cancel":
					cancel()
				}
				return d, nil
			}})
			load := stageLoader(item)
			if mode == "owner" {
				load = func(context.Context) (*execution.TaskExecution, error) { return nil, ownerErr }
			}
			out, err := s.ExecutionStages(ctx, "user", load)
			if err == nil || out.ExecutionID != "" || out.Stages != nil {
				t.Fatal("error returned partial stages", out, err)
			}
			if mode == "owner" && (!errors.Is(err, ownerErr) || calls != 0) {
				t.Fatal("owner error changed or query leaked", err, calls)
			}
			if mode == "malformed" && calls != 0 {
				t.Fatal("parsed bad reference after querying")
			}
			if mode == "same_reference" && calls != 1 {
				t.Fatal("same reference queried twice", calls)
			}
			if strings.HasPrefix(mode, "budget_") && !errors.Is(err, tracequery.ErrBudget) {
				t.Fatal("not cumulative budget failure", err)
			}
			if len(s.slots) != 0 || len(s.upstream) != 0 {
				t.Fatal("failed query leaked slots")
			}
		})
	}
}

func TestExecutionStagesSharesAdmissionWithPlatformQueries(t *testing.T) {
	item, _, _ := stageExecution(t)
	s := NewTraceService(nil, nil)
	releases := []func(){}
	for i := range 8 {
		principal := fmt.Sprintf("user-%d", i)
		if i < 2 {
			principal = "same-user"
		}
		release, err := s.acquire(principal)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	for _, principal := range []string{"same-user", "other-user"} {
		called := false
		out, err := s.ExecutionStages(t.Context(), principal, func(context.Context) (*execution.TaskExecution, error) { called = true; return item, nil })
		if !errors.Is(err, tracequery.ErrBusy) || called || out.Stages != nil {
			t.Fatal("separate admission pool", err, called)
		}
	}
}

func TestExecutionStagesOwnerCancellationStopsBeforeTempo(t *testing.T) {
	item, _, _ := stageExecution(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	s := NewTraceService(nil, sampleBackend{detail: func(context.Context, string) (tracequery.Detail, error) {
		calls++
		return tracequery.Detail{}, tracequery.ErrNotFound
	}})
	out, err := s.ExecutionStages(ctx, "user", func(context.Context) (*execution.TaskExecution, error) {
		cancel()
		return item, nil
	})
	if !errors.Is(err, context.Canceled) || out.Stages != nil || calls != 0 || len(s.slots) != 0 || len(s.upstream) != 0 {
		t.Fatal("Owner cancellation was reset before Tempo", out, calls, err)
	}
}

func TestExecutionStagesPreservesZeroValuesAndCapsOldAnchorWindow(t *testing.T) {
	item, enqueue, _ := stageExecution(t)
	enqueue.StartedAt = time.Now().UTC().Add(-2 * time.Hour)
	setStageReferences(t, item, enqueue, nil)
	s := NewTraceService(nil, stageWindowBackend{read: func(_ context.Context, _ string, start, end time.Time) (tracequery.Detail, error) {
		if !start.Equal(enqueue.StartedAt) || !end.Equal(start.Add(time.Hour)) {
			t.Fatal("old anchor exceeded original one hour window", start, end)
		}
		d := stageDetail(enqueue, "enqueue")
		d.Spans[0].End, d.Spans[0].DurationMS = enqueue.StartedAt, 0
		return d, nil
	}})
	out, err := s.ExecutionStages(t.Context(), "user", stageLoader(item))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	for _, want := range []string{`"attempt":0`, `"duration_ms":0`, `"state":"not_recorded"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatal("zero value omitted", string(raw))
		}
	}
}
