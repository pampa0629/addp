package processmetrics

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/addp/common/config"
	"github.com/addp/common/httpmetrics"
	"github.com/gin-gonic/gin"
)

// Test-owned executable, built and launched only by the metrics T2 lifecycle.
// Each invocation is a distinct Linux process with its own native CPU/RSS.
func TestIntegrationMetricsSelfSource(t *testing.T) {
	if os.Getenv("ADDP_PROCESS_METRICS_T2_SOURCE") != "1" {
		t.Skip("requires standard metrics T2 lifecycle")
	}
	name, listen, endpoint, path := os.Getenv("ADDP_PROCESS_T2_INSTANCE"), os.Getenv("ADDP_PROCESS_T2_LISTEN"), os.Getenv("ADDP_PROCESS_T2_ENDPOINT"), os.Getenv("ADDP_PROCESS_T2_RECORD")
	identity := Identity{ModuleName: "monitor", Role: "worker", InstanceID: name, StartedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if os.Getenv("ADDP_PROCESS_T2_ROLE") == "backend" {
		identity.Role = "backend"
	}
	source, err := Start(config.ProcessMetricsDeployment{ModuleName: identity.ModuleName, Role: identity.Role, Listen: listen, Endpoint: endpoint, TLSDir: "/source/process-tls"}, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	var business *httptest.Server
	if identity.Role == "backend" {
		router := gin.New()
		router.Use(httpmetrics.Process().Middleware(), httpmetrics.Recovery())
		router.GET("/items/:id", func(c *gin.Context) { c.Status(204) })
		router.GET("/slow", func(c *gin.Context) { time.Sleep(20 * time.Millisecond); c.Status(204) })
		router.GET("/failure", func(c *gin.Context) {
			mode, _ := os.ReadFile(path + ".http-mode")
			if string(mode) == "healthy" {
				c.Status(204)
			} else {
				c.Status(503)
			}
		})
		business = httptest.NewServer(router)
		defer business.Close()
	}
	data, err := json.Marshal(map[string]any{"id": os.Getenv("ADDP_PROCESS_T2_ID"), "module_name": identity.ModuleName, "instance_id": name, "role": identity.Role, "started_at": identity.StartedAt, "instance": listen})
	if err != nil || os.WriteFile(path, data, 0666) != nil {
		t.Fatal("write test-owned identity")
	}
	allocation := make([]byte, 4<<20)
	for i := range allocation {
		allocation[i] = byte(i)
	}
	end := time.Now().Add(8 * time.Minute)
	requestIndex := 0
	for time.Now().Before(end) {
		if _, err := os.Stat(path + ".stop"); err == nil {
			runtime.KeepAlive(allocation)
			return
		}
		if business != nil {
			paths := []string{"/items/fixture-id", "/slow", "/failure", "/missing/fixture-id"}
			response, err := business.Client().Get(business.URL + paths[requestIndex%len(paths)])
			if err != nil {
				t.Fatal("owned HTTP fixture request failed")
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			requestIndex++
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
