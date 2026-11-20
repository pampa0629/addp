package processmetrics

import (
	"math"
	"net"
	"os"
	"strings"
	"time"

	"github.com/addp/common/config"
	"github.com/prometheus/client_golang/prometheus"
)

// StartCompute opens the optional private source of a registered Go runtime.
func StartCompute(kind string, started time.Time) (*Server, error) {
	prefix := "ADDP_" + strings.ToUpper(kind) + "_METRICS_"
	endpoint := os.Getenv(prefix + "ENDPOINT")
	if endpoint == "" {
		return nil, nil
	}
	d := config.ProcessMetricsDeployment{ModuleName: kind, Role: "backend", Endpoint: endpoint, Listen: os.Getenv(prefix + "LISTEN"), TLSDir: os.Getenv(prefix + "TLS_DIR")}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	host, _, _ := net.SplitHostPort(d.Listen)
	address := net.ParseIP(host)
	if !address.IsLoopback() && !address.IsPrivate() {
		return nil, ErrSourceUnavailable
	}
	registry := prometheus.NewRegistry()
	metrics := []struct {
		name, help string
		value      func() float64
	}{
		{"observed_at_seconds", "Time of observation.", func() float64 { return float64(time.Now().Unix()) }},
		{"uptime_seconds", "Elapsed seconds since process startup.", func() float64 { return time.Since(started).Seconds() }},
		{"resident_memory_bytes", "Current process resident memory.", func() float64 {
			sample, err := sampleSelf()
			if err != nil {
				return math.NaN()
			}
			return float64(sample.ResidentBytes)
		}},
	}
	for _, metric := range metrics {
		registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "addp_" + kind + "_" + metric.name, Help: metric.help}, metric.value))
	}
	return serveRegistry(d, registry)
}

func (s *Server) Done() <-chan struct{} { return s.done }
