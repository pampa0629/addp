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
	"os"
	"os/exec"
	"os/signal"
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
	if mode == "observe" {
		return observe()
	}
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
	o, err := runtimelog.FromEnvironment("runtime-probe", "backend", uuid.NewString())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	delay, err := runtimelog.Probe(ctx, o, os.Getenv("LOKI_URL"), os.Getenv("LOKI_READ_TOKEN"))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"observed_at": time.Now().UTC(), "probe_delivered": true, "delay_ms": delay.Milliseconds()})
}
