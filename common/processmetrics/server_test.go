package processmetrics

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/config"
)

func testSourcePKI(t *testing.T) (string, *x509.CertPool, tls.Certificate) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test source CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "ca.crt"), caPEM, 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	issue := func(serial int64, usage x509.ExtKeyUsage) ([]byte, []byte) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "source-test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	}
	cert, key := issue(2, x509.ExtKeyUsageServerAuth)
	for name, value := range map[string][]byte{"server.crt": cert, "server.key": key} {
		if err := os.WriteFile(filepath.Join(directory, name), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cert, key = issue(3, x509.ExtKeyUsageClientAuth)
	client, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	return directory, roots, client
}

func testDeployment(t *testing.T, directory string) config.ProcessMetricsDeployment {
	t.Helper()
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	return config.ProcessMetricsDeployment{ModuleName: "monitor", Role: "worker", Listen: address, Endpoint: "https://" + address + "/metrics", TLSDir: directory}
}

func TestSourceRequiresMutualTLSAndReleasesListener(t *testing.T) {
	directory, roots, certificate := testSourcePKI(t)
	d := testDeployment(t, directory)
	identity := Identity{ModuleName: "monitor", Role: "worker", InstanceID: "current-process", StartedAt: time.Now().Add(-time.Second)}
	s, err := start(d, identity, func() (Sample, error) { return Sample{CPUSeconds: 2, ResidentBytes: 8192}, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.Declaration().Endpoint != d.Endpoint {
		t.Fatal("wrong endpoint declaration")
	}
	copy := s.Declaration()
	copy.Endpoint = "changed"
	if s.Declaration().Endpoint != d.Endpoint {
		t.Fatal("declaration is mutable")
	}
	for _, authenticated := range []bool{false, true} {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
		if authenticated {
			tlsConfig.Certificates = []tls.Certificate{certificate}
		}
		transport := &http.Transport{TLSClientConfig: tlsConfig}
		t.Cleanup(transport.CloseIdleConnections)
		client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
		response, err := client.Get(d.Endpoint)
		if !authenticated {
			if err == nil {
				response.Body.Close()
				t.Fatal("unauthenticated source access succeeded")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || !strings.Contains(string(body), `runtime_instance_id="current-process"`) || !strings.Contains(string(body), "process_resident_memory_bytes 8192") || strings.Contains(string(body), "go_memstats") {
			t.Fatalf("source response: %d %s %v", response.StatusCode, body, err)
		}
	}
	if _, err := start(d, identity, sampleSelf); err == nil {
		t.Fatal("occupied listener must fail without port avoidance")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", d.Listen)
	if err != nil {
		t.Fatalf("listener not released: %v", err)
	}
	listener.Close()
}

func TestSourceConfigurationAndSamplingFailureAreExplicit(t *testing.T) {
	directory, _, _ := testSourcePKI(t)
	d := testDeployment(t, directory)
	identity := Identity{ModuleName: "monitor", Role: "worker", InstanceID: "self", StartedAt: time.Now()}
	if _, err := start(d, identity, func() (Sample, error) { return Sample{}, ErrSampleUnavailable }); err != ErrSourceUnavailable {
		t.Fatal("failed native sample must not advertise")
	}
	for _, mutate := range []func(*config.ProcessMetricsDeployment, *Identity){
		func(d *config.ProcessMetricsDeployment, i *Identity) { d.TLSDir = "/nonexistent/process-source" },
		func(d *config.ProcessMetricsDeployment, i *Identity) { d.Endpoint = "https://localhost:18100/metrics" },
		func(d *config.ProcessMetricsDeployment, i *Identity) { i.Role = "backend" },
		func(d *config.ProcessMetricsDeployment, i *Identity) { i.StartedAt = time.Now().Add(time.Hour) },
	} {
		deployment, subject := d, identity
		mutate(&deployment, &subject)
		if _, err := start(deployment, subject, sampleSelf); err != ErrSourceUnavailable {
			t.Fatal("unsafe source advertised")
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "unexpected.key"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := start(d, identity, sampleSelf); err != ErrSourceUnavailable {
		t.Fatal("unexpected secret files accepted")
	}
}
