package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/addp/monitor/internal/frontendprobe"
	"github.com/addp/monitor/internal/sourceserver"
)

var errConfiguration = errors.New("frontend source configuration is invalid")

type configuration struct {
	Mode         string `json:"mode"`
	PublicOrigin string `json:"public_origin"`
	Workspace    string `json:"workspace"`
	ConsolePort  int    `json:"console_port"`
	MonitorPort  int    `json:"monitor_port"`
	DockerSocket string `json:"docker_socket"`
}

type frontend struct {
	module  string
	entry   string
	service string
	port    int
}

type resource struct {
	cores, memoryBytes float64
	restarts           *uint64
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
		if c.DockerSocket != "" || c.ConsolePort < 1 || c.ConsolePort > 65535 ||
			c.MonitorPort < 1 || c.MonitorPort > 65535 || c.ConsolePort == c.MonitorPort {
			return nil, errConfiguration
		}
	case "compose":
		socket, err := os.Stat(c.DockerSocket)
		if !filepath.IsAbs(c.DockerSocket) || c.ConsolePort != 0 || c.MonitorPort != 0 ||
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

func nativeResource(ctx context.Context, run command, workspace string, f frontend) *resource {
	raw, err := os.ReadFile(filepath.Join(workspace, ".dev-pids", f.module+"-frontend.pid"))
	parent, valid := positivePID(strings.TrimSpace(string(raw)))
	if err != nil || !valid {
		return nil
	}
	listener, err := run(ctx, "lsof", "-t", "-iTCP:"+strconv.Itoa(f.port), "-sTCP:LISTEN")
	pid, valid := positivePID(listener)
	if err != nil || !valid {
		return nil
	}
	cwd, err := run(ctx, "lsof", "-a", "-p", listener, "-d", "cwd", "-Fn")
	if err != nil || cwd != "p"+listener+"\nfcwd\nn"+filepath.Join(workspace, f.module, "frontend") {
		return nil
	}
	fields, err := run(ctx, "ps", "-p", listener, "-o", "ucomm=,pcpu=,rss=")
	data := strings.Fields(fields)
	if err != nil || len(data) != 3 || data[0] != "node" {
		return nil
	}
	// The standard launcher records the wrapper PID. A listener owned by a
	// different workspace or a different launcher does not count.
	for depth := 0; pid != parent && depth < 8; depth++ {
		next, err := run(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "ppid=")
		pid, valid = positivePID(next)
		if err != nil || !valid {
			return nil
		}
	}
	stillListening, err := run(ctx, "lsof", "-t", "-iTCP:"+strconv.Itoa(f.port), "-sTCP:LISTEN")
	cpu, cpuOK := number(data[1])
	rss, rssOK := number(data[2])
	if pid != parent || err != nil || stillListening != listener || !cpuOK || !rssOK || rss <= 0 {
		return nil
	}
	return &resource{cores: cpu / 100, memoryBytes: rss * 1024}
}

var containerID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var memoryUsage = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*(B|KiB|MiB|GiB|TiB)$`)

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
	raw, err := docker("stats", "--no-stream", "--no-trunc", "--format", "{{json .}}", id)
	var stats struct{ CPUPerc, MemUsage string }
	if err != nil || json.Unmarshal([]byte(raw), &stats) != nil || !strings.HasSuffix(stats.CPUPerc, "%") {
		return nil
	}
	after, ok := inspect()
	if !ok || before.State.StartedAt != after.State.StartedAt || before.RestartCount != after.RestartCount {
		return nil
	}
	cpu, valid := number(strings.TrimSuffix(stats.CPUPerc, "%"))
	usage, _, _ := strings.Cut(stats.MemUsage, "/")
	match := memoryUsage.FindStringSubmatch(strings.TrimSpace(usage))
	if !valid || len(match) != 3 {
		return nil
	}
	memory, valid := number(match[1])
	if !valid || memory <= 0 {
		return nil
	}
	scales := map[string]float64{"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40}
	return &resource{cores: cpu / 100, memoryBytes: memory * scales[match[2]], restarts: &after.RestartCount}
}

func collect(ctx context.Context, cfg configuration, probe *frontendprobe.Prober, run command) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	frontends := []frontend{
		{module: "console", entry: "/", service: "console", port: cfg.ConsolePort},
		{module: "monitor", entry: "/module-ui/monitor/", service: "monitor-frontend", port: cfg.MonitorPort},
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
			var data *resource
			if cfg.Mode == "vite" {
				data = nativeResource(ctx, run, cfg.Workspace, f)
			} else {
				data = composeResource(ctx, run, cfg, f)
			}
			if data != nil {
				metric("cpu_cores", data.cores)
				metric("memory_bytes", data.memoryBytes)
				if data.restarts != nil {
					metric("restart_count", *data.restarts)
				}
			}
		}()
	}
	work.Wait()
	return append(metrics[0], metrics[1]...), nil
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
