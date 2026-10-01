package main

import (
	"encoding/json"
	"fmt"
	_ "github.com/addp/common/logger"
	"github.com/addp/common/runtimelog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "business-output-probe" {
		fmt.Fprintln(os.Stdout, "ready")
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(os.Getenv("ADDP_TEST_RELEASE_PATH")); err == nil {
				fmt.Fprintln(os.Stdout, "receiver already gone")
				fmt.Fprintln(os.Stderr, "receiver already gone")
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(9)
	}

	if os.Getenv("ADDP_RUNTIME_LOG_TEST_CHILD") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestLaunchPreservesPIDExitAndCapturesBeforeRegistration(t *testing.T) {
	root := t.TempDir()
	self, _ := os.Executable()
	cmd := exec.Command(self, "launch", "--module", "manager", "--role", "backend", "--", "sh", "-c", `echo "$ADDP_PROCESS_INSTANCE_ID $$"; echo "startup failed" >&2; exit 17`)
	cmd.Env = append(os.Environ(), "ADDP_RUNTIME_LOG_TEST_CHILD=1", "ADDP_RUNTIME_LOG_ROOT="+root)
	e := cmd.Start()
	if e != nil {
		t.Fatal(e)
	}
	pid := cmd.Process.Pid
	e = cmd.Wait()
	if exit, ok := e.(*exec.ExitError); !ok || exit.ExitCode() != 17 {
		t.Fatalf("exit: %v", e)
	}
	var lines []runtimelog.Entry
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		files, _ := filepath.Glob(filepath.Join(root, "manager", "*", "*.jsonl"))
		lines = nil
		for _, file := range files {
			body, _ := os.ReadFile(file)
			for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
				var entry runtimelog.Entry
				if json.Unmarshal([]byte(line), &entry) == nil {
					lines = append(lines, entry)
				}
			}
		}
		if len(lines) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(lines) != 2 {
		t.Fatalf("startup entries: %d", len(lines))
	}
	for _, line := range lines {
		if line.Channel == "stdout" && line.Message != fmt.Sprintf("%s %s", line.InstanceID, strconv.Itoa(pid)) {
			t.Fatal("business PID or identity changed")
		}
	}
}

func TestLaunchSignalReachesBusinessAndReceiverClosesAfterEOF(t *testing.T) {
	root := t.TempDir()
	self, _ := os.Executable()
	cmd := exec.Command(self, "launch", "--module", "manager", "--role", "backend", "--", "sh", "-c", `trap 'echo terminated; exit 23' TERM; echo ready; while :; do sleep 0.1; done`)
	cmd.Env = append(os.Environ(), "ADDP_RUNTIME_LOG_TEST_CHILD=1", "ADDP_RUNTIME_LOG_ROOT="+root)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	var files []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		files, _ = filepath.Glob(filepath.Join(root, "manager", "*", "*.jsonl"))
		if len(files) > 0 {
			body, _ := os.ReadFile(files[0])
			if strings.Contains(string(body), "ready") {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(files) == 0 {
		t.Fatal("receiver did not capture readiness")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 23 {
			t.Fatalf("signal exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("business ignored termination")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		file, err := os.OpenFile(files[0], os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		locked := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
		body, _ := os.ReadFile(files[0])
		file.Close()
		if locked && strings.Contains(string(body), "terminated") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("receiver retained its segment lock after business EOF")
}

func TestReceiverFailureDoesNotKillBusinessThroughSIGPIPE(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "release")
	self, _ := os.Executable()
	cmd := exec.Command(self, "launch", "--module", "manager", "--role", "backend", "--", self, "business-output-probe")
	cmd.Env = append(os.Environ(), "ADDP_RUNTIME_LOG_TEST_CHILD=1", "ADDP_RUNTIME_LOG_ROOT="+root, "ADDP_TEST_RELEASE_PATH="+release)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	receiver := 0
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command("ps", "-axo", "pid=,ppid=,args=").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 2 && fields[1] == strconv.Itoa(cmd.Process.Pid) && strings.Contains(line, " capture --module manager") {
				receiver, _ = strconv.Atoi(fields[0])
			}
		}
		files, _ := filepath.Glob(filepath.Join(root, "manager", "*", "*.jsonl"))
		if receiver != 0 && len(files) > 0 {
			body, _ := os.ReadFile(files[0])
			if strings.Contains(string(body), "ready") {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if receiver == 0 {
		t.Fatal("receiver child not found")
	}
	if err := syscall.Kill(receiver, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	// Wait until its segment lock is released, so the next writes really see EPIPE.
	files, _ := filepath.Glob(filepath.Join(root, "manager", "*", "*.jsonl"))
	if len(files) == 0 {
		t.Fatal("readiness source missing")
	}
	deadline = time.Now().Add(5 * time.Second)
	unlocked := false
	for time.Now().Before(deadline) {
		file, err := os.OpenFile(files[0], os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		unlocked = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
		file.Close()
		if unlocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !unlocked {
		t.Fatal("receiver did not terminate")
	}
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("receiver fault killed business: %v", err)
	}
}
