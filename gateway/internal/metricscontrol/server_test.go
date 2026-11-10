package metricscontrol

import (
	"context"
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
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/addp/gateway/internal/config"
)

func testCertificates(t *testing.T) (*config.Config, *tls.Config) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	directory := t.TempDir()
	var client tls.Certificate
	for _, name := range []string{"server", "client"} {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		usage := x509.ExtKeyUsageServerAuth
		if name == "client" {
			usage = x509.ExtKeyUsageClientAuth
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"control.test"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
		if name == "client" {
			client, err = tls.X509KeyPair(certPEM, keyPEM)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			for filename, data := range map[string][]byte{"server.crt": certPEM, "server.key": keyPEM, "client-ca.crt": caPEM} {
				if err := os.WriteFile(filepath.Join(directory, filename), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return &config.Config{MetricsControlListen: address, MetricsControlServerName: "control.test", MetricsControlTLSDir: directory}, &tls.Config{RootCAs: roots, ServerName: "control.test", Certificates: []tls.Certificate{client}, MinVersion: tls.VersionTLS12}
}

func TestRequestBoundary(t *testing.T) {
	const token = "/api/v1/system/oauth/token"
	const discovery = "/api/v1/monitor/platform/metrics_discovery"
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"token", "POST", token, "grant_type=client_credentials", 204},
		{"discovery", "GET", discovery, "", 204},
		{"token method", "GET", token, "", 405},
		{"discovery method", "POST", discovery, "", 405},
		{"head", "HEAD", discovery, "", 405},
		{"query", "GET", discovery + "?secret=value", "", 400},
		{"empty query", "POST", token + "?", "", 400},
		{"encoded path", "POST", "/api/v1/system/oauth/%74oken", "", 404},
		{"trailing slash", "POST", token + "/", "", 404},
		{"health", "GET", "/health/ready", "", 404},
		{"other API", "GET", "/api/v1/system/users", "", 404},
		{"dot path", "POST", "/api/v1/system/../system/oauth/token", "", 404},
		{"body boundary", "POST", token, strings.Repeat("a", 8192), 204},
		{"body exceeded", "POST", token, strings.Repeat("a", 8193), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != tc.body || r.Header.Get("Authorization") != "Bearer test-credential" {
					t.Fatal("request was altered")
				}
				if _, ok := r.Context().Deadline(); !ok {
					t.Fatal("missing upstream deadline")
				}
				w.WriteHeader(204)
			})
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer test-credential")
			r.ContentLength = -1 // The body limit must also apply to streaming requests.
			w := httptest.NewRecorder()
			handler(next).ServeHTTP(w, r)
			if w.Code != tc.status || called != (tc.status == 204) {
				t.Fatalf("status %d, reached owner %v", w.Code, called)
			}
		})
	}
}

func TestTLSLifecycleAndOwnerAuthorization(t *testing.T) {
	cfg, clientTLS := testCertificates(t)
	var calls atomic.Int64
	server, err := Start(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer owner-token" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	_, other := testCertificates(t)
	for _, tc := range []struct {
		name   string
		cert   *tls.Config
		bearer bool
		status int
	}{
		{"authorized", clientTLS, true, 204},
		{"owner rejects", clientTLS, false, 401},
		{"no client certificate", &tls.Config{RootCAs: clientTLS.RootCAs, ServerName: "control.test"}, true, 0},
		{"untrusted client", &tls.Config{RootCAs: clientTLS.RootCAs, ServerName: "control.test", Certificates: other.Certificates}, true, 0},
		{"wrong hostname", &tls.Config{RootCAs: clientTLS.RootCAs, ServerName: "wrong.test", Certificates: clientTLS.Certificates}, true, 0},
		{"untrusted server", &tls.Config{RootCAs: other.RootCAs, ServerName: "control.test", Certificates: clientTLS.Certificates}, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &http.Transport{TLSClientConfig: tc.cert}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			r, _ := http.NewRequest("GET", "https://"+cfg.MetricsControlListen+"/api/v1/monitor/platform/metrics_discovery", nil)
			if tc.bearer {
				r.Header.Set("Authorization", "Bearer owner-token")
			}
			before := calls.Load()
			response, err := client.Do(r)
			if tc.status == 0 {
				if err == nil {
					response.Body.Close()
					t.Fatal("TLS request unexpectedly accepted")
				}
				if calls.Load() != before {
					t.Fatal("unauthorized TLS reached HTTP owner")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status %d", response.StatusCode)
			}
		})
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", cfg.MetricsControlListen)
	if err != nil {
		t.Fatalf("listener was not released: %v", err)
	}
	listener.Close()
}

func TestDeploymentValidationAndSecretProjection(t *testing.T) {
	if server, err := Start(&config.Config{MetricsControlTLSDir: "missing"}, nil); server != nil || err != nil {
		t.Fatal("disabled listener validated unused input")
	}
	cfg, _ := testCertificates(t)
	for _, edit := range []func(*config.Config){
		func(c *config.Config) { c.MetricsControlListen = ":9444" },
		func(c *config.Config) { c.MetricsControlListen = "localhost:9444" },
		func(c *config.Config) { c.MetricsControlListen = "127.0.0.1:0" },
		func(c *config.Config) { c.MetricsControlServerName = "wrong.test" },
		func(c *config.Config) { c.MetricsControlTLSDir = "relative" },
	} {
		invalid := *cfg
		edit(&invalid)
		if server, err := Start(&invalid, nil); server != nil || err != ErrUnavailable {
			t.Fatal("invalid input accepted or leaked diagnostics")
		}
	}
	projection := t.TempDir()
	for _, name := range []string{"server.crt", "server.key", "client-ca.crt"} {
		if err := os.Symlink(filepath.Join(cfg.MetricsControlTLSDir, name), filepath.Join(projection, name)); err != nil {
			t.Fatal(err)
		}
	}
	cfg.MetricsControlTLSDir = projection
	server, err := Start(cfg, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(context.Background())
	if duplicate, err := Start(cfg, http.NotFoundHandler()); duplicate != nil || err != ErrUnavailable {
		t.Fatal("occupied listener must fail closed")
	}
}

// TestStandaloneControlFixture runs the same listener against the T2 protocol
// owner. The T2 lifecycle owns its process, certificates and network.
func TestStandaloneControlFixture(t *testing.T) {
	if os.Getenv("ADDP_GATEWAY_CONTROL_FIXTURE") != "1" {
		t.Skip("selected only by the isolated metrics T2 lifecycle")
	}
	target, err := url.Parse(os.Getenv("ADDP_GATEWAY_CONTROL_FIXTURE_UPSTREAM"))
	if err != nil || target.Scheme != "http" || target.Host == "" {
		t.Fatal("invalid fixture upstream")
	}
	cfg := config.Load()
	server, err := Start(cfg, httputil.NewSingleHostReverseProxy(target))
	if err != nil || server == nil {
		t.Fatal("fixture listener unavailable")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}
}
