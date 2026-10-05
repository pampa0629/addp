package metricsdiscovery

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

type sourcePKI struct {
	roots          *x509.CertPool
	client, server tls.Certificate
}

func testSourcePKI(t *testing.T, serverIP bool) sourcePKI {
	t.Helper()
	newKey := func() *ecdsa.PrivateKey {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	caKey := newKey()
	now := time.Now().UTC()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "disposable source CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	issue := func(serial int64, usage x509.ExtKeyUsage) tls.Certificate {
		key := newKey()
		leaf := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		if usage == x509.ExtKeyUsageServerAuth {
			leaf.DNSNames = []string{"source.test"}
			if serverIP {
				leaf.IPAddresses = []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
			}
		}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	}
	return sourcePKI{roots: roots, client: issue(2, x509.ExtKeyUsageClientAuth), server: issue(3, x509.ExtKeyUsageServerAuth)}
}

func testPolicy(t *testing.T, pki sourcePKI, ports ...uint16) *SourcePolicy {
	t.Helper()
	p, err := NewSourcePolicy([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}, ports, pki.roots, pki.client)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func testSourceServer(t *testing.T, pki sourcePKI, clientAuth tls.ClientAuthType, version uint16, handler http.Handler) (*httptest.Server, uint16) {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{MinVersion: version, MaxVersion: version, Certificates: []tls.Certificate{pki.server},
		ClientAuth: clientAuth, ClientCAs: pki.roots}
	server.StartTLS()
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	port, _ := strconv.ParseUint(u.Port(), 10, 16)
	return server, uint16(port)
}

func TestSourcePolicyRejectsUnsafeEndpointsBeforeDNS(t *testing.T) {
	p := testPolicy(t, testSourcePKI(t, true), 9443)
	var lookups int
	p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		return nil, errors.New("must not resolve an invalid endpoint")
	})
	for _, endpoint := range []string{
		"http://127.0.0.1:9443/metrics", "https://name:secret@127.0.0.1:9443/metrics",
		"https://127.0.0.1/metrics", "https://127.0.0.1:9444/metrics", "https://127.0.0.1:09443/metrics",
		"https://127.0.0.1:9443/metrics?", "https://127.0.0.1:9443/metrics#", "https://127.0.0.1:9443/debug",
		"https://127.0.0.1:9443/%6detric%73", " https://127.0.0.1:9443/metrics", "https://source.test.:9443/metrics",
		"https://SOURCE.test:9443/metrics", "https://-source.test:9443/metrics", "https://2130706433:9443/metrics",
		"https://127.1:9443/metrics", "https://[::1%25en0]:9443/metrics", "https://8.8.8.8:9443/metrics",
		"https://169.254.169.254:9443/metrics", "https://[fe80::1]:9443/metrics", "https://0.0.0.0:9443/metrics",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := p.Resolve(context.Background(), endpoint); !errors.Is(err, ErrEndpointForbidden) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if lookups != 0 {
		t.Fatalf("invalid endpoint caused %d DNS queries", lookups)
	}
}

