package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/repository"
	"github.com/google/uuid"
)

type targetTestStore struct {
	rows   []metricsdiscovery.NodeTarget
	writes int
}

func (s *targetTestStore) Snapshot(context.Context) ([]metricsdiscovery.NodeTarget, error) {
	return s.rows, nil
}
func (s *targetTestStore) Get(_ context.Context, id string) (metricsdiscovery.NodeTarget, error) {
	for _, row := range s.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return metricsdiscovery.NodeTarget{}, repository.ErrTargetNotFound
}
func (s *targetTestStore) Mutate(_ context.Context, row metricsdiscovery.NodeTarget, create, remove bool, _ func(context.Context, []metricsdiscovery.NodeTarget) error) (metricsdiscovery.NodeTarget, error) {
	s.writes++
	if create {
		s.rows = append(s.rows, row)
	}
	return row, nil
}

type targetTestNodes struct {
	err       error
	listCalls int
	tokens    []string
}

func (n *targetTestNodes) GetHostNodeForUser(_ context.Context, id, token string) (*client.HostNodeReference, error) {
	n.tokens = append(n.tokens, token)
	return &client.HostNodeReference{NodeID: id, Version: 1}, n.err
}
func (n *targetTestNodes) AuthorizeHostNodesForUser(_ context.Context, token string) error {
	n.listCalls++
	n.tokens = append(n.tokens, token)
	return n.err
}
func TestMonitoringTargetDisabledIntentAndOwnerAuthorization(t *testing.T) {
	store, nodes := &targetTestStore{}, &targetTestNodes{}
	s := NewMonitoringTargetService(store, nodes, nil, false, nil)
	value := false
	input := MonitoringTargetInput{Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: uuid.NewString()}, MonitorKind: "host_resources", Source: metricsdiscovery.NodeSource{Type: "node_exporter", Endpoint: "https://127.0.0.1:443/metrics"}, Enabled: &value}
	row, err := s.Save(context.Background(), "", "addp_at_user", input)
	if err != nil || row.Version != 1 || store.writes != 1 {
		t.Fatalf("save disabled: %v", err)
	}
	value = true
	if _, err := s.Save(context.Background(), "", "addp_at_user", input); !errors.Is(err, metricsdiscovery.ErrDisabled) || store.writes != 1 {
		t.Fatalf("enabled save with disabled capability: %v", err)
	}
	nodes.err = &client.SystemAPIError{StatusCode: 403}
	value = false
	if _, err := s.Save(context.Background(), "", "addp_at_user", input); err == nil || store.writes != 1 {
		t.Fatal("owner denial bypassed")
	}
	for _, token := range nodes.tokens {
		if token != "addp_at_user" {
			t.Fatal("User token replaced")
		}
	}
}
func TestMonitoringTargetListEmptyAndPaginationDoNotBypassOwner(t *testing.T) {
	store, nodes := &targetTestStore{}, &targetTestNodes{}
	s := NewMonitoringTargetService(store, nodes, nil, false, nil)
	page, err := s.List(context.Background(), "addp_at_user", 1, 20)
	if err != nil || len(page.Data) != 0 || page.Data == nil || nodes.listCalls != 1 {
		t.Fatalf("empty list: %#v %v", page, err)
	}
	nodes.err = &client.SystemAPIError{StatusCode: 403}
	if _, err := s.List(context.Background(), "addp_at_user", 1, 20); err == nil {
		t.Fatal("empty inventory bypassed owner")
	}
}
