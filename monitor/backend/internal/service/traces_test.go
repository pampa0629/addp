package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/tracequery"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type traceUsers struct {
	reads int
	deny  bool
	token string
}

func (u *traceUsers) GetRuntimeInstancesForUser(_ context.Context, ids []uint, token string) ([]client.RuntimeInstanceReference, error) {
	u.reads++
	u.token = token
	return []client.RuntimeInstanceReference{{ID: 1, ModuleName: "monitor", InstanceID: "one", Role: "backend", Status: "down"}}, nil
}
func (u *traceUsers) GetProcessInstancesForUser(_ context.Context, ids []string, token string) ([]client.RuntimeInstanceReference, error) {
	u.reads++
	if token != u.token || u.deny {
		return nil, tracequery.ErrDenied
	}
	return []client.RuntimeInstanceReference{{ID: 1, ModuleName: "monitor", InstanceID: "one", Role: "backend", Status: "down"}, {ID: 2, ModuleName: "system", InstanceID: "two", Role: "backend", Status: "down"}}, nil
}

type traceBackend struct {
	users  *traceUsers
	revoke bool
}

func (b traceBackend) Search(context.Context, client.RuntimeInstanceReference, time.Time, time.Time, int) ([]string, error) {
	return []string{"11111111111111111111111111111111"}, nil
}
func (b traceBackend) Trace(_ context.Context, id string, _, _ time.Time) (tracequery.Detail, error) {
	if b.revoke {
		b.users.deny = true
	}
	return tracequery.Detail{ID: id, Start: time.Now().Add(-time.Minute), End: time.Now().Add(-time.Minute + time.Second), Spans: []tracequery.Span{{ID: "1111111111111111", Module: "monitor", Instance: "one", Role: "backend"}, {ID: "2222222222222222", Module: "system", Instance: "two", Role: "backend"}}}, nil
}
func TestTraceHistoricalParticipantsUseCurrentUserAndRevokeClosed(t *testing.T) {
	users := &traceUsers{}
	s := NewTraceService(users, traceBackend{users: users})
	now := time.Now().UTC().Add(-time.Second)
	q := TraceQuery{InstanceID: 1, Start: now.Add(-15 * time.Minute), End: now, Limit: 20}
	out, e := s.Query(context.Background(), "user", "addp_at_user", q)
	if e != nil || len(out.Data) != 1 || users.reads != 2 {
		t.Fatal("offline trace or current user failed", out, e, users.reads)
	}
	s.backend = traceBackend{users: users, revoke: true}
	out, e = s.Query(context.Background(), "user", "addp_at_user", q)
	if !errors.Is(e, tracequery.ErrDenied) || len(out.Data) != 0 {
		t.Fatal("revoked participants leaked", out, e)
	}
}
func TestTraceQueryRejectsUnboundedWindowBeforeReading(t *testing.T) {
	u := &traceUsers{}
	s := NewTraceService(u, traceBackend{})
	now := time.Now().Add(-time.Second)
	_, e := s.Query(context.Background(), "user", "addp_at_user", TraceQuery{InstanceID: 1, Start: now.Add(-2 * time.Hour), End: now, Limit: 20})
	if !errors.Is(e, tracequery.ErrInvalid) || u.reads != 0 {
		t.Fatal(e, u.reads)
	}
}

func TestTraceJSONBudgetCountsEscapedRouteBytes(t *testing.T) {
	out := TraceResponse{Data: []tracequery.Detail{{Spans: make([]tracequery.Span, 1000)}}}
	for i := range out.Data[0].Spans {
		out.Data[0].Spans[i].Route = strings.Repeat("<", 1024)
	}
	if e := boundedTraceResponse(out); !errors.Is(e, tracequery.ErrBudget) {
		t.Fatal("escaped JSON exceeded response budget", e)
	}
	out.Data[0].Spans = out.Data[0].Spans[:1]
	if e := boundedTraceResponse(out); e != nil {
		t.Fatal(e)
	}
}

// These fixtures gate individual reads so concurrency checks do not depend on
// completing a network operation within an arbitrary latency threshold.
type concurrentTraceBackend struct {
	ids  []string
	read func(context.Context, string) (tracequery.Detail, error)
}

