package service

import (
	"context"
	"errors"
	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/tracequery"
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
