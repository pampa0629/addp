// Package metricsdiscovery owns the controlled node-source admission and
// current-identity projection used by Monitor's metrics discovery path.
package metricsdiscovery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	Timeout                      = 5 * time.Second
	EndpointSampleLimit          = 20000
	SelfSampleReservation        = 20000
	TotalSampleReservation       = 200000
	EndpointBodyLimit      int64 = 10 << 20
	TargetLimit                  = 1000
)

var (
	ErrDisabled            = errors.New("metrics discovery is disabled")
	ErrUnconfigured        = errors.New("metrics source policy is not configured")
	ErrInvalidTarget       = errors.New("invalid node monitoring target")
	ErrEndpointForbidden   = errors.New("metrics endpoint is outside the deployment policy")
	ErrResolutionFailed    = errors.New("metrics endpoint resolution failed")
	ErrAdmissionFailed     = errors.New("metrics source admission failed")
	ErrUnprotected         = errors.New("metrics source protection could not be verified")
	ErrBudgetExceeded      = errors.New("metrics discovery budget exceeded")
	ErrIdentityUnavailable = errors.New("current observability identities are unavailable")
)

// Resolver must honor ctx; the production implementation is net.DefaultResolver.
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// SourcePolicy contains deployment facts only. It cannot authorize a node or
// attribute containers to module instances or tenants.
type SourcePolicy struct {
	prefixes  []netip.Prefix
	ports     map[uint16]bool
	resolver  Resolver
	tlsConfig *tls.Config
}

func NewSourcePolicy(prefixes []netip.Prefix, ports []uint16, roots *x509.CertPool, certificate tls.Certificate) (*SourcePolicy, error) {
	if len(prefixes) == 0 || len(prefixes) > 64 || len(ports) == 0 || len(ports) > 64 || roots == nil ||
		len(certificate.Certificate) == 0 || certificate.PrivateKey == nil {
		return nil, ErrUnconfigured
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil || leaf.IsCA || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil, ErrUnconfigured
	}
	clientAuth := false
	for _, usage := range leaf.ExtKeyUsage {
		clientAuth = clientAuth || usage == x509.ExtKeyUsageClientAuth
	}
	if !clientAuth {
		return nil, ErrUnconfigured
	}
	seen := make(map[netip.Prefix]bool)
	for _, prefix := range prefixes {
		if !prefix.IsValid() || prefix.Bits() == 0 || prefix.Addr().Is4In6() || prefix != prefix.Masked() || seen[prefix] {
			return nil, ErrUnconfigured
		}
		seen[prefix] = true
	}
	portSet := make(map[uint16]bool, len(ports))
	for _, port := range ports {
		if port == 0 || portSet[port] {
			return nil, ErrUnconfigured
		}
		portSet[port] = true
	}
	// Keep caller-owned pools and certificate slices out of mutable policy state.
	certificate.Leaf = leaf
	chain := make([][]byte, len(certificate.Certificate))
	for i, der := range certificate.Certificate {
		chain[i] = append([]byte(nil), der...)
	}
	certificate.Certificate = chain
	return &SourcePolicy{
		prefixes: append([]netip.Prefix(nil), prefixes...), ports: portSet, resolver: net.DefaultResolver,
		tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots.Clone(), Certificates: []tls.Certificate{certificate}},
	}, nil
}

type ResolvedEndpoint struct {
	Address string // Canonical IP:port, including IPv6 brackets; never an editable DNS name.
}

func parseEndpoint(endpoint string) (*url.URL, error) {
	if len(endpoint) > 512 || strings.TrimSpace(endpoint) != endpoint || !strings.HasPrefix(endpoint, "https://") || strings.ContainsAny(endpoint, "?#\\") {
		return nil, ErrEndpointForbidden
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" || u.Path != "/metrics" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, ErrEndpointForbidden
	}
	host, portText, err := net.SplitHostPort(u.Host)
	port, portErr := strconv.ParseUint(portText, 10, 16)
	if err != nil || portErr != nil || port == 0 || portText != strconv.FormatUint(port, 10) || host == "" {
		return nil, ErrEndpointForbidden
	}
	if ip, err := netip.ParseAddr(host); (err != nil && !validDNSName(host)) || (err == nil && ip.Zone() != "") {
		return nil, ErrEndpointForbidden
	}
	return u, nil
}