func (b concurrentTraceBackend) Search(context.Context, client.RuntimeInstanceReference, time.Time, time.Time, int) ([]string, error) {
	return b.ids, nil
}
func (b concurrentTraceBackend) Trace(ctx context.Context, id string, _, _ time.Time) (tracequery.Detail, error) {
	return b.read(ctx, id)
}

type concurrentTraceUsers struct {
	anchors atomic.Int32
	deny    atomic.Bool
}

func (u *concurrentTraceUsers) GetRuntimeInstancesForUser(context.Context, []uint, string) ([]client.RuntimeInstanceReference, error) {
	u.anchors.Add(1)
	return []client.RuntimeInstanceReference{{ID: 1, ModuleName: "monitor", InstanceID: "one", Role: "backend"}}, nil
}
func (u *concurrentTraceUsers) GetProcessInstancesForUser(context.Context, []string, string) ([]client.RuntimeInstanceReference, error) {
	if u.deny.Load() {
		return nil, tracequery.ErrDenied
	}
	return []client.RuntimeInstanceReference{{ModuleName: "monitor", InstanceID: "one", Role: "backend"}}, nil
}
func concurrentTraceIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("%032x", i+1)
	}
	return ids
}
func concurrentTraceDetail(id string) tracequery.Detail {
	return tracequery.Detail{ID: id, Start: time.Now().Add(-time.Minute), End: time.Now().Add(-time.Minute + time.Second), Spans: []tracequery.Span{{ID: "1111111111111111", Module: "monitor", Instance: "one", Role: "backend"}}}
}
func concurrentTraceQuery(n int) TraceQuery {
	end := time.Now().Add(-time.Second)
	return TraceQuery{InstanceID: 1, Start: end.Add(-15 * time.Minute), End: end, Limit: n}
}
func awaitTraceRead(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case id := <-started:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("bounded reads did not start")
		return ""
	}
}

