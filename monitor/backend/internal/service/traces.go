package service

import (
	"context"
	"encoding/json"
	"github.com/addp/common/client"
	"github.com/addp/common/httptrace"
	"github.com/addp/monitor/internal/tracequery"
	"sync"
	"time"
)

type TraceUsers interface {
	UserRuntimeInstanceReader
	GetProcessInstancesForUser(context.Context, []string, string) ([]client.RuntimeInstanceReference, error)
}
type TraceService struct {
	users    TraceUsers
	backend  tracequery.Backend
	slots    chan struct{}
	upstream chan struct{}
	mu       sync.Mutex
	subjects map[string]int
}

func NewTraceService(users TraceUsers, backend tracequery.Backend) *TraceService {
	return &TraceService{users: users, backend: backend, slots: make(chan struct{}, 8), upstream: make(chan struct{}, 8), subjects: map[string]int{}}
}

type TraceQuery struct {
	InstanceID uint
	ID         string
	Start, End time.Time
	Limit      int
}
type TraceResponse struct {
	Data          []tracequery.Detail `json:"data"`
	Start         time.Time           `json:"start"`
	End           time.Time           `json:"end"`
	QueriedAt     time.Time           `json:"queried_at"`
	PolicyVersion int                 `json:"policy_version"`
	Coverage      string              `json:"coverage"`
}

func (s *TraceService) Query(ctx context.Context, principal, token string, q TraceQuery) (TraceResponse, error) {
	now := time.Now().UTC()
	out := TraceResponse{Data: []tracequery.Detail{}, Start: q.Start, End: q.End, QueriedAt: now, PolicyVersion: tracequery.Definition().Version, Coverage: "sampled"}
	if q.InstanceID == 0 || principal == "" || q.Start.IsZero() || !q.Start.Before(q.End) || q.End.Sub(q.Start) > time.Hour || q.End.After(now) || q.Start.Before(now.Add(-time.Duration(tracequery.Definition().RetentionHours)*time.Hour)) || q.Limit < 1 || q.Limit > 100 || (q.ID != "" && !httptrace.ParseTraceID(q.ID)) {
		return out, tracequery.ErrInvalid
	}
	if s == nil || s.users == nil || s.backend == nil {
		return out, tracequery.ErrUpstream
	}
	s.mu.Lock()
	if s.subjects[principal] >= 2 {
		s.mu.Unlock()
		return out, tracequery.ErrBusy
	}
	s.subjects[principal]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.subjects[principal]--
		if s.subjects[principal] == 0 {
			delete(s.subjects, principal)
		}
		s.mu.Unlock()
	}()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return out, tracequery.ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var anchor []client.RuntimeInstanceReference
	e := s.callUpstream(ctx, func() error {
		var err error
		anchor, err = s.users.GetRuntimeInstancesForUser(ctx, []uint{q.InstanceID}, token)
		return err
	})
	if e != nil {
		return out, e
	}
	if len(anchor) != 1 || anchor[0].ID != q.InstanceID {
		return out, tracequery.ErrDenied
	}
	if anchor[0].Role != "backend" && anchor[0].Role != "ingress" {
		return out, tracequery.ErrInvalid
	}
	ids := []string{q.ID}
	if q.ID == "" {
		e = s.callUpstream(ctx, func() error {
			var err error
			ids, err = s.backend.Search(ctx, anchor[0], q.Start, q.End, q.Limit)
			return err
		})
		if e != nil {
			return out, e
		}
	}
	if len(ids) > q.Limit {
		return out, tracequery.ErrBudget
	}
	details, processIDs, e := s.readDetails(ctx, anchor[0], ids, q.Start, q.End)
	if e != nil {
		return out, e
	}
	out.Data = details

	authorize := func() error {
		if len(processIDs) == 0 {
			var rows []client.RuntimeInstanceReference
			err := s.callUpstream(ctx, func() error {
				var e error
				rows, e = s.users.GetRuntimeInstancesForUser(ctx, []uint{q.InstanceID}, token)
				return e
			})
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0].ID != q.InstanceID || rows[0].ModuleName != anchor[0].ModuleName || rows[0].InstanceID != anchor[0].InstanceID || rows[0].Role != anchor[0].Role {
				return tracequery.ErrDenied
			}
			return nil
		}
		var rows []client.RuntimeInstanceReference
		err := s.callUpstream(ctx, func() error {
			var e error
			rows, e = s.users.GetProcessInstancesForUser(ctx, processIDs, token)
			return e
		})
		if err != nil {
			return err
		}
		allowed := map[string]client.RuntimeInstanceReference{}
		for _, v := range rows {
			allowed[v.ModuleName+"/"+v.InstanceID] = v
		}
		for _, d := range out.Data {
			for _, span := range d.Spans {
				v, ok := allowed[span.Module+"/"+span.Instance]
				if !ok || v.Role != span.Role {
					return tracequery.ErrDenied
				}
			}
		}
		return nil
	}
	if e = authorize(); e != nil {
		return TraceResponse{}, e
	}
	if ctx.Err() != nil {
		return TraceResponse{}, ctx.Err()
	}
	if err := boundedTraceResponse(out); err != nil {
		return TraceResponse{}, err
	}
	return out, nil
}
func boundedTraceResponse(out TraceResponse) error {
	data, err := json.Marshal(out)
	if err != nil || len(data) > 2<<20 {
		return tracequery.ErrBudget
	}
	return nil
}

