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
	mu       sync.Mutex
	subjects map[string]int
}

func NewTraceService(users TraceUsers, backend tracequery.Backend) *TraceService {
	return &TraceService{users: users, backend: backend, slots: make(chan struct{}, 8), subjects: map[string]int{}}
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
	anchor, e := s.users.GetRuntimeInstancesForUser(ctx, []uint{q.InstanceID}, token)
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
		ids, e = s.backend.Search(ctx, anchor[0], q.Start, q.End, q.Limit)
		if e != nil {
			return out, e
		}
	}
	if len(ids) > q.Limit {
		return out, tracequery.ErrBudget
	}
	processIDs := []string{}
	seen := map[string]bool{}
	total := 0
	for _, id := range ids {
		d, err := s.backend.Trace(ctx, id, q.Start, q.End)
		if err != nil {
			return out, err
		}
		if d.ID != id {
			return out, tracequery.ErrUpstream
		}
		if d.Start.IsZero() || d.End.Before(d.Start) || d.End.Before(q.Start) || !d.Start.Before(q.End) {
			return out, tracequery.ErrNotFound
		}
		matched := false
		total += len(d.Spans)
		if total > 1000 {
			return out, tracequery.ErrBudget
		}
		for _, v := range d.Spans {
			matched = matched || (v.Module == anchor[0].ModuleName && v.Instance == anchor[0].InstanceID)
			if !seen[v.Instance] {
				seen[v.Instance] = true
				processIDs = append(processIDs, v.Instance)
			}
		}
		if !matched {
			return out, tracequery.ErrDenied
		}
		out.Data = append(out.Data, d)
	}
	if len(processIDs) > 100 {
		return out, tracequery.ErrBudget
	}
	authorize := func() error {
		if len(processIDs) == 0 {
			rows, err := s.users.GetRuntimeInstancesForUser(ctx, []uint{q.InstanceID}, token)
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0].ID != q.InstanceID || rows[0].ModuleName != anchor[0].ModuleName || rows[0].InstanceID != anchor[0].InstanceID || rows[0].Role != anchor[0].Role {
				return tracequery.ErrDenied
			}
			return nil
		}
		rows, err := s.users.GetProcessInstancesForUser(ctx, processIDs, token)
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
