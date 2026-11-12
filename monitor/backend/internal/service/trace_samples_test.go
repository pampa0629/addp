package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/tracequery"
)

type sampleUsers struct {
	reads            atomic.Int32
	revoke           atomic.Bool
	denyParticipants atomic.Bool
}

func (u *sampleUsers) GetRuntimeInstancesForUser(_ context.Context, ids []uint, token string) ([]client.RuntimeInstanceReference, error) {
	u.reads.Add(1)
	if token != "addp_at_user" || u.revoke.Load() {
		return nil, tracequery.ErrDenied
	}
	rows := []client.RuntimeInstanceReference{}
	for _, id := range ids {
		rows = append(rows, client.RuntimeInstanceReference{ID: id, ModuleName: "monitor", InstanceID: fmt.Sprint(id), Role: "backend"})
	}
	return rows, nil
}
func (u *sampleUsers) GetProcessInstancesForUser(_ context.Context, ids []string, token string) ([]client.RuntimeInstanceReference, error) {
	if token != "addp_at_user" || u.denyParticipants.Load() {
		return nil, tracequery.ErrDenied
	}
	rows := []client.RuntimeInstanceReference{}
	for _, id := range ids {
		rows = append(rows, client.RuntimeInstanceReference{ID: 1, ModuleName: "monitor", InstanceID: id, Role: "backend"})
	}
	return rows, nil
}

type sampleBackend struct {
	search func(context.Context, client.RuntimeInstanceReference) ([]string, error)
	detail func(context.Context, string) (tracequery.Detail, error)
}

func (b sampleBackend) Search(ctx context.Context, row client.RuntimeInstanceReference, _, _ time.Time, limit int, filter *tracequery.HTTPFailureFilter) ([]string, error) {
	if limit != 1 || filter != nil {
		panic("sample check escaped one-candidate search")
	}
	return b.search(ctx, row)
}
func (b sampleBackend) Trace(ctx context.Context, id string, _, _ time.Time) (tracequery.Detail, error) {
	return b.detail(ctx, id)
}
func sampleDetail(id string) tracequery.Detail {
	d := concurrentTraceDetail(id)
	d.Spans[0].Instance = id
	return d
}
func sampleWindow() (time.Time, time.Time) {
	end := time.Now().UTC().Add(-time.Second)
	return end.Add(-15 * time.Minute), end
}

func TestTraceSamplesMixedStatesVerifyCandidatesAndCurrentPage(t *testing.T) {
	users := &sampleUsers{}
	var searches atomic.Int32
	backend := sampleBackend{
		search: func(ctx context.Context, row client.RuntimeInstanceReference) ([]string, error) {
			searches.Add(1)
			if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
				t.Error("unbounded batch deadline")
			}
			switch row.ID {
			case 1, 4:
				return []string{row.InstanceID}, nil
			case 3:
				return nil, tracequery.ErrUpstream
			default:
				return []string{}, nil
			}
		},
		detail: func(_ context.Context, id string) (tracequery.Detail, error) {
			if id == "4" {
				return tracequery.Detail{}, tracequery.ErrNotFound
			}
			return sampleDetail(id), nil
		},
	}
	service := NewTraceService(users, backend)
	start, end := sampleWindow()
	ids := []uint{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	result, err := service.Samples(t.Context(), "user", "addp_at_user", ids, start, end)
	if err != nil || len(result.Data) != 10 || searches.Load() != 10 || result.Start != start || result.End != end {
		t.Fatalf("bad batch: %+v %v", result, err)
	}
	expected := []string{"has_samples", "not_found", "unavailable", "unavailable", "not_found", "not_found", "not_found", "not_found", "not_found", "not_found"}
	for i, row := range result.Data {
		if row.InstanceID != ids[i] || row.State != expected[i] {
			t.Fatalf("wrong row: %+v", row)
		}
	}
	users.denyParticipants.Store(true)
	result, err = service.Samples(t.Context(), "user", "addp_at_user", []uint{1}, start, end)
	if !errors.Is(err, tracequery.ErrDenied) || len(result.Data) != 0 {
		t.Fatalf("denied candidate leaked: %+v %v", result, err)
	}
	users.denyParticipants.Store(false)
	backend.search = func(context.Context, client.RuntimeInstanceReference) ([]string, error) {
		users.revoke.Store(true)
		return []string{}, nil
	}
	service.backend = backend
	result, err = service.Samples(t.Context(), "user", "addp_at_user", []uint{2}, start, end)
	if !errors.Is(err, tracequery.ErrDenied) || len(result.Data) != 0 {
		t.Fatalf("revocation leaked an empty result: %+v %v", result, err)
	}
}