func (p *SourcePolicy) parse(endpoint string) (*url.URL, error) {
	if p == nil {
		return nil, ErrUnconfigured
	}
	u, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.ParseUint(u.Port(), 10, 16)
	if !p.ports[uint16(port)] {
		return nil, ErrEndpointForbidden
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !p.allowed(ip) {
		return nil, ErrEndpointForbidden
	}
	return u, nil
}

func validDNSName(host string) bool {
	if len(host) > 253 || strings.ToLower(host) != host {
		return false
	}
	hasLetter := false
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if ch >= 'a' && ch <= 'z' {
				hasLetter = true
				continue
			}
			if ch != '-' && (ch < '0' || ch > '9') {
				return false
			}
		}
	}
	return hasLetter
}

func (p *SourcePolicy) allowed(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address == netip.MustParseAddr("255.255.255.255") || address == netip.MustParseAddr("100.100.100.200") ||
		address == netip.MustParseAddr("168.63.129.16") || address == netip.MustParseAddr("fd00:ec2::254") {
		return false
	}
	for _, prefix := range p.prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// Resolve rechecks every DNS answer and returns one pinned address. Unknown,
// forbidden or ambiguous resolution fails the snapshot rather than clearing it.
func (p *SourcePolicy) Resolve(ctx context.Context, endpoint string) (ResolvedEndpoint, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	u, err := p.parse(endpoint)
	if err != nil {
		return ResolvedEndpoint{}, err
	}
	if err := ctx.Err(); err != nil {
		return ResolvedEndpoint{}, ErrResolutionFailed
	}
	host := u.Hostname()
	ip, err := netip.ParseAddr(host)
	if err != nil {
		addresses, err := p.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || ctx.Err() != nil || len(addresses) == 0 || len(addresses) > 64 {
			return ResolvedEndpoint{}, ErrResolutionFailed
		}
		for _, address := range addresses {
			if !p.allowed(address) {
				return ResolvedEndpoint{}, ErrEndpointForbidden
			}
			address = address.Unmap()
			if ip.IsValid() && ip != address {
				return ResolvedEndpoint{}, ErrEndpointForbidden
			}
			ip = address
		}
	}
	return ResolvedEndpoint{Address: net.JoinHostPort(ip.Unmap().String(), u.Port())}, nil
}

// Admit checks only endpoint transport admission. Target persistence must still
// validate the current User with System and reserve capacity atomically.
func (p *SourcePolicy) Admit(ctx context.Context, endpoint string) (ResolvedEndpoint, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	resolved, err := p.Resolve(ctx, endpoint)
	if err != nil {
		return ResolvedEndpoint{}, err
	}
	if _, err := p.fetch(ctx, resolved, true); err != nil {
		return ResolvedEndpoint{}, ErrAdmissionFailed
	}
	status, err := p.fetch(ctx, resolved, false)
	if err != nil {
		// TCP crypto/tls wraps peer alerts in net.OpError. Match only the two
		// certificate rejection alerts, never a network error or a timeout.
		var remote *net.OpError
		if ctx.Err() != nil || !errors.As(err, &remote) || remote.Op != "remote error" || remote.Err == nil ||
			(remote.Err.Error() != tls.AlertError(42).Error() && remote.Err.Error() != tls.AlertError(116).Error()) {
			return ResolvedEndpoint{}, ErrUnprotected
		}
	} else if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return ResolvedEndpoint{}, ErrUnprotected
	}
	return resolved, nil
}

func (p *SourcePolicy) fetch(ctx context.Context, resolved ResolvedEndpoint, authenticated bool) (int, error) {
	tlsConfig := p.tlsConfig.Clone()
	if !authenticated {
		tlsConfig.Certificates = nil
	}
	transport := &http.Transport{
		Proxy: nil, TLSClientConfig: tlsConfig, DisableKeepAlives: true, DisableCompression: true,
		DialContext:         (&net.Dialer{Timeout: Timeout}).DialContext,
		TLSHandshakeTimeout: Timeout, ResponseHeaderTimeout: Timeout, MaxResponseHeaderBytes: 32 << 10,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrEndpointForbidden }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+resolved.Address+"/metrics", nil)
	if err != nil {
		return 0, ErrAdmissionFailed
	}
	request.Header.Set("Accept", "text/plain; version=0.0.4, application/openmetrics-text")
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if !authenticated {
		return response.StatusCode, nil
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (contentType != "text/plain" && contentType != "application/openmetrics-text") ||
		response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" || response.ContentLength > EndpointBodyLimit {
		return 0, ErrAdmissionFailed
	}
	n, err := io.Copy(io.Discard, io.LimitReader(response.Body, EndpointBodyLimit+1))
	if err != nil || n > EndpointBodyLimit {
		return 0, ErrAdmissionFailed
	}
	return response.StatusCode, nil
}
