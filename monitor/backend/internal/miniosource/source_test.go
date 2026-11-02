package miniosource

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

const processFixture = `# TYPE minio_system_process_start_time_seconds gauge
minio_system_process_start_time_seconds{server="private-address:9000"} 1700000000
# TYPE minio_system_process_uptime_seconds gauge
minio_system_process_uptime_seconds{server="private-address:9000"} 100
`
const healthFixture = `# TYPE minio_cluster_health_drives_online_count gauge
minio_cluster_health_drives_online_count 1
# TYPE minio_cluster_health_capacity_usable_total_bytes gauge
minio_cluster_health_capacity_usable_total_bytes 1024
# TYPE minio_cluster_health_capacity_usable_free_bytes gauge
minio_cluster_health_capacity_usable_free_bytes 256
`
const usageFixture = `# TYPE minio_cluster_usage_objects_total_bytes gauge
minio_cluster_usage_objects_total_bytes 125
# TYPE minio_cluster_usage_objects_count gauge
minio_cluster_usage_objects_count 2
# TYPE minio_cluster_usage_objects_buckets_count gauge
minio_cluster_usage_objects_buckets_count 1
# TYPE minio_cluster_usage_objects_since_last_update_seconds gauge
minio_cluster_usage_objects_since_last_update_seconds 60
`
const apiFixture = `# TYPE minio_api_requests_total counter
minio_api_requests_total{server="private-address:9000",type="s3",name="GetObject"} 8
minio_api_requests_total{server="private-address:9000",type="s3",name="PutObject"} 2
# TYPE minio_api_requests_inflight_total gauge
minio_api_requests_inflight_total{server="private-address:9000",type="s3",name="GetObject"} 1
# TYPE minio_api_requests_4xx_errors_total counter
minio_api_requests_4xx_errors_total{server="private-address:9000",type="s3",name="GetObject"} 3
# TYPE minio_api_requests_traffic_sent_bytes counter
minio_api_requests_traffic_sent_bytes{server="private-address:9000",type="s3"} 400
# TYPE private_bucket_metadata gauge
private_bucket_metadata{bucket="must-not-escape"} 9
`

func fixture() map[string][]byte {
	return map[string][]byte{"system/process": []byte(processFixture), "cluster/health": []byte(healthFixture), "cluster/usage/objects": []byte(usageFixture), "api/requests": []byte(apiFixture)}
}
func targetFixture() Target {
	return Target{Address: netip.MustParseAddrPort("127.0.0.1:9000"), Scope: "infra", AccessKey: "ADDPAABBCCDDEEFF0011", SecretKey: "private-source-secret", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(1699999999, 0)}
}

func TestMinIOFixedProjectionPreservesOmissionAndPrivateEpoch(t *testing.T) {
	at := time.Unix(1700000100, 0)
	snapshot, err := project(fixture(), []byte(processFixture), targetFixture(), at, at)
	if err != nil || snapshot.Values["requests_total"] != 10 || snapshot.Values["scan_time_seconds"] != 1700000040 || snapshot.Values["capacity_free_bytes"] != 256 {
		t.Fatal("native fields were not mapped correctly", err)
	}
	staggered, err := project(fixture(), []byte(processFixture), targetFixture(), at, at.Add(-2*time.Second))
	if err != nil || staggered.Values["scan_time_seconds"] != 1700000038 || !staggered.SampledAt.Equal(at) {
		t.Fatal("scan observation was confused with final collection time")
	}
	for _, key := range []string{"errors_5xx_total", "drives_offline", "received_bytes_total"} {
		if _, ok := snapshot.Values[key]; ok {
			t.Fatal("omitted native statistics were fabricated")
		}
	}
	input := fixture()
	input["cluster/usage/objects"] = nil
	withoutScan, err := project(input, []byte(processFixture), targetFixture(), at, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := withoutScan.Values["scan_time_seconds"]; ok {
		t.Fatal("unscanned usage was dated")
	}
	input["cluster/health"] = append(input["cluster/health"], []byte("# TYPE minio_cluster_health_drives_offline_count gauge\nminio_cluster_health_drives_offline_count 0\n")...)
	explicit, err := project(input, []byte(processFixture), targetFixture(), at, at)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := explicit.Values["drives_offline"]; !ok || value != 0 {
		t.Fatal("an explicit real zero was lost")
	}
	other := targetFixture()
	other.ContainerID = strings.Repeat("b", 64)
	changed, err := project(fixture(), []byte(processFixture), other, at, at)
	if err != nil || changed.Epoch == snapshot.Epoch {
		t.Fatal("different deployments shared a counter generation")
	}
	snapshot.Values["private_bucket_name"] = 12
	handler := Handler(func(context.Context) (Snapshot, error) { return snapshot, nil })
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "addp_minio_requests_total{counter_epoch=") {
		t.Fatal("projection not published")
	}
	for _, private := range []string{"must-not-escape", "private-address", "GetObject", targetFixture().ContainerID, targetFixture().SecretKey, "private_bucket_name"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("native metadata escaped")
		}
	}
}

