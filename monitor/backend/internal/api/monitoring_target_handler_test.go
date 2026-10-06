package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/service"
	"github.com/google/uuid"
)

type apiTargetStore struct{ rows []metricsdiscovery.NodeTarget }

func (s *apiTargetStore) Snapshot(context.Context) ([]metricsdiscovery.NodeTarget, error) {
	return s.rows, nil
}
func (s *apiTargetStore) Get(_ context.Context, id string) (metricsdiscovery.NodeTarget, error) {
	for _, row := range s.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return metricsdiscovery.NodeTarget{}, repository.ErrTargetNotFound
}
func (s *apiTargetStore) Mutate(_ context.Context, row metricsdiscovery.NodeTarget, create, remove bool, _ func(context.Context, []metricsdiscovery.NodeTarget) error) (metricsdiscovery.NodeTarget, error) {
	if create {
		s.rows = append(s.rows, row)
	}
	return row, nil
}

func targetAPIIdentity(principal, clientID, token, permission string) authorization.AuthContext {
	identity := monitorTenantAuthContext()
	identity.Context = authorization.AuthSessionContext{Type: "platform"}
	identity.Principal.Type = principal
	identity.Client.ClientID = &clientID
	identity.Token.Type = token
	identity.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
	identity.Authorization.RoleAssignments[0].Permissions = []string{permission}
	identity.Authorization.RoleAssignments[0].RoleKey = "platform.system_administrator"
	identity.Authentication.AssuranceLevel = "aal2"
	if principal == "service_principal" {
		identity.Authentication.Methods = []string{"service_secret"}
		identity.Authentication.AssuranceLevel = "not_applicable"
		identity.Client.ScopeMode = "restricted"
		identity.Client.Scopes = []string{"addp.api"}
		identity.Authorization.RoleAssignments[0].RoleKey = "platform.prometheus_runtime"
	}
	return identity
}
func TestMetricsDiscoveryRejectsOtherIdentitiesAndDoesNotReturnEmptyOnFailure(t *testing.T) {
	for _, tc := range []struct {
		name, principal, client, token, permission string
		want                                       int
	}{
		{"tenant", "service_principal", "addp-prometheus", "service_access_token", "monitor.metrics_discovery.read", 403},
		{"delegated", "user", "addp-prometheus", "delegated_access_token", "monitor.metrics_discovery.read", 403},
		{"User", "user", "addp-web", "first_party_access_token", "monitor.metrics_discovery.read", 403},
		{"other service", "service_principal", "addp-log-observer", "service_access_token", "monitor.metrics_discovery.read", 403},
		{"wrong credential kind", "service_principal", "addp-prometheus", "oauth_access_token", "monitor.metrics_discovery.read", 403},
		{"missing permission", "service_principal", "addp-prometheus", "service_access_token", "system.runtime_registry.read", 403},
		{"unconfigured", "service_principal", "addp-prometheus", "service_access_token", "monitor.metrics_discovery.read", 503},
		{"disabled", "service_principal", "addp-prometheus", "service_access_token", "monitor.metrics_discovery.read", 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := targetAPIIdentity(tc.principal, tc.client, tc.token, tc.permission)
			if tc.name == "tenant" {
				identity.Context = monitorTenantAuthContext().Context
				identity.Authorization.RoleAssignments[0].Scope = monitorTenantAuthContext().Authorization.RoleAssignments[0].Scope
				identity.Authorization.RoleAssignments[0].RoleKey = "tenant.monitor_runtime"
			}
			if tc.name == "delegated" {
				identity.Client.ScopeMode = "restricted"
				identity.Client.Scopes = []string{"metrics.discover"}
				identity.Client.Audiences = []string{"monitor"}
				identity.Delegation = &authorization.DelegationFacts{DelegatedByClientID: tc.client, AgentRunID: "run-test", ToolCallID: "call-test"}
			}
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(identity)
			}))
			defer system.Close()
			targets := service.NewMonitoringTargetService(&apiTargetStore{}, nil, nil, tc.name == "unconfigured", nil)
			router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, system.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, nil, nil, targets, nil)
			req := httptest.NewRequest("GET", "/api/v1/monitor/platform/metrics_discovery", nil)
			req.Header.Set("Authorization", "Bearer addp_at_collector")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.want || w.Body.String() == "[]" || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("discovery=%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestTargetAPIForwardsUserAndValidatesStrictPayload(t *testing.T) {
	identity := targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.monitoring_target.create")
	nodeID := uuid.NewString()
	ownerCalls := 0
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/v1/system/platform/host_nodes/") {
			ownerCalls++
			if r.Header.Get("Authorization") != "Bearer addp_at_user" {
				t.Error("owner not called as User")
			}
			fmt.Fprintf(w, `{"node_id":%q,"version":1,"enabled":false}`, nodeID)
			return
		}
		json.NewEncoder(w).Encode(identity)
	}))
	defer system.Close()
	store := &apiTargetStore{}
	targets := service.NewMonitoringTargetService(store, client.NewSystemServiceClient(system.URL, nil, system.Client()), nil, false, nil)
	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, system.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, nil, nil, targets, nil)
	base := fmt.Sprintf(`{"subject":{"kind":"node","node_id":%q},"monitor_kind":"host_resources","source":{"type":"node_exporter","endpoint":"https://127.0.0.1:443/metrics"},"enabled":false}`, nodeID)
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{}`, 400}, {strings.Replace(base, `"enabled":false`, `"labels":{"spoof":"identity"},"enabled":false`, 1), 400},
		{base + ` {}`, 400}, {strings.Replace(base, `"enabled":false`, `"version":1,"enabled":false`, 1), 400},
		{strings.Replace(base, `"enabled":false`, `"enabled":true`, 1), 409}, {base, 201},
	} {
		req := httptest.NewRequest("POST", "/api/v1/monitor/platform/monitoring_targets", bytes.NewBufferString(tc.body))
		req.Header.Set("Authorization", "Bearer addp_at_user")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
		}
	}
	if len(store.rows) != 1 || ownerCalls != 2 {
		t.Fatalf("writes=%d owner=%d", len(store.rows), ownerCalls)
	}

	identity.Token.Type = "oauth_access_token"
	identity.Client.ScopeMode = "restricted"
	identity.Client.Scopes = []string{"addp.api"}
	oauthReq := httptest.NewRequest("POST", "/api/v1/monitor/platform/monitoring_targets", bytes.NewBufferString(base))
	oauthReq.Header.Set("Authorization", "Bearer addp_at_user")
	oauthReq.Header.Set("Content-Type", "application/json")
	oauthResponse := httptest.NewRecorder()
	router.ServeHTTP(oauthResponse, oauthReq)
	if oauthResponse.Code != 201 {
		t.Fatalf("current OAuth User rejected: %d %s", oauthResponse.Code, oauthResponse.Body.String())
	}
	identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.monitoring_target.read"}
	for _, query := range []string{"page_size=101", "page=1&page=2", "unexpected=1", "page=%zz"} {
		req := httptest.NewRequest("GET", "/api/v1/monitor/platform/monitoring_targets?"+query, nil)
		req.Header.Set("Authorization", "Bearer addp_at_user")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("query %q = %d", query, w.Code)
		}
	}
}

type apiCurrentIdentities struct {
	snapshot *models.ObservabilityIdentitySnapshot
	err      error
}

func (r *apiCurrentIdentities) GetObservabilityIdentities(context.Context) (*models.ObservabilityIdentitySnapshot, error) {
	if r.err != nil {
		return nil, r.err
	}
	r.snapshot.ObservedAt = time.Now().UTC()
	return r.snapshot, nil
}

// A real certificate/key and controlled CIDR initialize the same production
// policy. Discovery resolves addresses; source admission has its own TLS tests.
func apiDiscoveryPolicy(t *testing.T) *metricsdiscovery.SourcePolicy {
	t.Helper()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, issuer, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(issuer)
	policy, err := metricsdiscovery.NewSourcePolicy([]netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, []uint16{8443}, roots, tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestMetricsDiscoveryProjectsCurrentNodesAndFailsWithoutCurrentFacts(t *testing.T) {
	identity := targetAPIIdentity("service_principal", "addp-prometheus", "service_access_token", "monitor.metrics_discovery.read")
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(identity)
	}))
	defer system.Close()
	nodeID, targetID := uuid.NewString(), uuid.NewString()
	store := &apiTargetStore{rows: []metricsdiscovery.NodeTarget{{ID: targetID, Version: 3, Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: nodeID}, MonitorKind: "host_resources", Source: metricsdiscovery.NodeSource{Type: "node_exporter", Endpoint: "https://127.0.0.1:8443/metrics"}, Enabled: true}}}
	facts := &apiCurrentIdentities{snapshot: &models.ObservabilityIdentitySnapshot{Nodes: []models.ObservabilityNodeIdentity{{NodeID: nodeID, Version: 2}}, ModuleInstances: []models.ObservabilityModuleIdentity{}}}
	targets := service.NewMonitoringTargetService(store, nil, facts, true, apiDiscoveryPolicy(t))
	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, system.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, nil, nil, targets, nil)
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v1/monitor/platform/metrics_discovery", nil)
		req.Header.Set("Authorization", "Bearer addp_at_collector")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	w := request()
	if w.Code != 200 {
		t.Fatalf("projection status=%d %s", w.Code, w.Body.String())
	}
	var groups []metricsdiscovery.TargetGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Targets[0] != "127.0.0.1:8443" || groups[0].Labels["addp_node_id"] != nodeID || groups[0].Labels["__meta_addp_target_version"] != "3" {
		t.Fatalf("untrusted projection: %#v", groups)
	}
	facts.err = errors.New("private owner failure")
	if w := request(); w.Code != 503 || w.Body.String() == "[]" || strings.Contains(w.Body.String(), "private owner") {
		t.Fatalf("owner outage: %d %s", w.Code, w.Body.String())
	}
	facts.err = nil
	facts.snapshot.Nodes = []models.ObservabilityNodeIdentity{}
	if w := request(); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("node disabled: %d %s", w.Code, w.Body.String())
	}
	if !store.rows[0].Enabled {
		t.Fatal("current node facts rewrote collection intent")
	}
}
