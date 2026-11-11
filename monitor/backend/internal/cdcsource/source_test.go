package cdcsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

const brokerFixture = `# TYPE redpanda_storage_disk_total_bytes gauge
redpanda_storage_disk_total_bytes 100
# TYPE redpanda_storage_disk_free_bytes gauge
redpanda_storage_disk_free_bytes 40
# TYPE redpanda_cluster_unavailable_partitions gauge
redpanda_cluster_unavailable_partitions 0
`

func TestBrokerMissingIsNotZeroAndLagRequiresEveryPartition(t *testing.T) {
	v, err := broker([]byte(brokerFixture))
	if err != nil || v["consumer_group_partitions"] != 0 {
		t.Fatal(v, err)
	}
	if _, ok := v["consumer_group_pending_records"]; ok {
		t.Fatal("empty groups acquired a lag sample")
	}
	offsets := `# TYPE redpanda_kafka_max_offset gauge
redpanda_kafka_max_offset{redpanda_topic="private-topic",redpanda_partition="0"} 2
# TYPE redpanda_kafka_consumer_group_committed_offset gauge
redpanda_kafka_consumer_group_committed_offset{redpanda_topic="private-topic",redpanda_partition="0",redpanda_group="private-group"} 1
`
	v, err = broker([]byte(brokerFixture + offsets))
	if err != nil || v["consumer_group_pending_records"] != 1 {
		t.Fatal(v, err)
	}
	v, err = broker([]byte(brokerFixture + offsets + `redpanda_kafka_consumer_group_committed_offset{redpanda_topic="missing",redpanda_partition="1",redpanda_group="private-group"} 0` + "\n"))
	if err != nil || v["consumer_group_partitions"] != 2 {
		t.Fatal(v, err)
	}
	if _, ok := v["consumer_group_pending_records"]; ok {
		t.Fatal("partial lag published")
	}
	for _, raw := range []string{"", strings.Replace(brokerFixture, " 40", " NaN", 1), strings.Replace(brokerFixture, " 40", " 101", 1), brokerFixture + "redpanda_storage_disk_total_bytes 100\n"} {
		if _, err := broker([]byte(raw)); err != ErrUnavailable {
			t.Fatalf("invalid metrics accepted: %v", err)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConnectFixedPathsEmptyAndFailureProjection(t *testing.T) {
	for _, empty := range []bool{true, false} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			paths := []string{}
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				paths = append(paths, r.URL.RequestURI())
				body := "{}"
				if r.URL.Path == "/connectors" {
					if r.Method != "GET" || r.URL.RawQuery != "expand=status" {
						t.Fatal(r.URL)
					}
					if !empty {
						body = `{"private-connector":{"status":{"connector":{"state":"RUNNING"},"tasks":[{"id":0,"state":"FAILED","trace":"secret-password"}]}}}`
					}
				} else if r.URL.Path == "/jolokia/" {
					var request struct {
						Type      string
						MBean     string
						Attribute []string
					}
					if json.NewDecoder(r.Body).Decode(&request) != nil || r.Method != "POST" || request.Type != "read" {
						t.Fatal("non-read JMX")
					}
					if request.MBean != taskMBean && request.MBean != streamingMBean {
						t.Fatal("unexpected bean")
					}
					body = `{"status":404,"error_type":"javax.management.InstanceNotFoundException","error":"private bean missing"}`
					if !empty && request.MBean == taskMBean {
						body = `{"status":200,"value":{"private-bean":{"offset-commit-failure-percentage":0.25}}}`
					}
					if !empty && request.MBean == streamingMBean {
						body = `{"status":200,"value":{"private-stream":{"Connected":true,"MilliSecondsBehindSource":-5}}}`
					}
				} else {
					t.Fatal("unexpected path", r.URL)
				}
				code := 200
				if empty && r.URL.Path == "/jolokia/" {
					code = 404
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			raw, err := read(context.Background(), Target{Kind: "kafka-connect", Address: netip.MustParseAddr("127.0.0.1")}, client)
			if err != nil || len(paths) != 3 {
				t.Fatal(string(raw), err, paths)
			}
			out := string(raw)
			if strings.Contains(out, "private") || strings.Contains(out, "secret") {
				t.Fatal("sensitive upstream data leaked")
			}
			if empty {
				if !strings.Contains(out, "addp_cdc_connect_tasks 0\n") || strings.Contains(out, "source_delay_max_seconds") || strings.Contains(out, "failure_ratio_max") {
					t.Fatal(out)
				}
			} else if !strings.Contains(out, `task_states{state="FAILED"} 1`) || !strings.Contains(out, "source_delay_max_seconds -0.005") || !strings.Contains(out, "offset_commit_failure_ratio_max 0.25") {
				t.Fatal(out)
			}
		})
	}
}

func TestSourceRejectsUnreadableRedirectAndOversize(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{302, "secret"}, {200, strings.Repeat("x", MaxResponseBytes+1)}, {200, "not metrics"}} {
		client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}
		raw, err := read(context.Background(), Target{Kind: "redpanda", Address: netip.MustParseAddr("127.0.0.1")}, client)
		if err != ErrUnavailable || raw != nil {
			t.Fatal("upstream failure did not fail closed")
		}
	}
	for _, status := range []string{`null`, `{"a":{}}`, `{"a":{"status":{"connector":{"state":"UNKNOWN"},"tasks":[]}}}`, `{"a":{"status":{"connector":{"state":"RUNNING"}}}}`} {
		_, err := connect(func(uint16, string, []byte) ([]byte, error) { return []byte(status), nil })
		if err != ErrUnavailable {
			t.Fatal("invalid state accepted")
		}
	}
}