func TestMinIORestartsAndMalformedSamplesFailClosed(t *testing.T) {
	for name, change := range map[string]func(map[string][]byte, *[]byte){
		"restart": func(_ map[string][]byte, end *[]byte) {
			*end = []byte(strings.ReplaceAll(processFixture, "1700000000", "1700000001"))
		},
		"uptime rewind": func(_ map[string][]byte, end *[]byte) {
			*end = []byte(strings.ReplaceAll(processFixture, "} 100", "} 90"))
		},
		"server changed": func(input map[string][]byte, _ *[]byte) {
			input["api/requests"] = []byte(strings.ReplaceAll(apiFixture, "private-address:9000", "another-node:9000"))
		},
		"duplicate": func(input map[string][]byte, _ *[]byte) {
			input["api/requests"] = append(input["api/requests"], []byte("minio_api_requests_total{server=\"private-address:9000\",type=\"s3\",name=\"GetObject\"} 7\n")...)
		},
		"bucket label": func(input map[string][]byte, _ *[]byte) {
			input["api/requests"] = []byte(strings.ReplaceAll(apiFixture, "type=\"s3\",name=\"GetObject\"", "type=\"s3\",name=\"GetObject\",bucket=\"private\""))
		},
		"not s3": func(input map[string][]byte, _ *[]byte) {
			input["api/requests"] = []byte(strings.ReplaceAll(apiFixture, "type=\"s3\"", "type=\"admin\""))
		},
		"negative": func(input map[string][]byte, _ *[]byte) {
			input["cluster/health"] = []byte(strings.ReplaceAll(healthFixture, " 1024", " -1"))
		},
		"nan": func(input map[string][]byte, _ *[]byte) {
			input["cluster/health"] = []byte(strings.ReplaceAll(healthFixture, " 1024", " NaN"))
		},
		"type mismatch": func(input map[string][]byte, _ *[]byte) {
			input["api/requests"] = []byte(strings.Replace(apiFixture, "requests_total counter", "requests_total gauge", 1))
		},
		"over budget": func(input map[string][]byte, _ *[]byte) {
			input["cluster/health"] = []byte(strings.Repeat(" ", MaxResponseBytes+1))
		},
		"no process": func(input map[string][]byte, _ *[]byte) { input["system/process"] = nil },
	} {
		t.Run(name, func(t *testing.T) {
			input := fixture()
			end := []byte(processFixture)
			change(input, &end)
			if _, err := project(input, end, targetFixture(), time.Unix(1700000100, 0), time.Unix(1700000100, 0)); err == nil {
				t.Fatal("invalid source became a successful snapshot")
			}
		})
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMinIOReadsOnlyFiveFixedRequestsWithShortCredentialsAndNoPartialFailure(t *testing.T) {
	input := fixture()
	var calls []string
	fail := ""
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.RawQuery != "" || r.URL.Host != "127.0.0.1:9000" {
			t.Fatal("unexpected upstream request")
		}
		path := strings.TrimPrefix(r.URL.Path, "/minio/metrics/v3/")
		calls = append(calls, path)
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		if len(parts) != 3 {
			t.Fatal("missing token")
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			t.Fatal(err)
		}
		var claims map[string]any
		_ = json.Unmarshal(payload, &claims)
		if claims["iss"] != "prometheus" || claims["sub"] != targetFixture().AccessKey || claims["exp"].(float64) > float64(time.Now().Unix()+300) {
			t.Fatal("unbounded or wrong-purpose token")
		}
		code := 200
		body := input[path]
		if path == fail {
			code = 403
			body = []byte("private-upstream-error")
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	if _, err := read(context.Background(), targetFixture(), client); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"system/process", "cluster/health", "cluster/usage/objects", "api/requests", "system/process"}) {
		t.Fatal("endpoint scope changed", calls)
	}
	calls = nil
	fail = "cluster/health"
	snapshot, err := read(context.Background(), targetFixture(), client)
	if err == nil || snapshot.Values != nil || len(calls) != 2 || strings.Contains(err.Error(), "private") {
		t.Fatal("failure retried, leaked detail or returned partial data")
	}
	for _, address := range []string{"8.8.8.8:9000", "127.0.0.1:8180", "[::ffff:127.0.0.1]:9000"} {
		target := targetFixture()
		target.Address = netip.MustParseAddrPort(address)
		if target.Valid() {
			t.Fatal("unbounded target accepted")
		}
	}
}
