// runtime-log installs output pipes and execs the business process without
// changing its PID. A separate receiver owns bounded storage and exits at EOF.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/addp/common/runtimelog"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: runtime-log launch|capture --module name --role role -- command")
	}
	mode := os.Args[1]
	if mode == "probe" {
		return probe()
	}
	f := flag.NewFlagSet(mode, flag.ContinueOnError)
	module := f.String("module", "", "module")
	role := f.String("role", "backend", "role")
	if err := f.Parse(os.Args[2:]); err != nil {
		return err
	}
	id := os.Getenv("ADDP_PROCESS_INSTANCE_ID")
	if mode == "launch" {
		id = uuid.NewString()
		if err := os.Setenv("ADDP_PROCESS_INSTANCE_ID", id); err != nil {
			return err
		}
	}
	if mode == "prune" {
		id = "housekeeper"
		*module = "housekeeper"
		*role = "backend"
	}
	o, err := runtimelog.FromEnvironment(*module, *role, id)
	if err != nil {
		return err
	}
	if mode == "prune" {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
		defer cancel()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			_ = runtimelog.Prune(o)
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
	if mode == "capture" {
		out := os.NewFile(3, "stdout-pipe")
		stderr := os.NewFile(4, "stderr-pipe")
		defer out.Close()
		defer stderr.Close()
		return runtimelog.Capture(o, out, stderr)
	}
	if mode != "launch" || len(f.Args()) == 0 {
		return fmt.Errorf("invalid launch command")
	}
	program, err := exec.LookPath(f.Args()[0])
	if err != nil {
		return err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		return err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	capture := exec.Command(self, "capture", "--module", *module, "--role", *role)
	capture.ExtraFiles = []*os.File{outR, errR}
	capture.Stderr = os.Stderr
	capture.Env = os.Environ()
	if err = capture.Start(); err != nil {
		return err
	}
	outR.Close()
	errR.Close()
	if err = unix.Dup2(int(outW.Fd()), 1); err != nil {
		return err
	}
	if err = unix.Dup2(int(errW.Fd()), 2); err != nil {
		return err
	}
	outW.Close()
	errW.Close()
	// A dead receiver must not terminate the business process through the
	// default SIGPIPE action. Writers may still receive EPIPE; logging is best effort.
	signal.Ignore(syscall.SIGPIPE)
	return syscall.Exec(program, f.Args(), os.Environ())
}

func probe() error {
	id := uuid.NewString()
	o, err := runtimelog.FromEnvironment("runtime-probe", "backend", id)
	if err != nil {
		return err
	}
	started := time.Now()
	if err = runtimelog.Capture(o, strings.NewReader("runtime-log-delivery-probe\n"), strings.NewReader("")); err != nil {
		return err
	}
	endpoint := os.Getenv("LOKI_URL")
	target, err := url.Parse(endpoint)
	if err != nil || target.Host == "" {
		return fmt.Errorf("runtime log probe endpoint unavailable")
	}
	target.Path = "/loki/api/v1/query_range"
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for time.Since(started) < 20*time.Second {
		target.RawQuery = url.Values{"query": {`{deployment="addp",module_name="runtime-probe"} | instance_id=` + strconv.Quote(id)}, "start": {strconv.FormatInt(started.Add(-time.Second).UnixNano(), 10)}, "end": {strconv.FormatInt(time.Now().UnixNano(), 10)}, "limit": {"10"}}.Encode()
		req, _ := http.NewRequest(http.MethodGet, target.String(), nil)
		req.Header.Set("Authorization", "Bearer "+os.Getenv("LOKI_READ_TOKEN"))
		res, e := client.Do(req)
		if e == nil {
			body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
			res.Body.Close()
			if res.StatusCode == 200 && strings.Contains(string(body), id) {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{"observed_at": time.Now().UTC(), "probe_delivered": true, "delay_ms": time.Since(started).Milliseconds()})
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("runtime log delivery probe failed; collection completeness unknown")
}