func TestTraceDetailReadsOverlapFourAndKeepSearchOrder(t *testing.T) {
	ids := concurrentTraceIDs(20)
	gates := map[string]chan struct{}{}
	for _, id := range ids {
		gates[id] = make(chan struct{})
	}
	defer func() {
		for _, gate := range gates {
			select {
			case <-gate:
			default:
				close(gate)
			}
		}
	}()
	started := make(chan string, 20)
	backend := concurrentTraceBackend{ids: ids, read: func(ctx context.Context, id string) (tracequery.Detail, error) {
		started <- id
		select {
		case <-gates[id]:
			return concurrentTraceDetail(id), nil
		case <-ctx.Done():
			return tracequery.Detail{}, ctx.Err()
		}
	}}
	service := NewTraceService(&concurrentTraceUsers{}, backend)
	type response struct {
		data TraceResponse
		err  error
	}
	done := make(chan response, 1)
	go func() {
		out, err := service.Query(context.Background(), "user", "token", concurrentTraceQuery(20))
		done <- response{out, err}
	}()
	first := map[string]bool{}
	for range 4 {
		first[awaitTraceRead(t, started)] = true
	}
	for _, id := range ids[:4] {
		if !first[id] {
			t.Fatal("first four requests were not bounded", first)
		}
	}
	select {
	case id := <-started:
		t.Fatal("fifth detail started before a slot was released", id)
	default:
	}
	// Complete the fourth result first and ensure another read can start while
	// the first result is still blocked; output must nevertheless retain order.
	close(gates[ids[3]])
	if id := awaitTraceRead(t, started); id != ids[4] {
		t.Fatal("next detail", id)
	}
	for _, id := range ids {
		if id != ids[3] {
			close(gates[id])
		}
	}
	result := <-done
	if result.err != nil || len(result.data.Data) != 20 {
		t.Fatal(result.err, len(result.data.Data))
	}
	for i, d := range result.data.Data {
		if d.ID != ids[i] {
			t.Fatal("search order changed", i, d.ID)
		}
	}
	if len(service.upstream) != 0 {
		t.Fatal("upstream slots leaked")
	}
}
func TestTraceAllUpstreamsShareEightAndPrincipalLimitStillTwo(t *testing.T) {
	ids := concurrentTraceIDs(20)
	started := make(chan string, 60)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	users := &concurrentTraceUsers{}
	backend := concurrentTraceBackend{ids: ids, read: func(ctx context.Context, id string) (tracequery.Detail, error) {
		started <- id
		select {
		case <-release:
			return concurrentTraceDetail(id), nil
		case <-ctx.Done():
			return tracequery.Detail{}, ctx.Err()
		}
	}}
	service := NewTraceService(users, backend)
	done := make(chan error, 3)
	for range 2 {
		go func() {
			_, err := service.Query(context.Background(), "same", "token", concurrentTraceQuery(20))
			done <- err
		}()
	}
	for range 8 {
		awaitTraceRead(t, started)
	}
	if len(service.upstream) != 8 {
		t.Fatal("global upstream budget not shared", len(service.upstream))
	}
	if _, err := service.Query(context.Background(), "same", "token", concurrentTraceQuery(20)); !errors.Is(err, tracequery.ErrBusy) {
		t.Fatal("principal budget relaxed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, err := service.Query(ctx, "other", "token", concurrentTraceQuery(20)); done <- err }()
	for deadline := time.Now().Add(2 * time.Second); ; {
		service.mu.Lock()
		accepted := service.subjects["other"] == 1
		service.mu.Unlock()
		if accepted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("third principal was not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("shared budget wait ignored caller cancellation", err)
	}
	if users.anchors.Load() != 2 {
		t.Fatal("System authorization bypassed shared eight-call budget", users.anchors.Load())
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if len(service.upstream) != 0 || len(service.slots) != 0 {
		t.Fatal("request or upstream slots leaked")
	}
}
func TestTraceFailedParallelReadCancelsWorkersAndReturnsNoPartialData(t *testing.T) {
	ids := concurrentTraceIDs(20)
	started := make(chan string, 20)
	failure := make(chan struct{})
	var active atomic.Int32
	backend := concurrentTraceBackend{ids: ids, read: func(ctx context.Context, id string) (tracequery.Detail, error) {
		active.Add(1)
		defer active.Add(-1)
		started <- id
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
			return tracequery.Detail{}, errors.New("request deadline was extended")
		}
		if id == ids[0] {
			select {
			case <-failure:
				return tracequery.Detail{}, tracequery.ErrUpstream
			case <-ctx.Done():
				return tracequery.Detail{}, ctx.Err()
			}
		}
		<-ctx.Done()
		return tracequery.Detail{}, ctx.Err()
	}}
	service := NewTraceService(&concurrentTraceUsers{}, backend)
	type response struct {
		data TraceResponse
		err  error
	}
	done := make(chan response, 1)
	go func() {
		out, err := service.Query(context.Background(), "user", "token", concurrentTraceQuery(20))
		done <- response{out, err}
	}()
	for range 4 {
		awaitTraceRead(t, started)
	}
	close(failure)
	result := <-done
	if !errors.Is(result.err, tracequery.ErrUpstream) || len(result.data.Data) != 0 || active.Load() != 0 || len(service.upstream) != 0 {
		t.Fatal("failed read retained workers or partial response", result.err, len(result.data.Data), active.Load())
	}
}
func TestTraceParallelReadsKeepCumulativeSpanBudget(t *testing.T) {
	backend := concurrentTraceBackend{ids: concurrentTraceIDs(2), read: func(_ context.Context, id string) (tracequery.Detail, error) {
		d := concurrentTraceDetail(id)
		span := d.Spans[0]
		d.Spans = make([]tracequery.Span, 501)
		for i := range d.Spans {
			d.Spans[i] = span
			d.Spans[i].ID = fmt.Sprintf("%016x", i+1)
		}
		return d, nil
	}}
	service := NewTraceService(&concurrentTraceUsers{}, backend)
	result, err := service.Query(context.Background(), "user", "token", concurrentTraceQuery(2))
	if !errors.Is(err, tracequery.ErrBudget) || len(result.Data) != 0 || len(service.upstream) != 0 {
		t.Fatal("parallel reads bypassed aggregate budget", err, len(result.Data))
	}
}

func TestTraceParallelReadsRecheckCurrentUserBeforeReturningAnyData(t *testing.T) {
	ids := concurrentTraceIDs(20)
	users := &concurrentTraceUsers{}
	backend := concurrentTraceBackend{ids: ids, read: func(_ context.Context, id string) (tracequery.Detail, error) {
		if id == ids[len(ids)-1] {
			users.deny.Store(true)
		}
		return concurrentTraceDetail(id), nil
	}}
	service := NewTraceService(users, backend)
	out, err := service.Query(context.Background(), "user", "token", concurrentTraceQuery(20))
	if !errors.Is(err, tracequery.ErrDenied) || len(out.Data) != 0 || len(service.upstream) != 0 {
		t.Fatal("revoked current user received parallel details", err, len(out.Data))
	}
}

func TestTraceLinksAuthorizeTargetsAndBindSystemIDWithoutFetchingThem(t *testing.T) {
	users := &traceUsers{}
	reads := 0
	backend := concurrentTraceBackend{ids: concurrentTraceIDs(1), read: func(_ context.Context, id string) (tracequery.Detail, error) {
		reads++
		d := concurrentTraceDetail(id)
		d.Spans[0].Links = []tracequery.Link{{TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("b", 16), ModuleName: "system", InstanceID: "two", Role: "backend", StartedAt: time.Now().Add(-2 * time.Hour), RuntimeInstanceID: 999}}
		return d, nil
	}}
	s := NewTraceService(users, backend)
	out, err := s.Query(context.Background(), "user", "token", concurrentTraceQuery(1))
	if err != nil || reads != 1 || len(out.Data) != 1 || out.Data[0].Spans[0].Links[0].RuntimeInstanceID != 2 {
		t.Fatal("link did not use current User identity, or recursively fetched target", out, err, reads)
	}
	for _, identity := range []tracequery.Link{{ModuleName: "foreign", InstanceID: "two", Role: "backend"}, {ModuleName: "system", InstanceID: "two", Role: "ingress"}, {ModuleName: "system", InstanceID: "missing", Role: "backend"}} {
		s.backend = concurrentTraceBackend{ids: concurrentTraceIDs(1), read: func(_ context.Context, id string) (tracequery.Detail, error) {
			d := concurrentTraceDetail(id)
			d.Spans[0].Links = []tracequery.Link{identity}
			return d, nil
		}}
		out, err := s.Query(context.Background(), "user", "token", concurrentTraceQuery(1))
		if !errors.Is(err, tracequery.ErrDenied) || len(out.Data) != 0 {
			t.Fatal("unreadable causal target leaked", out, err)
		}
	}
}
func TestTraceLinksCountTowardWholeRequestInstanceBudget(t *testing.T) {
	backend := concurrentTraceBackend{ids: concurrentTraceIDs(1), read: func(_ context.Context, id string) (tracequery.Detail, error) {
		d := concurrentTraceDetail(id)
		for i := 1; i < 100; i++ {
			d.Spans = append(d.Spans, tracequery.Span{Module: "monitor", Instance: fmt.Sprintf("instance-%d", i), Role: "backend"})
		}
		d.Spans[0].Links = []tracequery.Link{{ModuleName: "system", InstanceID: "target", Role: "backend"}}
		return d, nil
	}}
	out, err := NewTraceService(&traceUsers{}, backend).Query(context.Background(), "user", "token", concurrentTraceQuery(1))
	if !errors.Is(err, tracequery.ErrBudget) || len(out.Data) != 0 {
		t.Fatal("links bypassed whole-request instance budget", out, err)
	}
}
func TestTraceLinkTargetRevocationReturnsNoPriorData(t *testing.T) {
	users := &traceUsers{}
	backend := concurrentTraceBackend{ids: concurrentTraceIDs(1), read: func(_ context.Context, id string) (tracequery.Detail, error) {
		users.deny = true
		d := concurrentTraceDetail(id)
		d.Spans[0].Links = []tracequery.Link{{ModuleName: "system", InstanceID: "two", Role: "backend"}}
		return d, nil
	}}
	out, err := NewTraceService(users, backend).Query(context.Background(), "user", "token", concurrentTraceQuery(1))
	if !errors.Is(err, tracequery.ErrDenied) || len(out.Data) != 0 {
		t.Fatal("revoked Link target leaked source detail", out, err)
	}
}
