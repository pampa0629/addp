package config

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/addp/monitor/internal/metricsdiscovery"
)

// Optional metrics configuration never becomes a business startup dependency.
// An invalid selection remains unconfigured, rather than silently enabling it.
func loadMetricsPolicy() (bool, *metricsdiscovery.SourcePolicy) {
	flag := os.Getenv("ADDP_OBSERVABILITY_METRICS_ENABLED")
	if flag == "" || flag == "false" {
		return false, nil
	}
	if flag != "true" {
		return true, nil
	}
	var cidrs []netip.Prefix
	for _, value := range strings.Split(os.Getenv("MONITOR_METRICS_ALLOWED_CIDRS"), ",") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return true, nil
		}
		cidrs = append(cidrs, prefix)
	}
	var ports []uint16
	for _, value := range strings.Split(os.Getenv("MONITOR_METRICS_ALLOWED_PORTS"), ",") {
		n, err := strconv.ParseUint(value, 10, 16)
		if err != nil || strconv.FormatUint(n, 10) != value || n == 0 {
			return true, nil
		}
		ports = append(ports, uint16(n))
	}
	read := func(key string) []byte {
		file, err := os.Open(os.Getenv(key))
		if err != nil {
			return nil
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 {
			return nil
		}
		data := make([]byte, info.Size())
		n, err := io.ReadFull(file, data)
		if err != nil || n != len(data) {
			return nil
		}
		return data
	}
	ca := read("MONITOR_METRICS_CA_FILE")
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return true, nil
	}
	cert, err := tls.X509KeyPair(read("MONITOR_METRICS_CLIENT_CERT_FILE"), read("MONITOR_METRICS_CLIENT_KEY_FILE"))
	if err != nil {
		return true, nil
	}
	policy, err := metricsdiscovery.NewSourcePolicy(cidrs, ports, roots, cert)
	if err != nil {
		return true, nil
	}
	return true, policy
}
