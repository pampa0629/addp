package main

import (
	"context"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKubeUsageRequiresCurrentTimestampAndBothContainerLabels(t *testing.T) {
	now := time.Unix(1800000000, 0)
	raw := fmt.Sprintf(`# TYPE container_cpu_usage_seconds_total counter
container_cpu_usage_seconds_total{container="monitor-frontend",namespace="site",pod="monitor-pod"} 0 %d
container_cpu_usage_seconds_total{container="monitor-frontend",namespace="foreign",pod="monitor-pod"} 23 %d
container_cpu_usage_seconds_total{container="other",namespace="site",pod="stale"} 5 %d
# TYPE container_memory_working_set_bytes gauge
container_memory_working_set_bytes{container="monitor-frontend",namespace="site",pod="monitor-pod"} 8192 %d
`, now.UnixMilli(), now.UnixMilli(), now.Add(-31*time.Second).UnixMilli(), now.UnixMilli())
	rows, err := kubeSamples([]byte(raw), "site", now)
	s := rows[[2]string{"monitor-pod", "monitor-frontend"}]
	if err != nil || len(rows) != 1 || s == nil || s.cpu == nil || *s.cpu != 0 || s.memory == nil || *s.memory != 8192 {
		t.Fatalf("samples=%v err=%v", rows, err)
	}
}

func TestKubeObservationUsesVerifiedContainerGeneration(t *testing.T) {
	for _, tc := range []struct {
		name             string
		replace, foreign bool
	}{{"same generation", false, false}, {"replaced during read", true, false}, {"foreign controller", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			podReads := 0
			api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer projected" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/deployments/console"):
					io.WriteString(w, `{"metadata":{"name":"console","namespace":"site","uid":"deployment"}}`)
				case strings.HasSuffix(r.URL.Path, "/replicasets"):
					owner := "deployment"
					if tc.foreign {
						owner = "unrelated"
					}
					fmt.Fprintf(w, `{"items":[{"metadata":{"uid":"rs","namespace":"site","ownerReferences":[{"apiVersion":"apps/v1","kind":"Deployment","uid":"%s","controller":true}]}}]}`, owner)
				case strings.HasSuffix(r.URL.Path, "/pods"):
					if r.URL.Query().Get("fieldSelector") != "spec.nodeName=node" {
						t.Error("node filter missing")
					}
					podReads++
					id := "first"
					if tc.replace && podReads == 2 {
						id = "second"
					}
					fmt.Fprintf(w, `{"items":[{"metadata":{"name":"console-pod","uid":"pod","namespace":"site","ownerReferences":[{"apiVersion":"apps/v1","kind":"ReplicaSet","uid":"rs","controller":true}]},"spec":{"nodeName":"node"},"status":{"containerStatuses":[{"name":"console","containerID":"%s","restartCount":1,"state":{"running":{"startedAt":"2020-01-01T00:00:00Z"}}}]}}]}`, id)
				case r.URL.Path == "/metrics/resource":
					ts := time.Now().UnixMilli()
					fmt.Fprintf(w, `# TYPE container_cpu_usage_seconds_total counter
container_cpu_usage_seconds_total{container="console",namespace="site",pod="console-pod"} 12 %d
# TYPE container_memory_working_set_bytes gauge
container_memory_working_set_bytes{container="console",namespace="site",pod="console-pod"} 32768 %d
`, ts, ts)
				default:
					http.NotFound(w, r)
				}
			}))
			defer api.Close()
			k := kubeConfig{API: api.URL, Kubelet: api.URL, Node: "node", Namespace: "site", CAFile: filepath.Join(t.TempDir(), "ca"), TokenFile: filepath.Join(t.TempDir(), "token")}
			if err := os.WriteFile(k.CAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(k.TokenFile, []byte("projected"), 0600); err != nil {
				t.Fatal(err)
			}
			rows, resources, err := k.snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			r := resources["console"]
			if tc.foreign {
				if len(rows) != 0 {
					t.Fatal("foreign deployment reported")
				}
				return
			}
			if len(rows) != 1 || rows[0].module != "console" {
				t.Fatal(rows)
			}
			if tc.replace {
				if r != nil {
					t.Fatal("replaced generation admitted")
				}
				return
			}
			if r == nil || r.generation != "pod/first" || r.cpuSeconds != 12 || r.memoryBytes != 32768 || *r.restarts != 1 {
				t.Fatal(r)
			}
		})
	}
}
