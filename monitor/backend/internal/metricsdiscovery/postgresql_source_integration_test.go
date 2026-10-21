package metricsdiscovery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"testing"
)

// The owned postgres-source gate supplies the production mTLS source. This
// checks its protection through the same admission policy used by HTTP SD.
func TestIntegrationMetricsPostgreSQLAdmission(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_ADMISSION_INTEGRATION") != "1" {
		t.Skip("requires standard Monitor metrics T2 gate")
	}
	endpoint, err := url.Parse(os.Getenv("ADDP_POSTGRES_ADMISSION_ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	ip, err := netip.ParseAddr(endpoint.Hostname())
	if err != nil || !ip.IsLoopback() {
		t.Fatal("expected owned loopback source")
	}
	port, err := strconv.ParseUint(endpoint.Port(), 10, 16)
	if err != nil || port == 0 {
		t.Fatal("expected explicit source port")
	}
	ca, err := os.ReadFile(os.Getenv("ADDP_POSTGRES_ADMISSION_CA"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("invalid source CA")
	}
	certificate, err := tls.LoadX509KeyPair(os.Getenv("ADDP_POSTGRES_ADMISSION_CERT"), os.Getenv("ADDP_POSTGRES_ADMISSION_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewSourcePolicy([]netip.Prefix{netip.PrefixFrom(ip, ip.BitLen())}, []uint16{uint16(port)}, roots, certificate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = policy.Admit(context.Background(), endpoint.String()); err != nil {
		t.Fatalf("production PostgreSQL mTLS source was not admitted: %v", err)
	}
}
