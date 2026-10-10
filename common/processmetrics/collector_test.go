package processmetrics

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestCollectorOnlyExposesCurrentProcess(t *testing.T) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(newCollector("monitor", "worker", "own-instance", time.Unix(100, 0), func() (Sample, error) { return Sample{CPUSeconds: 0, ResidentBytes: 4096}, nil }))
	families, err := registry.Gather()
	if err != nil || len(families) != 4 {
		t.Fatalf("fixed families: %v %v", families, err)
	}
	values := map[string]float64{}
	for _, family := range families {
		if len(family.Metric) != 1 {
			t.Fatal("one sample per family")
		}
		m := family.Metric[0]
		if family.GetName() == "addp_process_identity_info" {
			labels := map[string]string{}
			for _, label := range m.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["module_name"] != "monitor" || labels["runtime_role"] != "worker" || labels["runtime_instance_id"] != "own-instance" || len(labels) != 5 {
				t.Fatalf("identity: %v", labels)
			}
		} else if len(m.Label) != 0 {
			t.Fatal("resource labels are supplied by sole discovery")
		}
		if m.Counter != nil {
			values[family.GetName()] = m.Counter.GetValue()
		} else {
			values[family.GetName()] = m.Gauge.GetValue()
		}
	}
	if values["process_cpu_seconds_total"] != 0 || values["process_resident_memory_bytes"] != 4096 || values["process_start_time_seconds"] != 100 {
		t.Fatalf("values: %v", values)
	}
}

func TestFailedSampleCannotBecomeValidZero(t *testing.T) {
	for _, sample := range []Sampler{
		func() (Sample, error) { return Sample{}, ErrSampleUnavailable },
		func() (Sample, error) { return Sample{CPUSeconds: math.NaN()}, nil },
		func() (Sample, error) { return Sample{CPUSeconds: math.Inf(1)}, nil },
		func() (Sample, error) { return Sample{CPUSeconds: -1}, nil },
	} {
		registry := prometheus.NewRegistry()
		registry.MustRegister(newCollector("monitor", "backend", "self", time.Now(), sample))
		if _, err := registry.Gather(); err == nil {
			t.Fatal("failed sample gathered")
		}
		rr := httptest.NewRecorder()
		handler(registry).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if rr.Code != 500 || strings.Contains(rr.Body.String(), "process_resident_memory_bytes") || strings.Contains(rr.Body.String(), "self") {
			t.Fatalf("unsafe failure: %d %s", rr.Code, rr.Body.String())
		}
	}
}

func TestPrivateMetricsHandlerRejectsOtherRoutesAndParameters(t *testing.T) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(newCollector("monitor", "backend", "self", time.Now(), func() (Sample, error) { return Sample{}, nil }))
	for _, test := range []struct {
		method, path string
		status       int
	}{{"GET", "/metrics", 200}, {"GET", "/metrics?", 404}, {"GET", "/metrics?pid=1", 404}, {"GET", "/health", 404}, {"POST", "/metrics", 405}, {"HEAD", "/metrics", 405}, {"GET", "/%6detrics", 404}} {
		rr := httptest.NewRecorder()
		handler(registry).ServeHTTP(rr, httptest.NewRequest(test.method, test.path, nil))
		if rr.Code != test.status {
			t.Fatalf("%s %s: %d", test.method, test.path, rr.Code)
		}
	}
}