func TestTraceSamplesBoundPageConcurrencyAndReleaseOnCancellation(t *testing.T) {
	started := make(chan struct{}, 10)
	var active, peak atomic.Int32
	backend := sampleBackend{search: func(ctx context.Context, _ client.RuntimeInstanceReference) ([]string, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	service := NewTraceService(&sampleUsers{}, backend)
	start, end := sampleWindow()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.Samples(ctx, "user", "addp_at_user", []uint{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, start, end)
		done <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("two workers did not start")
		}
	}
	if len(service.slots) != 1 || service.subjects["user"] != 1 || len(service.upstream) != 2 {
		t.Fatal("batch bypassed shared query/upstream quotas")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if active.Load() != 0 || peak.Load() != 2 || len(service.slots) != 0 || len(service.upstream) != 0 || len(service.subjects) != 0 {
		t.Fatal("batch workers or quota leaked")
	}
}

func TestTraceSamplesRejectInvalidInputsAndShareAdmission(t *testing.T) {
	users := &sampleUsers{}
	service := NewTraceService(users, sampleBackend{})
	start, end := sampleWindow()
	for _, ids := range [][]uint{nil, {0}, {1, 1}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}} {
		if _, err := service.Samples(t.Context(), "user", "addp_at_user", ids, start, end); !errors.Is(err, tracequery.ErrInvalid) {
			t.Fatal(ids, err)
		}
	}
	for _, bounds := range [][2]time.Time{{end, start}, {start.Add(-2 * time.Hour), end}, {start, time.Now().Add(time.Hour)}, {start.Add(-73 * time.Hour), end}} {
		if _, err := service.Samples(t.Context(), "user", "addp_at_user", []uint{1}, bounds[0], bounds[1]); !errors.Is(err, tracequery.ErrInvalid) {
			t.Fatal(bounds, err)
		}
	}
	service.subjects["user"] = 2
	if _, err := service.Samples(t.Context(), "user", "addp_at_user", []uint{1}, start, end); !errors.Is(err, tracequery.ErrBusy) {
		t.Fatal(err)
	}
	delete(service.subjects, "user")
	for range 8 {
		service.slots <- struct{}{}
	}
	if _, err := service.Samples(t.Context(), "user", "addp_at_user", []uint{1}, start, end); !errors.Is(err, tracequery.ErrBusy) || len(service.subjects) != 0 {
		t.Fatal("global admission leaked", err)
	}
	if users.reads.Load() != 0 {
		t.Fatal("rejected batch performed upstream work")
	}
}

func TestTraceSamplesShareSpanAndEscapedJSONBudgets(t *testing.T) {
	for _, largeJSON := range []bool{false, true} {
		budget := &traceSampleBudget{}
		detail := sampleDetail("1")
		detail.Spans = make([]tracequery.Span, 501)
		if largeJSON {
			detail.Spans = detail.Spans[:300]
			for i := range detail.Spans {
				detail.Spans[i].Route = strings.Repeat("<", 1024)
			}
		}
		if err := budget.consume(detail); err != nil {
			t.Fatal(err)
		}
		if err := budget.consume(detail); !errors.Is(err, tracequery.ErrBudget) {
			t.Fatal("batch received a fresh projection budget", err)
		}
		if err := budget.consume(sampleDetail("2")); !errors.Is(err, tracequery.ErrBudget) {
			t.Fatal("exhausted budget reopened", err)
		}
	}
}

func TestTraceSamplesShareParticipantBudgetIncludingLinks(t *testing.T) {
	budget := &traceSampleBudget{}
	detail := sampleDetail("1")
	for i := range 99 {
		detail.Spans[0].Links = append(detail.Spans[0].Links, tracequery.Link{InstanceID: fmt.Sprint(i + 2)})
	}
	if err := budget.consume(detail); err != nil {
		t.Fatal(err)
	}
	if err := budget.consume(sampleDetail("100")); err != nil {
		t.Fatal("duplicate participant counted twice", err)
	}
	if err := budget.consume(sampleDetail("101")); !errors.Is(err, tracequery.ErrBudget) {
		t.Fatal("batch received a fresh participant budget", err)
	}
}

func TestTraceSamplesUnreadableCandidateNeverMeansNoSamples(t *testing.T) {
	for _, kind := range []string{"missing", "wrong_id", "empty", "outside_window"} {
		t.Run(kind, func(t *testing.T) {
			start, end := sampleWindow()
			backend := sampleBackend{
				search: func(context.Context, client.RuntimeInstanceReference) ([]string, error) { return []string{"1"}, nil },
				detail: func(context.Context, string) (tracequery.Detail, error) {
					d := sampleDetail("1")
					switch kind {
					case "missing":
						return d, tracequery.ErrNotFound
					case "wrong_id":
						d.ID = "2"
					case "empty":
						d.Spans = nil
					case "outside_window":
						d.Start, d.End = end.Add(time.Second), end.Add(2*time.Second)
					}
					return d, nil
				},
			}
			result, err := NewTraceService(&sampleUsers{}, backend).Samples(t.Context(), "user", "addp_at_user", []uint{1}, start, end)
			if err != nil || len(result.Data) != 1 || result.Data[0].State != "unavailable" {
				t.Fatalf("unreadable sample became empty: %+v %v", result, err)
			}
		})
	}
}
