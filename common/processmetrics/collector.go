// Package processmetrics owns bounded, local sampling of the current process.
package processmetrics

import (
	"errors"
	"math"
	"runtime"
	"time"

	"github.com/addp/common/models"
	"github.com/prometheus/client_golang/prometheus"
)

var ErrSampleUnavailable = errors.New("process sample unavailable")

type Sample struct {
	CPUSeconds    float64
	ResidentBytes uint64
}
type Sampler func() (Sample, error)

type collector struct {
	sample                    Sampler
	started                   float64
	identity, cpu, rss, start *prometheus.Desc
}

func newCollector(module, role, instance string, started time.Time, sample Sampler) *collector {
	labels := prometheus.Labels{"module_name": module, "runtime_role": role, "runtime_instance_id": instance, "operating_system": runtime.GOOS, "schema_version": models.ProcessMetricsSchema}
	started = started.Truncate(time.Microsecond)
	return &collector{sample: sample, started: float64(started.Unix()) + float64(started.Nanosecond())/1e9,
		identity: prometheus.NewDesc("addp_process_identity_info", "Registered current process identity.", nil, labels),
		cpu:      prometheus.NewDesc("process_cpu_seconds_total", "Current process user and system CPU seconds.", nil, nil),
		rss:      prometheus.NewDesc("process_resident_memory_bytes", "Current process resident memory in bytes.", nil, nil),
		start:    prometheus.NewDesc("process_start_time_seconds", "Registered process start time in Unix seconds.", nil, nil)}
}
func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.identity, c.cpu, c.rss, c.start} {
		ch <- d
	}
}
func (c *collector) Collect(ch chan<- prometheus.Metric) {
	s, err := c.sample()
	if err != nil || s.CPUSeconds < 0 || math.IsNaN(s.CPUSeconds) || math.IsInf(s.CPUSeconds, 0) {
		ch <- prometheus.NewInvalidMetric(c.cpu, ErrSampleUnavailable)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.identity, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(c.cpu, prometheus.CounterValue, s.CPUSeconds)
	ch <- prometheus.MustNewConstMetric(c.rss, prometheus.GaugeValue, float64(s.ResidentBytes))
	ch <- prometheus.MustNewConstMetric(c.start, prometheus.GaugeValue, c.started)
}
