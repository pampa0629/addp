package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/addp/monitor/internal/frontendprobe"
	"github.com/addp/monitor/internal/sourceserver"
)

var errConfiguration = errors.New("frontend source configuration is invalid")

type configuration struct {
	BusinessNFS  bool   `json:"business_nfs,omitempty"`
	Mode         string `json:"mode"`
	PublicOrigin string `json:"public_origin"`
	Workspace    string `json:"workspace"`
	DockerSocket string `json:"docker_socket"`
}

type frontend struct {
	module  string
	entry   string
	service string
	port    int
}

type resource struct {
	cpuSeconds, memoryBytes float64
	generation              string
	restarts                *uint64
}

func (c configuration) validate() (*url.URL, error) {
	origin, err := url.Parse(c.PublicOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Hostname() == "" ||
		origin.Path != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.User != nil ||
		!filepath.IsAbs(c.Workspace) || filepath.Clean(c.Workspace) != c.Workspace {
		return nil, errConfiguration
	}
	switch c.Mode {
	case "vite":
		if c.DockerSocket != "" {
			return nil, errConfiguration
		}
	case "compose":
		if c.BusinessNFS {
			return nil, errConfiguration
		}
		socket, err := os.Stat(c.DockerSocket)
		if !filepath.IsAbs(c.DockerSocket) ||
			err != nil || socket.Mode()&os.ModeSocket == 0 {
			return nil, errConfiguration
		}
	default:
		return nil, errConfiguration
	}
	return origin, nil
}

type command func(context.Context, string, ...string) (string, error)

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	data, err := cmd.Output()
	if len(data) > 64<<10 {
		return "", errConfiguration
	}
	return strings.TrimSpace(string(data)), err
}

func positivePID(raw string) (int, bool) {
	pid, err := strconv.Atoi(raw)
	return pid, err == nil && pid > 1 && strconv.Itoa(pid) == raw
}

// Take node-wide snapshots so adding a module does not add another set of lsof/ps processes.
func nativeResources(ctx context.Context, run command, workspace string, frontends []frontend) map[string]*resource {
	result := make(map[string]*resource)
	if len(frontends) == 0 {
		return result
	}
	ports := make([]string, 0, len(frontends))
	for _, f := range frontends {
		ports = append(ports, strconv.Itoa(f.port))
	}
	listeners := func() map[string]string {
		raw, err := run(ctx, "lsof", "-nP", "-iTCP:"+strings.Join(ports, ","), "-sTCP:LISTEN", "-Fpn")
		byPort := make(map[string]string)
		if err != nil {
			return byPort
		}
		var pid string
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(line, "p") {
				pid = line[1:]
			}
			if !strings.HasPrefix(line, "n") {
				continue
			}
			_, port, err := net.SplitHostPort(line[1:])
			if err != nil {
				continue
			}
			if old, ok := byPort[port]; ok && old != pid {
				byPort[port] = ""
			} else {
				byPort[port] = pid
			}
		}
		return byPort
	}
	before := listeners()
	var ids []string
	for _, pid := range before {
		if _, ok := positivePID(pid); ok {
			ids = append(ids, pid)
		}
	}
	if len(ids) == 0 {
		return result
	}
	idList := strings.Join(ids, ",")
	raw, err := run(ctx, "lsof", "-a", "-p", idList, "-d", "cwd", "-Fn")
	if err != nil {
		return result
	}
	cwd := map[string]string{}
	var pid string
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "p") {
			pid = line[1:]
		}
		if strings.HasPrefix(line, "n") {
			cwd[pid] = line[1:]
		}
	}
	raw, err = run(ctx, "ps", "-A", "-o", "pid=,ppid=")
	if err != nil {
		return result
	}
	parents := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 {
			parents[fields[0]] = fields[1]
		}
	}
	raw, err = run(ctx, "ps", "-p", idList, "-o", "pid=,ucomm=,time=,rss=,lstart=")
	if err != nil {
		return result
	}
	stats := map[string]*resource{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 9 || fields[1] != "node" {
			continue
		}
		cpu, cpuOK := processCPUSeconds(fields[2])
		rss, rssOK := number(fields[3])
		if cpuOK && rssOK && rss > 0 {
			stats[fields[0]] = &resource{cpuSeconds: cpu, memoryBytes: rss * 1024, generation: fields[0] + " " + strings.Join(fields[4:], " ")}
		}
	}
	after := listeners()
	for _, f := range frontends {
		port := strconv.Itoa(f.port)
		pid := before[port]
		if stats[pid] == nil || after[port] != pid || cwd[pid] != filepath.Join(workspace, f.module, "frontend") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(workspace, ".dev-pids", f.module+"-frontend.pid"))
		owner := strings.TrimSpace(string(raw))
		if _, ok := positivePID(owner); err != nil || !ok {
			continue
		}
		ancestor := pid
		for depth := 0; ancestor != "" && ancestor != owner && depth < 8; depth++ {
			ancestor = parents[ancestor]
		}
		if ancestor == owner {
			result[f.module] = stats[pid]
		}
	}
	return result
}

var containerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Darwin ps reports accumulated user and system time as minutes:seconds.hundredths.
func processCPUSeconds(raw string) (float64, bool) {
	minutes, seconds, ok := strings.Cut(raw, ":")
	m, err := strconv.ParseUint(minutes, 10, 64)
	sec, valid := number(seconds)
	return float64(m)*60 + sec, ok && err == nil && valid && sec < 60
}

func number(raw string) (float64, bool) {
	value, err := strconv.ParseFloat(raw, 64)
	return value, err == nil && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func composeResource(ctx context.Context, run command, cfg configuration, f frontend) *resource {
	docker := func(args ...string) (string, error) {
		return run(ctx, "docker", append([]string{"--host", "unix://" + cfg.DockerSocket}, args...)...)
	}
	const project = "addp-platform"
	id, err := docker("ps", "--no-trunc", "--filter", "label=com.docker.compose.project="+project,
		"--filter", "label=com.docker.compose.service="+f.service, "--format", "{{.ID}}")
	if err != nil || !containerID.MatchString(id) {
		return nil
	}
	type observedContainer struct {
		ID           string
		RestartCount uint64
		State        struct {
			Running   bool
			StartedAt time.Time
		}
		Config struct{ Labels map[string]string }
	}
	inspect := func() (observedContainer, bool) {
		var row observedContainer
		raw, err := docker("inspect", "--type", "container", "--format", `{"ID":{{json .ID}},"RestartCount":{{json .RestartCount}},"State":{{json .State}},"Config":{"Labels":{{json .Config.Labels}}}}`, id)
		ok := err == nil && json.Unmarshal([]byte(raw), &row) == nil && row.ID == id && row.State.Running &&
			!row.State.StartedAt.IsZero() && row.Config.Labels["com.docker.compose.project"] == project &&
			row.Config.Labels["com.docker.compose.service"] == f.service &&
			row.Config.Labels["com.docker.compose.project.working_dir"] == cfg.Workspace
		return row, ok
	}
	before, ok := inspect()
	if !ok {
		return nil
	}
	// The CLI percentage is a short-window rate. Read the cumulative counter from
	// the same Docker socket and the same verified container as its memory stats.
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.DockerSocket)
	}}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+id+"/stats?stream=false&one-shot=true", nil)
	if err != nil {
		return nil
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var stats struct {
		CPU struct {
			Usage struct {
				Total *uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
		} `json:"cpu_stats"`
		Memory struct {
			Usage uint64
			Stats map[string]uint64
		} `json:"memory_stats"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&stats) != nil ||
		stats.CPU.Usage.Total == nil || stats.Memory.Usage == 0 {
		return nil
	}
	after, ok := inspect()
	if !ok || before.State.StartedAt != after.State.StartedAt || before.RestartCount != after.RestartCount {
		return nil
	}
	usage := stats.Memory.Usage
	for _, key := range []string{"total_inactive_file", "inactive_file"} {
		if inactive, exists := stats.Memory.Stats[key]; exists && inactive < usage {
			usage -= inactive
			break
		}
	}
	return &resource{cpuSeconds: float64(*stats.CPU.Usage.Total) / 1e9, memoryBytes: float64(usage), restarts: &after.RestartCount,
		generation: id + "/" + after.State.StartedAt.UTC().Format(time.RFC3339Nano)}
}

func deployedFrontends(cfg configuration) ([]frontend, error) {
	if cfg.Mode == "compose" {
		return []frontend{{module: "console", entry: "/", service: "console"}, {module: "monitor", entry: "/module-ui/monitor/", service: "monitor-frontend"}}, nil
	}
	raw, err := os.ReadFile(filepath.Join(cfg.Workspace, ".dev-state", "ports.env"))
	if err != nil {
		return nil, err
	}
	ports := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			ports[key] = value
		}
	}
	var rows []frontend
	for _, module := range frontendprobe.Modules {
		_, err := os.Stat(filepath.Join(cfg.Workspace, ".dev-pids", module+"-frontend.pid"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		port, err := strconv.Atoi(ports[strings.ToUpper(module)+"_FE_PORT"])
		if err != nil || port < 1 || port > 65535 {
			return nil, errConfiguration
		}
		entry := "/module-ui/" + module + "/"
		switch module {
		case "console":
			entry = "/"
		case "portal":
			entry = "/portal/"
		}
		rows = append(rows, frontend{module: module, entry: entry, port: port})
	}
	return rows, nil
}

// Export visibility describes mountd's advertised directory, without opening any exported files.
func businessNFS(ctx context.Context, workspace string, run command) []byte {
	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	listing, err := run(probeCtx, "/usr/bin/showmount", "-e", "localhost")
	if err != nil {
		return nil
	}
	export := filepath.Join(workspace, "business", "nfs", "data")
	lines := strings.Split(listing, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Exports list on ") {
		return nil
	}
	available := 0
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == export || strings.HasPrefix(line, export+" ") || strings.HasPrefix(line, export+"\t") {
			available = 1
			break
		}
	}
	return fmt.Appendf(nil, "addp_business_nfs_export_available %d\n", available)
}

func collect(ctx context.Context, cfg configuration, probe *frontendprobe.Prober, run command) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	frontends, err := deployedFrontends(cfg)
	if err != nil {
		return nil, err
	}
	var native map[string]*resource
	if cfg.Mode == "vite" {
		native = nativeResources(ctx, run, cfg.Workspace, frontends)
	}
	metrics := make([][]byte, len(frontends))
	var work sync.WaitGroup
	for i, f := range frontends {
		work.Add(1)
		go func() {
			defer work.Done()
			health := probe.Check(ctx, f.entry)
			label := fmt.Sprintf(`{frontend_module=%q,frontend_mode=%q}`, f.module, cfg.Mode)
			metric := func(key string, value any) {
				metrics[i] = fmt.Appendf(metrics[i], "addp_frontend_%s%s %v\n", key, label, value)
			}
			binary := func(value bool) int {
				if value {
					return 1
				}
				return 0
			}
			metric("entry_up", binary(health.Entry))
			if health.Asset != nil {
				metric("asset_up", binary(*health.Asset))
			}
			data := native[f.module]
			if cfg.Mode == "compose" {
				data = composeResource(ctx, run, cfg, f)
			}
			if data != nil {
				metrics[i] = fmt.Appendf(metrics[i], "addp_frontend_cpu_seconds_total%s,frontend_generation=%q} %v\n", strings.TrimSuffix(label, "}"), data.generation, data.cpuSeconds)
				metric("memory_bytes", data.memoryBytes)
				if data.restarts != nil {
					metric("restart_count", *data.restarts)
				}
			}
		}()
	}
	work.Wait()
	if cfg.BusinessNFS && runtime.GOOS == "darwin" {
		metrics = append(metrics, businessNFS(ctx, cfg.Workspace, run))
	}
	return bytes.Join(metrics, nil), nil
}

func run() error {
	file := flag.String("config", "", "deployment configuration JSON")
	tlsDir := flag.String("tls-dir", "", "source TLS directory")
	listen := flag.String("listen", "", "private numeric bind IP and port")
	check := flag.Bool("check", false, "read one local observation without opening a listener")
	flag.Parse()
	if !filepath.IsAbs(*file) || (!*check && !filepath.IsAbs(*tlsDir)) {
		return errConfiguration
	}
	f, err := os.Open(*file)
	if err != nil {
		return errConfiguration
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 4096))
	d.DisallowUnknownFields()
	var cfg configuration
	if d.Decode(&cfg) != nil {
		return errConfiguration
	}
	if _, err := d.Token(); err != io.EOF {
		return errConfiguration
	}
	origin, err := cfg.validate()
	if err != nil {
		return err
	}
	probe := frontendprobe.New(origin)
	if *check {
		body, err := collect(context.Background(), cfg, probe, runCommand)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(body)
		return err
	}
	bind, err := netip.ParseAddrPort(*listen)
	if err != nil || bind.Port() == 0 || (!bind.Addr().IsLoopback() && !bind.Addr().IsPrivate()) || bind.Addr().Zone() != "" {
		return errConfiguration
	}
	return sourceserver.ServeAt(sourceserver.Handler(func(ctx context.Context) ([]byte, error) {
		return collect(ctx, cfg, probe, runCommand)
	}), *tlsDir, bind.String())
}

func main() {
	if run() != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Frontend observation source unavailable")
		os.Exit(1)
	}
}