// System authorization and Tempo calls share the same per-Monitor budget.
// Waiting for capacity consumes the original request deadline, never a new one.
func (s *TraceService) callUpstream(ctx context.Context, call func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.upstream <- struct{}{}:
		defer func() { <-s.upstream }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return call()
}

// Retain search order while overlapping independent I/O. The result channel
// and workers are bounded; cumulative budgets cancel outstanding reads early.
func (s *TraceService) readDetails(parent context.Context, anchor client.RuntimeInstanceReference, ids []string, start, end time.Time) ([]tracequery.Detail, []string, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	type result struct {
		index  int
		detail tracequery.Detail
		err    error
	}
	jobs := make(chan int, len(ids))
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	results := make(chan result, 4)
	var workers sync.WaitGroup
	for range min(4, len(ids)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				var d tracequery.Detail
				err := s.callUpstream(ctx, func() error {
					var e error
					d, e = s.backend.Trace(ctx, ids[i], start, end)
					return e
				})
				select {
				case results <- result{i, d, err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() { workers.Wait(); close(results) }()
	details := make([]tracequery.Detail, len(ids))
	processIDs := []string{}
	seen := map[string]bool{}
	spans, completed := 0, 0
	var failure error
	fail := func(err error) { failure = err; cancel() }
	for r := range results {
		if failure != nil {
			continue
		}
		if r.err != nil {
			fail(r.err)
			continue
		}
		d := r.detail
		if d.ID != ids[r.index] {
			fail(tracequery.ErrUpstream)
			continue
		}
		if d.Start.IsZero() || d.End.Before(d.Start) || d.End.Before(start) || !d.Start.Before(end) {
			fail(tracequery.ErrNotFound)
			continue
		}
		spans += len(d.Spans)
		if spans > 1000 {
			fail(tracequery.ErrBudget)
			continue
		}
		matched := false
		for _, v := range d.Spans {
			matched = matched || (v.Module == anchor.ModuleName && v.Instance == anchor.InstanceID)
			if !seen[v.Instance] {
				seen[v.Instance] = true
				processIDs = append(processIDs, v.Instance)
			}
		}
		if len(processIDs) > 100 {
			fail(tracequery.ErrBudget)
			continue
		}
		if !matched {
			fail(tracequery.ErrDenied)
			continue
		}
		details[r.index] = d
		completed++
	}
	// Closing results means every worker has stopped and released its slot.
	if failure != nil {
		return nil, nil, failure
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if completed != len(ids) {
		return nil, nil, tracequery.ErrUpstream
	}
	return details, processIDs, nil
}
