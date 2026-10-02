package runtimelog

import "testing"

func TestCollectorMetricsMissingAreNotZero(t *testing.T) {
	if ParseCollector("process_start_time_seconds 1\n").Valid {
		t.Fatal("missing sending counters treated as zero")
	}
	data := "process_start_time_seconds 100\nloki_write_batch_retries_total{component_id=\"a\"} 2\nloki_write_batch_retries_total{component_id=\"b\"} 3\nloki_write_dropped_entries_total 0\nloki_write_sent_entries_total 20\n"
	value := ParseCollector(data)
	if !value.Valid || value.Retries != 5 || value.Sent != 20 {
		t.Fatalf("safe aggregation failed: %#v", value)
	}
	if ParseCollector(data + "loki_write_sent_entries_total NaN\n").Valid {
		t.Fatal("NaN accepted")
	}
}
