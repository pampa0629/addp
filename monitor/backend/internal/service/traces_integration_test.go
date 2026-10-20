package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/logger"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/common/runtimelog"
	"github.com/addp/monitor/internal/tracequery"
	"github.com/gin-gonic/gin"
)

// Only the standard local gate launches these test-owned native Go processes.
func TestOwnedTraceProcess(t *testing.T) {
	if os.Getenv("TRACE_T2_CHILD") != "true" {
		t.Skip("owned process fixture")
	}
	module, role := os.Getenv("TRACE_T2_MODULE"), os.Getenv("TRACE_T2_ROLE")
	stop := httptrace.StartOptional(module, role)
	defer stop()
	router := modulelifecycle.NewRouter()
	router.GET("/health/ready", func(c *gin.Context) { c.JSON(200, httptrace.Snapshot()) })
	router.GET("/items/:id", func(c *gin.Context) {
		logger.L().InfoContext(c.Request.Context(), "trace fixture request")
		target := os.Getenv("TRACE_T2_NEXT")
		if target != "" {
			req, _ := http.NewRequestWithContext(c.Request.Context(), "GET", target+c.Request.URL.RequestURI(), nil)
			res, e := httptrace.Do(&http.Client{Timeout: 5 * time.Second}, req, target)
			if e != nil {
				c.Status(502)
				return
			}
			defer res.Body.Close()
			c.Status(res.StatusCode)
			_, _ = io.Copy(c.Writer, res.Body)
			return
		}
		if c.Param("id") == "slow" {
			time.Sleep(1100 * time.Millisecond)
		}
		status := 200
		if c.Param("id") == "failure" {
			status = 503
		}
		traceID, spanID := httptrace.IDs(c.Request.Context())
		c.JSON(status, gin.H{"trace_id": traceID, "span_id": spanID})
	})
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: time.Second}
	if e = os.WriteFile(os.Getenv("TRACE_T2_ADDRESS"), []byte("http://"+listener.Addr().String()), 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(done)
	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	if e = server.Serve(listener); e != nil && e != http.ErrServerClosed {
		t.Fatal(e)
	}
}

type ownedTraceUsers struct {
	rows []client.RuntimeInstanceReference
	deny bool
}

