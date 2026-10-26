package processmetrics

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
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
	source, err := Start(config.ProcessMetricsDeployment{ModuleName: identity.ModuleName, Role: identity.Role, Listen: listen, Endpoint: endpoint, TLSDir: "/source/process-tls", CgroupV2: true}, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	var business *httptest.Server
	traffic := newHTTPFixtureTraffic(time.Now())
	var healthy atomic.Bool
	if identity.Role == "backend" {
		router := gin.New()
		router.Use(httpmetrics.Process().Middleware(), httpmetrics.Recovery())
		router.GET("/items/:id", func(c *gin.Context) { c.Status(204) })
		router.GET("/slow", func(c *gin.Context) { time.Sleep(20 * time.Millisecond); c.Status(204) })
		router.GET("/failure", func(c *gin.Context) {
			if healthy.Load() {
				c.Status(204)
			} else {
				c.Status(503)
			}
		})
		business = httptest.NewServer(router)
		defer business.Close()
		traffic.persist(t, path)
	}
	data, err := json.Marshal(map[string]any{"id": os.Getenv("ADDP_PROCESS_T2_ID"), "module_name": identity.ModuleName, "instance_id": name, "role": identity.Role, "started_at": identity.StartedAt, "instance": listen, "cgroup": source.Declaration().Cgroup, "cgroup_diagnostic": integrationCgroupFacts()})
	if err != nil || os.WriteFile(path, data, 0666) != nil {
		t.Fatal("write test-owned identity")
	}
	allocation := make([]byte, 4<<20)
	for i := range allocation {
		allocation[i] = byte(i)
	}
	end := time.Now().Add(8 * time.Minute)
	controlled := os.Getenv("ADDP_PROCESS_T2_RESOURCE_CONTROL") == "1"
	var extra []byte
	var activeUntil time.Time
	activated := false
	for time.Now().Before(end) {
		if _, err := os.Stat(path + ".stop"); err == nil {
			runtime.KeepAlive(allocation)
			return
		}
		if controlled {
			mode, _ := os.ReadFile(path + ".resource-mode")
			if string(mode) == "active" && !activated {
				activated = true
				activeUntil = time.Now().Add(180 * time.Second)
				extra = make([]byte, 24<<20)
				for i := range extra {
					extra[i] = byte(i)
				}
			}
			if string(mode) != "active" || !time.Now().Before(activeUntil) {
				if extra != nil {
					extra = nil
					debug.FreeOSMemory()
				}
				time.Sleep(40 * time.Millisecond)
				continue
			}
			runtime.KeepAlive(extra)
		}
		if business != nil {
			mode, err := os.ReadFile(path + ".http-mode")
			if err != nil && !os.IsNotExist(err) {
				t.Fatal("read HTTP fixture phase")
			}
			previousPhase := traffic.Phase
			requestPath, send := traffic.take(time.Now(), string(mode) == "healthy")
			healthy.Store(traffic.Phase == "healthy")
			if send || traffic.Phase != previousPhase {
				traffic.persist(t, path)
			}
			if send {
				response, err := business.Client().Get(business.URL + requestPath)
				if err != nil {
					t.Fatal("owned HTTP fixture request failed")
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
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

// Fixture-only scheduling: unused failure capacity cannot consume recovery.
// Keeping nextRequest across the one-way switch also prevents a switch burst.
type httpFixtureTraffic struct {
	Phase                string `json:"phase"`
	FailureAttempts      int    `json:"failure_attempts"`
	HealthyAttempts      int    `json:"healthy_attempts"`
	FailureLimit         int    `json:"failure_limit"`
	HealthyLimit         int    `json:"healthy_limit"`
	FailureLastMS        *int64 `json:"failure_last_ms"`
	HealthyLastMS        *int64 `json:"healthy_last_ms"`
	started, nextRequest time.Time
}

func newHTTPFixtureTraffic(start time.Time) *httpFixtureTraffic {
	return &httpFixtureTraffic{Phase: "failure", FailureLimit: 90, HealthyLimit: 90, started: start, nextRequest: start}
}

func (p *httpFixtureTraffic) take(now time.Time, healthy bool) (string, bool) {
	if healthy {
		p.Phase = "healthy"
	}
	count, limit, interval := &p.FailureAttempts, p.FailureLimit, 2*time.Second
	last := &p.FailureLastMS
	if p.Phase == "healthy" {
		count, limit, interval, last = &p.HealthyAttempts, p.HealthyLimit, time.Second, &p.HealthyLastMS
	}
	if *count >= limit || now.Before(p.nextRequest) {
		return "", false
	}
	paths := []string{"/items/fixture-id", "/slow", "/failure", "/missing/fixture-id"}
	path := paths[(p.FailureAttempts+p.HealthyAttempts)%len(paths)]
	*count++
	elapsed := now.Sub(p.started).Milliseconds()
	*last = &elapsed
	p.nextRequest = now.Add(interval)
	return path, true
}

func (p *httpFixtureTraffic) persist(t *testing.T, path string) {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil || os.WriteFile(path+".http-load.json", data, 0666) != nil || os.WriteFile(path+".requests", []byte(strconv.Itoa(p.FailureAttempts+p.HealthyAttempts)), 0666) != nil {
		t.Fatal("persist HTTP fixture attempt counts")
	}
}

// Only the owned T2 executable calls this. Emit fixed booleans, never raw
// proc paths, kernel file contents, endpoints or environment values.
func integrationCgroupFacts() map[string]bool {
	facts := map[string]bool{"self_sample_valid": sampleSelfCgroup().valid()}
	root, err := os.OpenRoot("/sys/fs/cgroup")
	facts["root_readable"] = err == nil
	if err != nil {
		return facts
	}
	defer root.Close()
	data, err := readCgroupFile(root.FS(), "cpu.stat")
	facts["cpu_stat_readable"] = err == nil
	if err == nil {
		facts["core_sched_field_present"] = strings.Contains("\n"+string(data), "\ncore_sched.force_idle_usec ")
	}
	_, _, err = readCgroupUsage(root.FS(), ".")
	facts["root_usage_parse_valid"] = err == nil
	return facts
}

func TestHTTPFixtureTrafficReservesRecoveryAfterFailureBudgetExhaustion(t *testing.T) {
	start := time.Unix(1000, 0)
	traffic := newHTTPFixtureTraffic(start)
	paths := []string{"/items/fixture-id", "/slow", "/failure", "/missing/fixture-id"}
	for i := 0; i < 90; i++ {
		at := start.Add(time.Duration(i) * 2 * time.Second)
		path, ok := traffic.take(at, false)
		if !ok || path != paths[i%4] {
			t.Fatal("failure phase template or cadence", i, path, ok)
		}
		if _, ok := traffic.take(at.Add(time.Second), false); ok {
			t.Fatal("failure phase burst")
		}
	}
	if _, ok := traffic.take(start.Add(300*time.Second), false); ok || traffic.HealthyAttempts != 0 {
		t.Fatal("failure exhausted recovery allocation")
	}
	for i := 0; i < 90; i++ {
		at := start.Add(time.Duration(301+i) * time.Second)
		// Removing the control file during cleanup must not refill either phase.
		path, ok := traffic.take(at, i == 0)
		if !ok || path != paths[(90+i)%4] {
			t.Fatal("late recovery did not use reserved requests", i, path, ok)
		}
		if _, ok := traffic.take(at.Add(500*time.Millisecond), true); ok {
			t.Fatal("recovery phase burst")
		}
	}
	if _, ok := traffic.take(start.Add(450*time.Second), true); ok || traffic.FailureAttempts != 90 || traffic.HealthyAttempts != 90 {
		t.Fatal("phase reset or request cap violated", traffic)
	}
	if *traffic.FailureLastMS != 178000 || *traffic.HealthyLastMS != 390000 {
		t.Fatal("request checkpoints are not relative to the producer clock")
	}
}

func TestHTTPFixtureTrafficSwitchKeepsCadenceAndDoesNotCatchUp(t *testing.T) {
	start := time.Unix(1000, 0)
	traffic := newHTTPFixtureTraffic(start)
	traffic.take(start, false)
	if _, ok := traffic.take(start.Add(time.Millisecond), true); ok {
		t.Fatal("phase switch bypassed previous request interval")
	}
	if _, ok := traffic.take(start.Add(200*time.Second), false); !ok {
		t.Fatal("healthy phase was not latched")
	}
	if _, ok := traffic.take(start.Add(200*time.Second), true); ok || traffic.HealthyAttempts != 1 {
		t.Fatal("missed slots were replayed")
	}
}
