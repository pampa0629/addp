package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/tracequery"
)

type TraceSampleState struct {
	InstanceID uint   `json:"instance_id"`
	State      string `json:"state"`
}
type TraceSamplesResponse struct {
	Data          []TraceSampleState `json:"data"`
	Start         time.Time          `json:"start"`
	End           time.Time          `json:"end"`
	QueriedAt     time.Time          `json:"queried_at"`
	PolicyVersion int                `json:"policy_version"`
	Coverage      string             `json:"coverage"`
}

// The page shares one cumulative projection budget, including rejected candidates.
type traceSampleBudget struct {
	mu           sync.Mutex
	spans, bytes int
	instances    map[string]bool
	exhausted    bool
}

func (b *traceSampleBudget) consume(detail tracequery.Detail) error {
	raw, err := json.Marshal(detail)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spans += len(detail.Spans)
	b.bytes += len(raw)
	if b.instances == nil {
		b.instances = map[string]bool{}
	}
	for _, span := range detail.Spans {
		b.instances[span.Instance] = true
		for _, link := range span.Links {
			b.instances[link.InstanceID] = true
		}
	}
	b.exhausted = b.exhausted || err != nil || b.spans > 1000 || b.bytes > 2<<20 || len(b.instances) > 100
	if b.exhausted {
		return tracequery.ErrBudget
	}
	return nil
}

func (s *TraceService) Samples(ctx context.Context, principal, token string, ids []uint, start, end time.Time) (TraceSamplesResponse, error) {
	now := time.Now().UTC()
	out := TraceSamplesResponse{Data: make([]TraceSampleState, len(ids)), Start: start, End: end, QueriedAt: now, PolicyVersion: tracequery.Definition().Version, Coverage: "sampled"}
	if principal == "" || len(ids) < 1 || len(ids) > 10 || start.IsZero() || !start.Before(end) || end.Sub(start) > time.Hour || end.After(now) || start.Before(now.Add(-time.Duration(tracequery.Definition().RetentionHours)*time.Hour)) {
		return TraceSamplesResponse{}, tracequery.ErrInvalid
	}
	seen := map[uint]bool{}
	for i, id := range ids {
		if id == 0 || seen[id] {
			return TraceSamplesResponse{}, tracequery.ErrInvalid
		}
		seen[id] = true
		out.Data[i] = TraceSampleState{InstanceID: id, State: "unavailable"}
	}
	if s == nil || s.users == nil || s.backend == nil {
		return TraceSamplesResponse{}, tracequery.ErrUpstream
	}
	release, err := s.acquire(principal)
	if err != nil {
		return TraceSamplesResponse{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// The same User authorizes the complete page before any sample search and again
	// before returning. The ordinary query verifies every candidate participant.
	readAnchors := func() (map[uint]client.RuntimeInstanceReference, error) {
		var rows []client.RuntimeInstanceReference
		err := s.callUpstream(ctx, func() error {
			var e error
			rows, e = s.users.GetRuntimeInstancesForUser(ctx, ids, token)
			return e
		})
		if err != nil {
			return nil, err
		}
		anchors := make(map[uint]client.RuntimeInstanceReference, len(rows))
		for _, row := range rows {
			if !seen[row.ID] || anchors[row.ID].ID != 0 || (row.Role != "backend" && row.Role != "ingress") {
				return nil, tracequery.ErrDenied
			}
			anchors[row.ID] = row
		}
		if len(anchors) != len(ids) {
			return nil, tracequery.ErrDenied
		}
		return anchors, nil
	}
	anchors, err := readAnchors()
	if err != nil {
		return TraceSamplesResponse{}, err
	}
	workCtx, stop := context.WithCancel(ctx)
	defer stop()
	type result struct {
		index    int
		response TraceResponse
		err      error
	}
	jobs := make(chan int, len(ids))
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	results := make(chan result, 2)
	budget := &traceSampleBudget{}
	var workers sync.WaitGroup
	for range min(2, len(ids)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if workCtx.Err() != nil {
					return
				}
				q := TraceQuery{InstanceID: ids[index], Start: start, End: end, Limit: 1}
				value, e := s.query(workCtx, token, q, anchors[ids[index]], now, budget)
				select {
				case results <- result{index, value, e}:
				case <-workCtx.Done():
					return
				}
			}
		}()
	}
	go func() { workers.Wait(); close(results) }()
	var denied error
	for item := range results {
		if denied != nil {
			continue
		}
		if item.err != nil {
			status, isSystem := client.SystemAPIStatusCode(item.err)
			if errors.Is(item.err, tracequery.ErrDenied) || (isSystem && (status == 401 || status == 403 || status == 404)) {
				denied = item.err
				stop()
			} else if errors.Is(item.err, tracequery.ErrBudget) {
				stop()
			}
			continue
		}
		if len(item.response.Data) > 0 {
			out.Data[item.index].State = "has_samples"
		} else {
			out.Data[item.index].State = "not_found"
		}
	}
	if denied != nil {
		return TraceSamplesResponse{}, denied
	}
	if ctx.Err() != nil {
		return TraceSamplesResponse{}, ctx.Err()
	}
	current, err := readAnchors()
	if err != nil {
		return TraceSamplesResponse{}, err
	}
	for id, anchor := range anchors {
		row := current[id]
		if row.ModuleName != anchor.ModuleName || row.InstanceID != anchor.InstanceID || row.Role != anchor.Role {
			return TraceSamplesResponse{}, tracequery.ErrDenied
		}
	}
	if ctx.Err() != nil {
		return TraceSamplesResponse{}, ctx.Err()
	}
	return out, nil
}
