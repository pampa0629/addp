package resourcequery

import (
	"fmt"
	"strconv"
	"strings"
)

var minioDefinitions = []Definition{
	{"minio.uptime_seconds", "seconds", 0},
	{"minio.drives.online", "drives", 0}, {"minio.drives.offline", "drives", 0},
	{"minio.capacity.total_bytes", "bytes", 0}, {"minio.capacity.free_bytes", "bytes", 0}, {"minio.capacity.used_percent", "percent", 0},
	{"minio.scan.object_bytes", "bytes", 0}, {"minio.scan.objects", "objects", 0}, {"minio.scan.buckets", "buckets", 0}, {"minio.scan.age_seconds", "seconds", 0},
	{"minio.requests.per_second", "requests_per_second", 60}, {"minio.requests.inflight", "requests", 0},
	{"minio.requests.errors_4xx_per_second", "requests_per_second", 60}, {"minio.requests.errors_5xx_per_second", "requests_per_second", 60},
	{"minio.requests.error_5xx_percent", "percent", 60},
	{"minio.traffic.sent_bytes_per_second", "bytes_per_second", 60}, {"minio.traffic.received_bytes_per_second", "bytes_per_second", 60},
}

var minioPhysical = map[string]string{
	"minio.uptime_seconds": "uptime_seconds", "minio.drives.online": "drives_online", "minio.drives.offline": "drives_offline",
	"minio.capacity.total_bytes": "capacity_total_bytes", "minio.capacity.free_bytes": "capacity_free_bytes",
	"minio.scan.object_bytes": "scan_object_bytes", "minio.scan.objects": "scan_objects", "minio.scan.buckets": "scan_buckets",
	"minio.requests.per_second": "requests_total", "minio.requests.inflight": "inflight_requests",
	"minio.requests.errors_4xx_per_second": "errors_4xx_total", "minio.requests.errors_5xx_per_second": "errors_5xx_total",
	"minio.traffic.sent_bytes_per_second": "sent_bytes_total", "minio.traffic.received_bytes_per_second": "received_bytes_total",
}

func MinIOCatalog() []Definition { return append([]Definition(nil), minioDefinitions...) }

// Optional native fields (including omitted zeroes) never gate unrelated values.
func (s EngineScope) minioFieldGuard(field, offset string) string {
	raw := s.selector(field) + offset
	return ` and (count(` + raw + `)==1) and (min(` + raw + `)>=0) and (max(timestamp(` + raw + `))==max(timestamp(` + s.selector("up") + offset + `)))`
}

func (s EngineScope) minioSnapshotGuard(offset string) string {
	up := s.selector("up") + offset
	stamp := `max(timestamp(` + up + `))`
	age := "16"
	if offset != "" {
		age = "76"
	}
	guard := ` and (count(` + up + `)==1) and (max(` + up + `)==1) and (` + stamp + `>=time()-` + age + `)`
	for _, field := range []string{"observed_at_seconds", "start_time_seconds", "uptime_seconds"} {
		guard += s.minioFieldGuard(field, offset)
	}
	observed, start, uptime := `max(`+s.selector("observed_at_seconds")+offset+`)`, `max(`+s.selector("start_time_seconds")+offset+`)`, `max(`+s.selector("uptime_seconds")+offset+`)`
	return guard + ` and (` + observed + `>=` + stamp + `-5) and (` + observed + `<=` + stamp + `+5) and (` + start + `>0) and (` + start + `>=` + strconv.FormatInt(s.Source.StartedAt.Unix()-1, 10) + `) and (` + start + `<=` + observed + `) and (abs(` + observed + `-` + start + `-` + uptime + `)<=5)`
}

func (s EngineScope) minioRate(field string) (string, string) {
	raw := s.selector(field)
	valid := counterWindow(raw)
	epoch := strings.TrimSuffix(raw, "}") + `,counter_epoch=~"[0-9a-f]{64}"}`
	guard := s.minioFieldGuard(field, "") + s.minioFieldGuard(field, " offset 1m") + s.minioSnapshotGuard(" offset 1m")
	guard += ` and (count(` + epoch + `)==1) and (count(` + raw + ` and ` + raw + ` offset 1m)==1) and (count(` + valid + `)==1) and (max(` + s.selector("start_time_seconds") + `)==max(` + s.selector("start_time_seconds") + ` offset 1m))`
	return `max(` + valid + `)`, guard
}

func (s EngineScope) minioExpression(p Plan) (string, error) {
	if len(p.Metrics) == 0 || len(p.Metrics) > len(minioDefinitions) {
		return "", ErrInvalid
	}
	seen, parts := map[string]bool{}, []string{}
	for _, d := range p.Metrics {
		var canonical *Definition
		for i := range minioDefinitions {
			if minioDefinitions[i].Key == d.Key {
				canonical = &minioDefinitions[i]
				break
			}
		}
		if canonical == nil || *canonical != d || seen[d.Key] {
			return "", ErrInvalid
		}
		seen[d.Key] = true
		guard := s.minioSnapshotGuard("")
		field, ok := minioPhysical[d.Key]
		value := `max(` + s.selector(field) + `)`
		switch d.Key {
		case "minio.capacity.used_percent":
			total, free := `max(`+s.selector("capacity_total_bytes")+`)`, `max(`+s.selector("capacity_free_bytes")+`)`
			value = `100*(` + total + `-` + free + `)/` + total
			guard += s.minioFieldGuard("capacity_total_bytes", "") + s.minioFieldGuard("capacity_free_bytes", "") + ` and (` + total + `>0) and (` + free + `<=` + total + `)`
		case "minio.scan.age_seconds":
			value = `time()-max(` + s.selector("scan_time_seconds") + `)`
		case "minio.requests.error_5xx_percent":
			requests, requestGuard := s.minioRate("requests_total")
			errors, errorGuard := s.minioRate("errors_5xx_total")
			value = `100*` + errors + `/` + requests
			guard += requestGuard + errorGuard + ` and (` + requests + `>0) and (` + errors + `<=` + requests + `) and (count(` + s.selector("requests_total") + ` and ` + s.selector("errors_5xx_total") + `)==1)`
		default:
			if !ok {
				return "", ErrInvalid
			}
			if d.WindowSeconds == 60 {
				var eligible string
				value, eligible = s.minioRate(field)
				guard += eligible
			} else {
				guard += s.minioFieldGuard(field, "")
			}
		}
		if strings.HasPrefix(d.Key, "minio.scan.") {
			guard += s.minioFieldGuard("scan_time_seconds", "") + ` and (max(` + s.selector("scan_time_seconds") + `)>0) and (max(` + s.selector("scan_time_seconds") + `)<=max(` + s.selector("observed_at_seconds") + `))`
		}
		for _, item := range []struct{ key, value string }{{"value", value}, {"sampled_at", `max(timestamp(` + s.selector("up") + `))`}} {
			parts = append(parts, fmt.Sprintf(`label_replace(label_replace((%s%s),"addp_metric",%s,"",""),"addp_component",%s,"","")`, item.value, guard, strconv.Quote(d.Key), strconv.Quote(item.key)))
		}
	}
	return strings.Join(parts, " or "), nil
}