func TestSourcePolicyRequiresExplicitNetworksAndPorts(t *testing.T) {
	pki := testSourcePKI(t, true)
	for _, tc := range []struct {
		prefixes []netip.Prefix
		ports    []uint16
	}{
		{nil, []uint16{9443}}, {[]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, nil},
		{[]netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}, []uint16{9443}},
		{[]netip.Prefix{netip.MustParsePrefix("127.0.0.1/8")}, []uint16{9443}},
		{[]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, []uint16{0}},
		{[]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, []uint16{9443, 9443}},
	} {
		if _, err := NewSourcePolicy(tc.prefixes, tc.ports, pki.roots, pki.client); !errors.Is(err, ErrUnconfigured) {
			t.Fatalf("got %v", err)
		}
	}
	if _, err := NewSourcePolicy([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, []uint16{9443}, pki.roots, pki.server); !errors.Is(err, ErrUnconfigured) {
		t.Fatal("server identity admitted as client")
	}
	p, err := NewSourcePolicy([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, []uint16{9443}, pki.roots, pki.client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Resolve(context.Background(), "https://127.0.0.1:9443/metrics"); !errors.Is(err, ErrEndpointForbidden) {
		t.Fatal("loopback allowed implicitly")
	}
	if _, err := p.Resolve(context.Background(), "https://10.1.2.3:9443/metrics"); err != nil {
		t.Fatalf("explicit private address rejected: %v", err)
	}
}

func TestSourcePolicyAlwaysRejectsMetadataEvenInsideAllowedRanges(t *testing.T) {
	pki := testSourcePKI(t, true)
	p, err := NewSourcePolicy([]netip.Prefix{
		netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("100.100.100.0/24"), netip.MustParsePrefix("168.63.129.0/24"),
		netip.MustParsePrefix("fd00::/8"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("ff00::/8"),
	}, []uint16{9443}, pki.roots, pki.client)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"169.254.169.254", "100.100.100.200", "168.63.129.16", "fd00:ec2::254", "224.0.0.1", "ff02::1"} {
		if _, err := p.Resolve(context.Background(), "https://"+net.JoinHostPort(host, "9443")+"/metrics"); !errors.Is(err, ErrEndpointForbidden) {
			t.Fatalf("metadata/multicast host admitted: %s", host)
		}
	}
}

func TestSourcePolicyRechecksDNSAndPinsExactlyOneAddress(t *testing.T) {
	p := testPolicy(t, testSourcePKI(t, true), 9443)
	answers := []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::ffff:127.0.0.1")}
	p.resolver = resolverFunc(func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		if network != "ip" || host != "source.test" {
			t.Fatal("unexpected DNS lookup")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded DNS lookup")
		}
		return answers, nil
	})
	got, err := p.Resolve(context.Background(), "https://source.test:9443/metrics")
	if err != nil || got.Address != "127.0.0.1:9443" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	for _, changed := range [][]netip.Addr{
		{netip.MustParseAddr("169.254.169.254")}, {netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("8.8.8.8")},
		{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("127.0.0.2")},
	} {
		answers = changed
		if _, err := p.Resolve(context.Background(), "https://source.test:9443/metrics"); !errors.Is(err, ErrEndpointForbidden) {
			t.Fatalf("changed answers admitted: %v", err)
		}
	}
	got, err = p.Resolve(context.Background(), "https://[::1]:9443/metrics")
	if err != nil || got.Address != "[::1]:9443" {
		t.Fatalf("IPv6 got=%v err=%v", got, err)
	}
}

func TestSourcePolicyDNSFailureAndCancellationHaveSafeErrors(t *testing.T) {
	p := testPolicy(t, testSourcePKI(t, true), 9443)
	p.resolver = resolverFunc(func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
		return nil, errors.New("private DNS diagnostics")
	})
	if _, err := p.Resolve(context.Background(), "https://source.test:9443/metrics"); err != ErrResolutionFailed {
		t.Fatalf("unsafe DNS error: %v", err)
	}
	p.resolver = resolverFunc(func(ctx context.Context, _, _ string) ([]netip.Addr, error) { <-ctx.Done(); return nil, ctx.Err() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.Resolve(ctx, "https://source.test:9443/metrics"); err != ErrResolutionFailed {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestSourceAdmissionVerifiesRealMTLSForTLS12And13(t *testing.T) {
	for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
		t.Run(tls.VersionName(version), func(t *testing.T) {
			pki := testSourcePKI(t, true)
			var requests atomic.Int32
			server, port := testSourceServer(t, pki, tls.RequireAndVerifyClientCert, version, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/metrics" || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
					t.Error("unauthenticated or unexpected request")
				}
				w.Header().Set("Content-Type", "text/plain; version=0.0.4")
				io.WriteString(w, "source_value 1\n")
			}))
			p := testPolicy(t, pki, port)
			p.resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			})
			resolved, err := p.Admit(context.Background(), "https://source.test:"+strconv.Itoa(int(port))+"/metrics")
			if err != nil || "https://"+resolved.Address != server.URL || requests.Load() != 1 {
				t.Fatalf("got=%v err=%v requests=%d", resolved, err, requests.Load())
			}
		})
	}
}

