package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
)

type resourcePolicies struct{ row models.ResourceQueryPolicy }

func (s *resourcePolicies) Get(context.Context) (models.ResourceQueryPolicy, error) {
	return s.row, nil
}
func (s *resourcePolicies) Save(_ context.Context, v models.ResourceQueryPolicy, expected uint64) (models.ResourceQueryPolicy, error) {
	if expected != s.row.Version {
		return v, resourcequery.ErrConflict
	}
	v.Version = expected + 1
	s.row = v
	return v, nil
}

type resourceNodes struct {
	enabled bool
	tokens  []string
	err     error
}

func (n *resourceNodes) GetHostNodeForUser(_ context.Context, id, token string) (*client.HostNodeReference, error) {
	n.tokens = append(n.tokens, token)
	return &client.HostNodeReference{NodeID: id, Version: 3, Enabled: n.enabled}, n.err
}
func (n *resourceNodes) AuthorizeHostNodesForUser(context.Context, string) error { return n.err }

type resourceBackend struct {
	validAge    *time.Duration
	evidence    *resourcequery.Collection
	calls       int
	collections int
	scope       resourcequery.Scope
	err         error
}

func (b *resourceBackend) Collection(_ context.Context, scope resourcequery.Scope, _ time.Time, _ resourcequery.Budget) (resourcequery.Collection, error) {
	b.collections++
	b.scope = scope
	if b.evidence != nil {
		return *b.evidence, b.err
	}
	return resourcequery.Collection{State: "no_sample", Filesystem: "unknown", Network: "unknown"}, b.err
}
func (b *resourceBackend) Query(_ context.Context, p resourcequery.Plan, s resourcequery.Scope, _ resourcequery.Budget) ([]resourcequery.Series, error) {
	b.calls++
	b.scope = s
	if b.err != nil {
		return nil, b.err
	}
	rows := resourcequery.Empty(p, "no_data")
	if b.validAge != nil {
		stamp := p.End.Add(-*b.validAge)
		value := 1.0
		rows[0].Points[0].Value = &value
		rows[0].Points[0].SampledAt = &stamp
		rows[0].Points[0].DataState = "valid"
	}
	return rows, nil
}
func resourceSourcePolicy(t *testing.T) *metricsdiscovery.SourcePolicy {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	p, e := metricsdiscovery.NewSourcePolicy([]netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, []uint16{9100}, x509.NewCertPool(), tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestResourceQueryCurrentUserScopeStoppedTargetsAndHotBudget(t *testing.T) {
	node := uuid.NewString()
	nodes := &resourceNodes{enabled: true}
	backend := &resourceBackend{}
	targets := &targetTestStore{rows: []metricsdiscovery.NodeTarget{{ID: uuid.NewString(), Version: 2, Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: node}, MonitorKind: "host_resources", Source: metricsdiscovery.NodeSource{Type: "node_exporter", Endpoint: "https://127.0.0.1:9100/metrics"}, Enabled: true}}}
	policies := &resourcePolicies{row: models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget()}}
	svc := NewResourceObservationService(policies, targets, nodes, backend, true, resourceSourcePolicy(t))
	query := func() (ResourceObservationResponse, error) {
		return svc.Query(context.Background(), "user-1", node, "addp_at_current_user", []string{"node.memory.used_percent"}, time.Time{}, time.Time{}, false, nil)
	}
	r, e := query()
	if e != nil || backend.calls != 1 || backend.collections != 1 || r.Collection.State != "no_sample" || backend.scope.NodeID != node || backend.scope.Instance != "127.0.0.1:9100" || r.TargetSavedVersion != 2 || r.NodeVersion != 3 || r.Series[0].Points[0].DataState != "no_data" {
		t.Fatalf("query=%+v err=%v", r, e)
	}
	nodes.enabled = false
	r, e = query()
	if e != nil || backend.calls != 1 || backend.collections != 1 || r.Collection.State != "not_connected" || r.Series[0].Points[0].DataState != "not_connected" {
		t.Fatal("stopped node queried historical samples")
	}
	nodes.enabled = true
	targets.rows[0].Enabled = false
	r, e = query()
	if e != nil || backend.calls != 1 || backend.collections != 1 || r.Collection.State != "not_connected" || r.Series[0].Points[0].DataState != "not_connected" {
		t.Fatal("stopped target queried")
	}
	nodes.err = &client.SystemAPIError{StatusCode: 403}
	if _, e = query(); e == nil || backend.calls != 1 || backend.collections != 1 {
		t.Fatal("owner denial bypassed")
	}
	nodes.err = nil
	for _, token := range nodes.tokens {
		if token != "addp_at_current_user" {
			t.Fatal("user token replaced")
		}
	}
	b := resourcequery.DefaultBudget()
	b.MaxMetrics = 1
	updated, e := svc.UpdatePolicy(context.Background(), ResourceQueryPolicyInput{Budget: b}, 1)
	if e != nil || updated.PendingRestart || updated.Version != 1 {
		t.Fatal(updated, e)
	}
	if _, e := svc.Query(context.Background(), "user-1", node, "token", []string{"node.memory.used_percent", "node.load.average_1m"}, time.Time{}, time.Time{}, false, nil); !errors.Is(e, resourcequery.ErrBudget) {
		t.Fatal("hot budget not applied", e)
	}
	svc.enabled = false
	if _, e = query(); !errors.Is(e, metricsdiscovery.ErrDisabled) {
		t.Fatal(e)
	}
	svc.enabled = true
	svc.backend = nil
	if _, e = query(); !errors.Is(e, metricsdiscovery.ErrUnconfigured) {
		t.Fatal(e)
	}
	svc.backend = backend
	targets.rows[0].Enabled = true
	age := 59 * time.Second
	backend.validAge = &age
	clock := time.Now().UTC().Truncate(time.Second)
	clockCalls := 0
	svc.now = func() time.Time {
		clockCalls++
		if clockCalls%2 == 1 {
			return clock
		}
		return clock.Add(2 * time.Second)
	}
	r, e = query()
	if e != nil || r.Series[0].Points[0].DataState != "stale" {
		t.Fatal("instant freshness ignored query completion time", r, e)
	}
	clockCalls = 0
	svc.now = func() time.Time {
		clockCalls++
		if clockCalls%2 == 1 {
			return clock
		}
		return clock.Add(-time.Second)
	}
	if _, e = query(); !errors.Is(e, resourcequery.ErrUnavailable) {
		t.Fatal("clock moved backwards but query claimed success", e)
	}
	svc.now = time.Now
	backend.validAge = nil
	targets.rows[0].Enabled = true
	backend.err = resourcequery.ErrUnavailable
	if r, e = query(); !errors.Is(e, resourcequery.ErrUnavailable) || r.QueriedAt != (time.Time{}) {
		t.Fatal("backend error claimed successful query", e)
	}
}

