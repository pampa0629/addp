package miniosource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/monitor/internal/sourceserver"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

type Snapshot struct {
	SampledAt time.Time
	Epoch     string
	Values    map[string]float64
}

type metric struct {
	input, output string
	counter, sum  bool
	labels        string
}

var catalogue = map[string][]metric{
	"system/process": {
		{"minio_system_process_start_time_seconds", "start_time_seconds", false, false, "server"},
		{"minio_system_process_uptime_seconds", "uptime_seconds", false, false, "server"},
	},
	"cluster/health": {
		{"minio_cluster_health_drives_online_count", "drives_online", false, false, ""},
		{"minio_cluster_health_drives_offline_count", "drives_offline", false, false, ""},
		{"minio_cluster_health_capacity_usable_total_bytes", "capacity_total_bytes", false, false, ""},
		{"minio_cluster_health_capacity_usable_free_bytes", "capacity_free_bytes", false, false, ""},
	},
	"cluster/usage/objects": {
		{"minio_cluster_usage_objects_total_bytes", "scan_object_bytes", false, false, ""},
		{"minio_cluster_usage_objects_count", "scan_objects", false, false, ""},
		{"minio_cluster_usage_objects_buckets_count", "scan_buckets", false, false, ""},
		{"minio_cluster_usage_objects_since_last_update_seconds", "scan_age_seconds", false, false, ""},
	},
	"api/requests": {
		{"minio_api_requests_total", "requests_total", true, true, "name,server,type"},
		{"minio_api_requests_inflight_total", "inflight_requests", false, true, "name,server,type"},
		{"minio_api_requests_4xx_errors_total", "errors_4xx_total", true, true, "name,server,type"},
		{"minio_api_requests_5xx_errors_total", "errors_5xx_total", true, true, "name,server,type"},
		{"minio_api_requests_traffic_sent_bytes", "sent_bytes_total", true, false, "server,type"},
		{"minio_api_requests_traffic_received_bytes", "received_bytes_total", true, false, "server,type"},
	},
}

func parse(raw []byte, path, server string) (map[string]float64, string, error) {
	if len(raw) > MaxResponseBytes {
		return nil, "", ErrUnavailable
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(raw))
	if err != nil {
		return nil, "", ErrUnavailable
	}
	count := 0
	for _, family := range families {
		count += len(family.Metric)
	}
	if count > 4096 {
		return nil, "", ErrUnavailable
	}
	values := make(map[string]float64)
	for _, def := range catalogue[path] {
		family := families[def.input]
		if family == nil {
			continue
		} // Missing includes native omitted zero; never synthesize.
		kind := dto.MetricType_GAUGE
		if def.counter {
			kind = dto.MetricType_COUNTER
		}
		if family.GetType() != kind || !def.sum && len(family.Metric) != 1 {
			return nil, "", ErrUnavailable
		}
		seen := make(map[string]bool)
		for _, item := range family.Metric {
			labels := make(map[string]string)
			names := make([]string, 0, len(item.Label))
			for _, label := range item.Label {
				name, value := label.GetName(), label.GetValue()
				if _, exists := labels[name]; exists || value == "" || len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
					return nil, "", ErrUnavailable
				}
				labels[name] = value
				names = append(names, name)
			}
			sort.Strings(names)
			if strings.Join(names, ",") != def.labels || labels["type"] != "" && labels["type"] != "s3" {
				return nil, "", ErrUnavailable
			}
			if host := labels["server"]; host != "" {
				if server != "" && server != host {
					return nil, "", ErrUnavailable
				}
				server = host
			}
			key := labels["server"] + "\x00" + labels["type"] + "\x00" + labels["name"]
			if seen[key] {
				return nil, "", ErrUnavailable
			}
			seen[key] = true
			var v float64
			if def.counter {
				if item.Counter == nil {
					return nil, "", ErrUnavailable
				}
				v = item.Counter.GetValue()
			} else {
				if item.Gauge == nil {
					return nil, "", ErrUnavailable
				}
				v = item.Gauge.GetValue()
			}
			if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, "", ErrUnavailable
			}
			values[def.output] += v
			if math.IsInf(values[def.output], 0) {
				return nil, "", ErrUnavailable
			}
		}
	}
	return values, server, nil
}

func project(input map[string][]byte, boundary []byte, target Target, at, scanAt time.Time) (Snapshot, error) {
	values := make(map[string]float64)
	server := ""
	for _, path := range paths {
		group, host, err := parse(input[path], path, server)
		if err != nil {
			return Snapshot{}, err
		}
		server = host
		for key, value := range group {
			values[key] = value
		}
	}
	end, _, err := parse(boundary, paths[0], server)
	if err != nil {
		return Snapshot{}, err
	}
	start, ok := values["start_time_seconds"]
	uptime, up := values["uptime_seconds"]
	endStart, endOK := end["start_time_seconds"]
	endUptime, endUp := end["uptime_seconds"]
	if !ok || !up || !endOK || !endUp || server == "" || start <= 0 || start > float64(at.UnixNano())/1e9 || start != endStart || endUptime < uptime {
		return Snapshot{}, ErrUnavailable
	}
	if total, exists := values["capacity_total_bytes"]; exists {
		if free, exists := values["capacity_free_bytes"]; exists && free > total {
			return Snapshot{}, ErrUnavailable
		}
	}
	if age, scanned := values["scan_age_seconds"]; scanned {
		stamp := float64(scanAt.UnixNano())/1e9 - age
		if stamp <= 0 {
			return Snapshot{}, ErrUnavailable
		}
		values["scan_time_seconds"] = stamp
	} else {
		for _, key := range []string{"scan_object_bytes", "scan_objects", "scan_buckets"} {
			delete(values, key)
		}
	}
	delete(values, "scan_age_seconds")
	identity := sha256.Sum256([]byte(target.ContainerID + "\n" + target.ContainerStartedAt.UTC().Format(time.RFC3339Nano) + "\n" + strconv.FormatFloat(start, 'g', -1, 64)))
	return Snapshot{SampledAt: at, Epoch: hex.EncodeToString(identity[:]), Values: values}, nil
}

func Handler(read func(context.Context) (Snapshot, error)) http.Handler {
	return sourceserver.Handler(func(ctx context.Context) ([]byte, error) {
		snapshot, err := read(ctx)
		if err != nil {
			return nil, err
		}
		var body bytes.Buffer
		fmt.Fprintf(&body, "addp_minio_observed_at_seconds %d\n", snapshot.SampledAt.Unix())
		keys := make([]string, 0, len(snapshot.Values))
		for key := range snapshot.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			counter, known := false, key == "scan_time_seconds"
			for _, group := range catalogue {
				for _, def := range group {
					if def.output == key {
						counter = def.counter
						known = true
					}
				}
			}
			if !known {
				continue
			}
			if counter {
				fmt.Fprintf(&body, "addp_minio_%s{counter_epoch=%q} %s\n", key, snapshot.Epoch, strconv.FormatFloat(snapshot.Values[key], 'g', -1, 64))
			} else {
				fmt.Fprintf(&body, "addp_minio_%s %s\n", key, strconv.FormatFloat(snapshot.Values[key], 'g', -1, 64))
			}
		}
		return body.Bytes(), nil
	})
}