func TestSourceAdmissionRejectsAnonymousAccessAndInconclusiveNegativeProbe(t *testing.T) {
	for _, anonymous := range []string{"readable", "connection closed", "redirect", "server failure", "forbidden"} {
		t.Run(anonymous, func(t *testing.T) {
			pki := testSourcePKI(t, true)
			server, port := testSourceServer(t, pki, tls.RequestClientCert, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if len(r.TLS.PeerCertificates) == 0 {
					switch anonymous {
					case "connection closed":
						conn, _, _ := w.(http.Hijacker).Hijack()
						conn.Close()
						return
					case "redirect":
						w.Header().Set("Location", "/other")
						w.WriteHeader(302)
						return
					case "server failure":
						w.WriteHeader(503)
						return
					case "forbidden":
						w.WriteHeader(403)
						return
					}
				}
				w.Header().Set("Content-Type", "text/plain")
				io.WriteString(w, "value 1\n")
			}))
			_, err := testPolicy(t, pki, port).Admit(context.Background(), server.URL+"/metrics")
			if anonymous == "forbidden" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err != ErrUnprotected {
				t.Fatalf("insecure/inconclusive source admitted: %v", err)
			}
		})
	}
}

func TestSourceAdmissionRejectsWrongTrustAndDNSOnlyServerCertificate(t *testing.T) {
	for _, ipSAN := range []bool{true, false} {
		pki := testSourcePKI(t, ipSAN)
		server, port := testSourceServer(t, pki, tls.RequireAndVerifyClientCert, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, "value 1\n")
		}))
		if ipSAN {
			pki.roots = testSourcePKI(t, true).roots
		}
		if _, err := testPolicy(t, pki, port).Admit(context.Background(), server.URL+"/metrics"); err != ErrAdmissionFailed {
			t.Fatalf("wrong source identity admitted: %v", err)
		}
	}
}

func TestSourceAdmissionBoundsResponseAndNeverFollowsRedirects(t *testing.T) {
	var followed atomic.Int32
	redirectDestination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
	defer redirectDestination.Close()
	for _, mode := range []string{"redirect", "html", "compressed", "oversized header", "oversized stream", "upstream error"} {
		t.Run(mode, func(t *testing.T) {
			pki := testSourcePKI(t, true)
			server, port := testSourceServer(t, pki, tls.RequireAndVerifyClientCert, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				switch mode {
				case "redirect":
					http.Redirect(w, r, redirectDestination.URL, 302)
				case "html":
					w.Header().Set("Content-Type", "text/html")
					io.WriteString(w, "private page")
				case "compressed":
					w.Header().Set("Content-Encoding", "gzip")
					io.WriteString(w, "compressed")
				case "oversized header":
					w.Header().Set("Content-Length", strconv.FormatInt(EndpointBodyLimit+1, 10))
					w.WriteHeader(200)
				case "oversized stream":
					w.(http.Flusher).Flush()
					io.Copy(w, io.LimitReader(strings.NewReader(strings.Repeat("x", int(EndpointBodyLimit+1))), EndpointBodyLimit+1))
				case "upstream error":
					w.WriteHeader(500)
					io.WriteString(w, "private diagnostics")
				}
			}))
			if _, err := testPolicy(t, pki, port).Admit(context.Background(), server.URL+"/metrics"); err != ErrAdmissionFailed {
				t.Fatalf("unsafe response admitted: %v", err)
			}
		})
	}
	if followed.Load() != 0 {
		t.Fatal("redirect was followed outside the controlled scope")
	}
}
