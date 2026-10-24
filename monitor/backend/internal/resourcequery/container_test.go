package resourcequery

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/addp/common/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContainerUsesExactCgroupAndOriginalCompleteCPUWindow(t *testing.T) {
	at := testTime
	cpu := .5
	mem := uint64(128 << 20)
	boundary := models.CgroupIdentity{BootID: "11111111-1111-4111-8111-111111111111", CgroupID: "100", PIDNamespaceID: "200"}
	proof := &models.ContainerProof{Identity: boundary, DockerEngineID: "engine", ContainerID: strings.Repeat("a", 64), ModuleName: "monitor", Role: "worker", StartedAt: at.Add(-time.Hour), ObservedAt: at, StableSince: at.Add(-2 * time.Minute), HierarchyDigest: strings.Repeat("b", 64), CPUCores: &cpu, MemoryBytes: &mem}
	scope := ProcessScope{ID: 1, ModuleName: "monitor", InstanceID: "current", Role: "worker", Instance: "127.0.0.1:9443", StartedAt: at.Add(-time.Hour), Cgroup: &boundary, Container: proof}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.ParseForm() != nil {
			t.Fatal("parse")
		}
		query := r.Form.Get("query")
		if strings.Contains(query, `"signal"`) {
			wire := collectionWire(t, map[string]string{"up_count": "1", "up": "1", "sampled_at": fmt.Sprint(at.Unix()), "identity_valid": "1"})
			rows := []map[string]any{}
			for _, row := range wire.Data.Result {
				rows = append(rows, map[string]any{"metric": row.Metric, "value": row.Value})
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": rows}})
			return
		}
		for _, part := range []string{"addp_cgroup_cpu_usage_seconds_total", "addp_cgroup_memory_current_bytes", `kernel_boot_id="` + boundary.BootID + `"`, `cgroup_id="100"`, `pid_namespace_id="200"`, "addp_process_identity_info", "offset 1m", "count_over_time(", ">=4", "resets(", "time()-76"} {
			if !strings.Contains(query, part) {
				t.Error("guard missing", part)
			}
		}
		if strings.Contains(query, "process_resident_memory_bytes") || strings.Contains(query, "process_cpu_seconds_total{") {
			t.Error("process and container usage mixed")
		}
		rows := []map[string]any{}
		for i, d := range processDefinitions[:2] {
			for _, component := range []string{"value", "sampled_at"} {
				value := .25
				if i == 1 {
					value = float64(64 << 20)
				}
				if component == "sampled_at" {
					value = float64(at.Unix())
				}
				rows = append(rows, map[string]any{"metric": map[string]string{"addp_metric": d.Key, "addp_component": component}, "value": []any{at.Unix(), fmt.Sprint(value)}})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": rows}})
	}))
	defer server.Close()
	client := &Client{origin: server.URL, http: server.Client()}
	data, err := client.ContainerObservation(context.Background(), scope, at, DefaultBudget())
	if err != nil || len(data.Series) != 4 || *data.Series[2].Points[0].Value != 50 || *data.Series[3].Points[0].Value != 50 {
		t.Fatal("incorrect cgroup ratios", data, err)
	}
	scope.Container = nil
	data, err = client.ContainerObservation(context.Background(), scope, at, DefaultBudget())
	if err != nil || data.Series[0].Points[0].Value == nil || data.Series[2].Points[0].Value != nil || data.Series[3].Points[0].Value != nil {
		t.Fatal("unverified proof produced a ratio", err)
	}
	scope.Container = proof
	proof.StableSince = at.Add(-time.Minute)
	data, err = client.ContainerObservation(context.Background(), scope, at, DefaultBudget())
	if err != nil || data.Series[2].Points[0].Value != nil || data.Series[3].Points[0].Value == nil {
		t.Fatal("short CPU configuration window accepted", err)
	}
	scope.Cgroup = nil
	scope.Container = nil
	_, err = client.ContainerObservation(context.Background(), scope, at, DefaultBudget())
	if err != nil || calls != 6 {
		t.Fatal("missing cgroup caused a query", calls, err)
	}
	if _, err = (*Client)(nil).ContainerObservation(context.Background(), scope, at, DefaultBudget()); err != ErrUnavailable {
		t.Fatal("nil client did not fail closed", err)
	}
}