func TestSourceCardinalityBound(t *testing.T) {
	var raw strings.Builder
	raw.WriteString(brokerFixture)
	for i := 0; i < 4097; i++ {
		fmt.Fprintf(&raw, "irrelevant_metric{index=\"%d\"} 1\n", i)
	}
	if _, err := broker([]byte(raw.String())); err != ErrUnavailable {
		t.Fatal("sample limit ignored")
	}
	entries := map[string]any{}
	for i := 0; i < 257; i++ {
		entries[fmt.Sprint(i)] = map[string]any{"status": map[string]any{"connector": map[string]string{"state": "RUNNING"}, "tasks": []any{}}}
	}
	data, _ := json.Marshal(entries)
	if _, err := connect(func(uint16, string, []byte) ([]byte, error) { return data, nil }); err != ErrUnavailable {
		t.Fatal("connector limit ignored")
	}
}

func TestConnectOnlyTypedAbsentMBeanIsEmpty(t *testing.T) {
	for _, raw := range []string{
		`{"status":404,"error_type":"java.lang.IllegalArgumentException"}`,
		`{"status":403,"error_type":"java.lang.SecurityException"}`,
		`{"status":200,"value":{"task":{}}}`,
		`{"status":200,"value":{"task":{"offset-commit-failure-percentage":null}}}`,
	} {
		values, err := connect(func(_ uint16, path string, _ []byte) ([]byte, error) {
			if path == "/connectors?expand=status" {
				return []byte(`{}`), nil
			}
			return []byte(raw), nil
		})
		if err != ErrUnavailable || values != nil {
			t.Fatal("unavailable JMX became empty success")
		}
	}
}

func TestConnectedStreamWithoutAnEventHasNoDelaySample(t *testing.T) {
	values, err := connect(func(_ uint16, path string, body []byte) ([]byte, error) {
		if path == "/connectors?expand=status" {
			return []byte(`{}`), nil
		}
		if strings.Contains(string(body), "offset-commit-failure-percentage") {
			return []byte(`{"status":200,"value":{}}`), nil
		}
		return []byte(`{"status":200,"value":{"stream":{"Connected":true,"MilliSecondsBehindSource":-1}}}`), nil
	})
	if err != nil || values["streaming_connected"] != 1 || values["source_delay_connectors"] != 0 {
		t.Fatal(values, err)
	}
	if _, ok := values["source_delay_max_seconds"]; ok {
		t.Fatal("unset native delay became a real sample")
	}
}
