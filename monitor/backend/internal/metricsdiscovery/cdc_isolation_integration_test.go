package metricsdiscovery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/models"
)

// The System identity reader is a protocol fixture; endpoints are real owned CDC sources.
func TestIntegrationCDCDynamicAdmissionIsolation(t *testing.T) {
	file := os.Getenv("ADDP_CDC_ADMISSION_FILE")
	if file == "" {
		t.Skip("requires owned CDC fixture")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Snapshot    models.ObservabilityIdentitySnapshot `json:"snapshot"`
		Expected    []string                             `json:"expected"`
		CA          string                               `json:"ca"`
		Certificate string                               `json:"certificate"`
		Key         string                               `json:"key"`
	}
	if json.Unmarshal(raw, &cfg) != nil || cfg.Snapshot.InfrastructureComponents == nil || len(cfg.Snapshot.InfrastructureComponents.Items) != 2 {
		t.Fatal("invalid CDC fixture")
	}
	ca, err := os.ReadFile(cfg.CA)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("invalid fixture CA")
	}
	client, err := tls.LoadX509KeyPair(cfg.Certificate, cfg.Key)
	if err != nil {
		t.Fatal(err)
	}
	ports := []uint16{}
	for _, item := range cfg.Snapshot.InfrastructureComponents.Items {
		endpoint, err := url.Parse(item.Metrics.Endpoint)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.ParseUint(endpoint.Port(), 10, 16)
		if err != nil {
			t.Fatal(err)
		}
		ports = append(ports, uint16(port))
	}
	policy, err := NewSourcePolicy([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, ports, roots, client, 36)
	if err != nil {
		t.Fatal(err)
	}
	reader := identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) {
		cfg.Snapshot.ObservedAt = time.Now().UTC()
		return &cfg.Snapshot, nil
	})
	groups, err := NewNodeProjector(true, policy, reader).Project(context.Background(), nil)
	if err != nil || len(groups) != len(cfg.Expected) {
		t.Fatalf("admitted %d, expected %d: %v", len(groups), len(cfg.Expected), err)
	}
	seen := map[string]bool{}
	for _, group := range groups {
		seen[group.Labels["addp_source"]] = true
	}
	for _, kind := range cfg.Expected {
		if !seen[kind] {
			t.Fatalf("healthy %s omitted", kind)
		}
	}
	t.Logf("real CDC transport admission: expected sources %v published", cfg.Expected)
}
