package processmetrics

import (
	"encoding/json"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/addp/common/config"
)

// Test-owned executable, built and launched only by the metrics T2 lifecycle.
// Each invocation is a distinct Linux process with its own native CPU/RSS.
func TestIntegrationMetricsSelfSource(t *testing.T) {
	if os.Getenv("ADDP_PROCESS_METRICS_T2_SOURCE") != "1" {
		t.Skip("requires standard metrics T2 lifecycle")
	}
	name, listen, endpoint, path := os.Getenv("ADDP_PROCESS_T2_INSTANCE"), os.Getenv("ADDP_PROCESS_T2_LISTEN"), os.Getenv("ADDP_PROCESS_T2_ENDPOINT"), os.Getenv("ADDP_PROCESS_T2_RECORD")
	identity := Identity{ModuleName: "monitor", Role: "worker", InstanceID: name, StartedAt: time.Now().UTC().Truncate(time.Microsecond)}
	source, err := Start(config.ProcessMetricsDeployment{ModuleName: identity.ModuleName, Role: identity.Role, Listen: listen, Endpoint: endpoint, TLSDir: "/source/process-tls"}, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	data, err := json.Marshal(map[string]any{"id": os.Getenv("ADDP_PROCESS_T2_ID"), "module_name": identity.ModuleName, "instance_id": name, "role": identity.Role, "started_at": identity.StartedAt, "instance": listen})
	if err != nil || os.WriteFile(path, data, 0666) != nil {
		t.Fatal("write test-owned identity")
	}
	allocation := make([]byte, 4<<20)
	for i := range allocation {
		allocation[i] = byte(i)
	}
	end := time.Now().Add(8 * time.Minute)
	for time.Now().Before(end) {
		if _, err := os.Stat(path + ".stop"); err == nil {
			runtime.KeepAlive(allocation)
			return
		}
		// Bounded real CPU work; no fake counters and no memory growth.
		finish := time.Now().Add(5 * time.Millisecond)
		for time.Now().Before(finish) {
			for i := 0; i < len(allocation); i += 4096 {
				allocation[i]++
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("standard gate did not stop the test-owned source")
}
