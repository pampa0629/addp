package httptrace

import "github.com/prometheus/client_golang/prometheus"

// Collector is registered only in the existing private process registry. No
// additional listener, global registry or telemetry export is created.
func Collector() prometheus.Collector { return healthCollector{} }

type healthCollector struct{}

var queueDescriptor = prometheus.NewDesc("addp_http_trace_queue_spans", "Ended spans awaiting export", nil, nil)
var dropDescriptor = prometheus.NewDesc("addp_http_trace_dropped_spans_total", "Ended spans dropped by the bounded queue", nil, nil)
var failDescriptor = prometheus.NewDesc("addp_http_trace_export_failed_spans_total", "Spans lost on failed export without retry", nil, nil)
var exportDescriptor = prometheus.NewDesc("addp_http_trace_exported_spans_total", "Spans accepted by the OTLP export transport; not a retention receipt", nil, nil)

func (healthCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{queueDescriptor, dropDescriptor, failDescriptor, exportDescriptor} {
		ch <- d
	}
}
func (healthCollector) Collect(ch chan<- prometheus.Metric) {
	s := Snapshot()
	ch <- prometheus.MustNewConstMetric(queueDescriptor, prometheus.GaugeValue, float64(s.Queued))
	for _, v := range []struct {
		d *prometheus.Desc
		n uint64
	}{{dropDescriptor, s.Dropped}, {failDescriptor, s.ExportFailed}, {exportDescriptor, s.Exported}} {
		ch <- prometheus.MustNewConstMetric(v.d, prometheus.CounterValue, float64(v.n))
	}
}