func (u *ownedTraceUsers) GetRuntimeInstancesForUser(_ context.Context, ids []uint, token string) ([]client.RuntimeInstanceReference, error) {
	if token != "addp_at_t2_current_user" || u.deny {
		return nil, tracequery.ErrDenied
	}
	for _, r := range u.rows {
		if r.ID == ids[0] {
			return []client.RuntimeInstanceReference{r}, nil
		}
	}
	return nil, nil
}
func (u *ownedTraceUsers) GetProcessInstancesForUser(_ context.Context, ids []string, token string) ([]client.RuntimeInstanceReference, error) {
	if token != "addp_at_t2_current_user" || u.deny {
		return nil, tracequery.ErrDenied
	}
	return u.rows, nil
}
func TestOwnedTracePipeline(t *testing.T) {
	w := os.Getenv("TRACE_T2_WORK")
	if w == "" {
		t.Skip("standard Trace T2 gate only")
	}
	users := &ownedTraceUsers{}
	var captures sync.WaitGroup
	t.Cleanup(func() { captures.Wait() })
	start := func(module, role, next string, id uint) string {
		return startOwnedTrace(t, w, users, &captures, module, role, next, id)
	}
	leaf := start("system", "backend", "", 3)
	middle := start("monitor", "backend", leaf, 2)
	gateway := start("gateway", "ingress", middle, 1)
	request := func(path string) string {
		req, _ := http.NewRequest("GET", gateway+path+"?payload=never-observe-this", nil)
		req.Header.Set("traceparent", "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")
		req.Header.Set("baggage", "payload=never-observe-this")
		req.Header.Set("tracestate", "external=never-observe-this")
		res, e := (&http.Client{Timeout: 6 * time.Second}).Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var body struct {
			ID string `json:"trace_id"`
		}
		if e = json.NewDecoder(res.Body).Decode(&body); e != nil {
			t.Fatal(e)
		}
		if !httptrace.ParseTraceID(body.ID) || body.ID == strings.Repeat("a", 32) {
			t.Fatal("external trace was not restarted")
		}
		return body.ID
	}
	from := time.Now().UTC().Add(-time.Second)
	failure := request("/items/failure")
	slow := request("/items/slow")
	normal := map[string]bool{}
	for i := 0; i < 200; i++ {
		normal[request(fmt.Sprintf("/items/%d", i))] = true
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(3 * time.Second)
	for _, address := range []string{leaf, middle, gateway} {
		res, err := http.Get(address + "/health/ready")
		if err != nil {
			t.Fatal(err)
		}
		var stats httptrace.Counters
		err = json.NewDecoder(res.Body).Decode(&stats)
		res.Body.Close()
		t.Logf("producer export counters: %+v", stats)
		if err != nil || stats.Exported == 0 || stats.ExportFailed != 0 {
			t.Fatalf("producer export failed: %+v %v", stats, err)
		}
	}
	until := time.Now().UTC()
	queryClient, e := httptrace.TLSHTTPClient(filepath.Join(w, "query-tls"), 5*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	backend, e := tracequery.NewClient(os.Getenv("TRACE_T2_QUERY"), queryClient)
	if e != nil {
		t.Fatal(e)
	}
	service := NewTraceService(users, backend)
	var retained TraceResponse
	// Keep the published 120-second decision wait. Never accelerate it for T2.
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		retained, e = service.Query(context.Background(), "t2-user", "addp_at_t2_current_user", TraceQuery{InstanceID: 1, Start: from, End: until, Limit: 100})
		hasFailure, hasSlow, ordinary := false, false, 0
		if e == nil {
			for _, d := range retained.Data {
				hasFailure = hasFailure || d.ID == failure
				hasSlow = hasSlow || d.ID == slow
				if normal[d.ID] {
					ordinary++
				}
			}
		}
		if hasFailure && hasSlow && ordinary > 0 && time.Now().After(until.Add(150*time.Second)) {
			if ordinary >= 50 {
				t.Fatal("normal traces were not sampled")
			}
			t.Logf("retained failure, slow and %d/200 normal traces with actual 120s tail wait", ordinary)
			break
		}
		time.Sleep(3 * time.Second)
	}
	if e != nil || len(retained.Data) == 0 {
		t.Fatalf("real Tempo query failed: %v", e)
	}
	foundFailure, foundSlow, ordinary := false, false, 0
	for _, d := range retained.Data {
		foundFailure = foundFailure || d.ID == failure
		foundSlow = foundSlow || d.ID == slow
		if normal[d.ID] {
			ordinary++
		}
		if len(d.Spans) != 5 || len(d.Edges) != 2 || d.Partial {
			t.Fatalf("actual spans/edges: %+v", d)
		}
		encoded, _ := json.Marshal(d)
		if strings.Contains(string(encoded), "never-observe-this") {
			t.Fatal("payload escaped safe projection")
		}
	}
	if !foundFailure || !foundSlow || ordinary == 0 || ordinary >= 50 {
		t.Fatalf("sampling outcome failure=%v slow=%v normal=%d last=%v", foundFailure, foundSlow, ordinary, e)
	}
	users.deny = true
	blocked, e := service.Query(context.Background(), "t2-user", "addp_at_t2_current_user", TraceQuery{InstanceID: 1, ID: failure, Start: from, End: until, Limit: 1})
	if e == nil || len(blocked.Data) > 0 {
		t.Fatal("revoked current User retained data")
	}
	users.deny = false
	// Query source credentials cannot access the separately authenticated read API.
	wrong, e := httptrace.TLSHTTPClient(filepath.Join(w, "source-tls"), 2*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	response, e := wrong.Get(os.Getenv("TRACE_T2_QUERY") + "/api/search")
	if e == nil {
		response.Body.Close()
		if response.StatusCode == 200 {
			t.Fatal("source credential authorized query")
		}
	}
	// The separately issued query client cannot write OTLP; read transport
	// never forwards arbitrary methods or administrative paths to Tempo.
	reqWrite, _ := http.NewRequest("POST", os.Getenv("TRACE_T2_OTLP"), nil)
	reqWrite.Header.Set("Content-Type", "application/x-protobuf")
	denied, writeErr := queryClient.Do(reqWrite)
	if writeErr == nil {
		denied.Body.Close()
		if denied.StatusCode == 200 {
			t.Fatal("query credential authorized OTLP write")
		}
	}
	for _, item := range []struct {
		method, path string
		status       int
	}{{"POST", "/api/search", 405}, {"GET", "/api/admin", 404}} {
		req, _ := http.NewRequest(item.method, os.Getenv("TRACE_T2_QUERY")+item.path, nil)
		res, err := queryClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != item.status {
			t.Fatal("query proxy exceeded its read-only boundary", res.StatusCode)
		}
	}
	// Exact Trace metadata must survive the real source receiver, Alloy and Loki.
	logURL, e := url.Parse(os.Getenv("TRACE_T2_LOKI") + "/loki/api/v1/query_range")
	if e != nil {
		t.Fatal(e)
	}
	q := url.Values{"query": {`{module_name="system"} | trace_id="` + failure + `"`}, "start": {fmt.Sprint(from.UnixNano())}, "end": {fmt.Sprint(time.Now().UnixNano())}, "limit": {"10"}}
	logURL.RawQuery = q.Encode()
	req, _ := http.NewRequest("GET", logURL.String(), nil)
	req.Header.Set("Authorization", "Bearer "+os.Getenv("LOKI_READ_TOKEN"))
	response, e = (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != 200 || !strings.Contains(string(body), failure) || !strings.Contains(string(body), "span_id") {
		t.Fatalf("real Loki Trace metadata missing: %d %s", response.StatusCode, body)
	}
	compose := func(args ...string) {
		command := append([]string{"compose", "--env-file", "/dev/null", "-p", os.Getenv("TRACE_T2_PROJECT"), "-f", os.Getenv("TRACE_T2_COMPOSE")}, args...)
		if output, err := exec.Command("docker", command...).CombinedOutput(); err != nil {
			t.Fatalf("owned collector lifecycle failed: %v %s", err, output)
		}
	}
	compose("stop", "alloy")
	for i := 0; i < 20; i++ {
		request("/items/outage")
		time.Sleep(200 * time.Millisecond)
	}
	outageDeadline := time.Now().Add(8 * time.Second)
	failedExports := false
	for time.Now().Before(outageDeadline) {
		res, err := (&http.Client{Timeout: time.Second}).Get(gateway + "/health/ready")
		if err != nil {
			t.Fatal("business readiness depended on collector", err)
		}
		var stats httptrace.Counters
		err = json.NewDecoder(res.Body).Decode(&stats)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if stats.ExportFailed > 0 {
			failedExports = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !failedExports {
		t.Fatal("collector outage had no bounded source loss counter")
	}
	compose("up", "-d", "--no-deps", "alloy")
	sourceProbe, err := httptrace.TLSHTTPClient(filepath.Join(w, "source-tls"), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	collectorReady := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		req, _ := http.NewRequest("POST", os.Getenv("TRACE_T2_OTLP"), nil)
		req.Header.Set("Content-Type", "application/x-protobuf")
		res, e := sourceProbe.Do(req)
		if e == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				collectorReady = true
				break
			}
		}
	}
	if !collectorReady {
		t.Fatal("owned collector endpoint changed or did not accept authenticated OTLP after restart")
	}
	time.Sleep(3 * time.Second)
	recoveredFrom := time.Now().Add(-time.Second)
	recoveredID := request("/items/failure")
	recoveredTo := time.Now().UTC()
	recovered := false
	for deadline := time.Now().Add(3 * time.Minute); time.Now().Before(deadline); time.Sleep(3 * time.Second) {
		result, err := service.Query(context.Background(), "t2-user", "addp_at_t2_current_user", TraceQuery{InstanceID: 1, ID: recoveredID, Start: recoveredFrom, End: recoveredTo, Limit: 1})
		if err == nil && len(result.Data) == 1 && len(result.Data[0].Spans) == 5 && len(result.Data[0].Edges) == 2 {
			recovered = true
			break
		}
	}
	if !recovered {
		t.Fatal("new trace did not recover after collector restart and real 120s tail wait")
	}
	t.Log("real receiver → shared Alloy → Loki Trace metadata, safe Tempo projection, historical participants, current User revocation and collector outage/recovery passed")
}

func startOwnedTrace(t *testing.T, w string, users *ownedTraceUsers, captures *sync.WaitGroup, module, role, next string, id uint) string {
	instance := "t2-" + module
	address := filepath.Join(w, module+".address")
	deployment, _ := json.Marshal([]httptrace.Deployment{{Module: module, Role: role, Endpoint: os.Getenv("TRACE_T2_OTLP"), TLSDir: filepath.Join(w, "source-tls")}})
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(exe, "-test.run=^TestOwnedTraceProcess$", "-test.timeout=38m")
	cmd.Env = append(os.Environ(), "TRACE_T2_CHILD=true", "ADDP_OBSERVABILITY_TRACES_ENABLED=true", "TRACE_T2_MODULE="+module, "TRACE_T2_ROLE="+role, "TRACE_T2_ADDRESS="+address, "TRACE_T2_NEXT="+next, "ADDP_TRACE_DEPLOYMENTS="+string(deployment), "ADDP_PROCESS_INSTANCE_ID="+instance)
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	errs, e := cmd.StderrPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	options := runtimelog.Options{Root: filepath.Join(w, "source"), Module: module, Role: role, InstanceID: instance, Node: "trace-t2", SegmentBytes: 1 << 20, InstanceBytes: 4 << 20, NodeBytes: 16 << 20, SourceAge: time.Hour}
	captures.Add(1)
	go func() {
		defer captures.Done()
		if e := runtimelog.Capture(options, out, errs); e != nil {
			t.Errorf("capture: %v", e)
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case e := <-done:
			if e != nil {
				t.Errorf("child %s: %v", module, e)
			}
		case <-time.After(6 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Errorf("child shutdown timeout")
		}
	})
	users.rows = append(users.rows, client.RuntimeInstanceReference{ID: id, ModuleName: module, Role: role, InstanceID: instance, Status: "down"})
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		b, e := os.ReadFile(address)
		if e == nil {
			return string(b)
		}
	}
	t.Fatal("fixture did not start")
	return ""
}
