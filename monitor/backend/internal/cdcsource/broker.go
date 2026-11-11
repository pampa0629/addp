package cdcsource

import (
	"bytes"
	"math"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func broker(raw []byte) (map[string]float64, error) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnavailable
	}
	count := 0
	for _, family := range families {
		count += len(family.Metric)
	}
	if count > 4096 {
		return nil, ErrUnavailable
	}
	values := map[string]float64{}
	for native, name := range map[string]string{
		"redpanda_storage_disk_total_bytes":       "disk_total_bytes",
		"redpanda_storage_disk_free_bytes":        "disk_free_bytes",
		"redpanda_cluster_unavailable_partitions": "unavailable_partitions",
	} {
		family := families[native]
		// First scope is exactly one broker. Missing or duplicated node aggregates are not zero.
		if family == nil || len(family.Metric) != 1 {
			return nil, ErrUnavailable
		}
		v, ok := gauge(family.Metric[0])
		if !ok || v < 0 {
			return nil, ErrUnavailable
		}
		values[name] = v
	}
	if values["disk_free_bytes"] > values["disk_total_bytes"] {
		return nil, ErrUnavailable
	}
	type partition struct{ topic, id string }
	ends := map[partition]float64{}
	for _, sample := range families["redpanda_kafka_max_offset"].GetMetric() {
		labels := metricLabels(sample)
		key := partition{labels["redpanda_topic"], labels["redpanda_partition"]}
		value, ok := gauge(sample)
		if !ok || key.topic == "" || key.id == "" || value < -1 || math.Trunc(value) != value {
			return nil, ErrUnavailable
		}
		if _, exists := ends[key]; exists {
			return nil, ErrUnavailable
		}
		ends[key] = value
	}
	groups := map[struct {
		group string
		key   partition
	}]bool{}
	complete, pending := true, float64(0)
	for _, sample := range families["redpanda_kafka_consumer_group_committed_offset"].GetMetric() {
		labels := metricLabels(sample)
		key := partition{labels["redpanda_topic"], labels["redpanda_partition"]}
		identity := struct {
			group string
			key   partition
		}{labels["redpanda_group"], key}
		value, ok := gauge(sample)
		if !ok || value < -1 || math.Trunc(value) != value || key.topic == "" || key.id == "" || identity.group == "" || groups[identity] {
			return nil, ErrUnavailable
		}
		groups[identity] = true
		end, exists := ends[key]
		if !exists || end < value {
			complete = false
			continue
		}
		pending += end - value
	}
	values["consumer_group_partitions"] = float64(len(groups))
	if len(groups) > 0 && complete {
		values["consumer_group_pending_records"] = pending
	}
	return values, nil
}

func gauge(m *dto.Metric) (float64, bool) {
	if m.Gauge == nil || m.Gauge.Value == nil {
		return 0, false
	}
	v := m.Gauge.GetValue()
	return v, !math.IsNaN(v) && !math.IsInf(v, 0)
}

func metricLabels(m *dto.Metric) map[string]string {
	labels := map[string]string{}
	for _, label := range m.Label {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}