type blockingResourcePolicies struct {
	entered chan struct{}
	wait    chan struct{}
}

func (p *blockingResourcePolicies) Get(ctx context.Context) (models.ResourceQueryPolicy, error) {
	p.entered <- struct{}{}
	select {
	case <-p.wait:
		return models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget()}, nil
	case <-ctx.Done():
		return models.ResourceQueryPolicy{}, ctx.Err()
	}
}
func (p *blockingResourcePolicies) Save(context.Context, models.ResourceQueryPolicy, uint64) (models.ResourceQueryPolicy, error) {
	return models.ResourceQueryPolicy{}, resourcequery.ErrInvalid
}
func TestResourceConcurrencyAdmissionPrecedesConfigurationRead(t *testing.T) {
	policies := &blockingResourcePolicies{entered: make(chan struct{}, 8), wait: make(chan struct{})}
	service := NewResourceObservationService(policies, nil, nil, nil, false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 8)
	node := uuid.NewString()
	for i := 0; i < 8; i++ {
		go func(i int) {
			_, e := service.Query(ctx, fmt.Sprint(i), node, "user", []string{"node.memory.used_percent"}, time.Time{}, time.Time{}, false, nil)
			done <- e
		}(i)
	}
	for i := 0; i < 8; i++ {
		select {
		case <-policies.entered:
		case <-time.After(time.Second):
			t.Fatal("configuration read did not start")
		}
	}
	_, e := service.Query(context.Background(), "extra", node, "user", []string{"node.memory.used_percent"}, time.Time{}, time.Time{}, false, nil)
	if !errors.Is(e, resourcequery.ErrBusy) {
		t.Fatal("configuration queued before admission", e)
	}
	cancel()
	for i := 0; i < 8; i++ {
		if e := <-done; !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	}
}

func TestResourceQueryExpiryClearsAllCollectorEvidence(t *testing.T) {
	node := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Second)
	sampled := now.Add(-59 * time.Second)
	backend := &resourceBackend{evidence: &resourcequery.Collection{State: "collecting", SampledAt: &sampled, Filesystem: "available", Network: "available"}}
	targets := &targetTestStore{rows: []metricsdiscovery.NodeTarget{{ID: uuid.NewString(), Version: 1, Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: node}, MonitorKind: "host_resources", Source: metricsdiscovery.NodeSource{Type: "node_exporter", Endpoint: "https://127.0.0.1:9100/metrics"}, Enabled: true}}}
	svc := NewResourceObservationService(&resourcePolicies{row: models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget()}}, targets, &resourceNodes{enabled: true}, backend, true, resourceSourcePolicy(t))
	calls := 0
	svc.now = func() time.Time {
		calls++
		if calls == 1 {
			return now
		}
		return now.Add(2 * time.Second)
	}
	result, err := svc.Query(context.Background(), "user", node, "addp_at_current_user", []string{"node.memory.used_percent"}, time.Time{}, time.Time{}, false, nil)
	if err != nil || result.Collection.State != "stale" || result.Collection.Filesystem != "unknown" || result.Collection.Network != "unknown" {
		t.Fatalf("expired coverage retained: %+v %v", result.Collection, err)
	}
}
