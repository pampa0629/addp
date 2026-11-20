package bootstrap_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// The gate passes a real module binary and only its process-scoped credentials.
func TestMySQLRuntimeStartupRefusal(t *testing.T) {
	binary := os.Getenv("ADDP_TEST_STARTUP_BINARY")
	if binary == "" {
		t.Skip("requires a built System or Meta Worker binary")
	}
	want := os.Getenv("ADDP_TEST_STARTUP_REASON")
	if want == "" {
		t.Fatal("missing expected startup failure")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PROJECT_ROOT=" + cmd.Dir, "ADDP_INFRA_DATABASE_PROVIDER=mysql", "TZ=Asia/Shanghai",
		"ADDP_MYSQL_HOST=127.0.0.1", "ADDP_MYSQL_PORT=" + os.Getenv("ADDP_TEST_MYSQL_PORT")}
	var prefix string
	switch filepath.Base(binary) {
	case "system-server":
		prefix = "ADDP_MYSQL_SYSTEM"
		redis := miniredis.RunT(t)
		redis.RequireAuth("fixture")
		cmd.Env = append(cmd.Env, "SYSTEM_BACKEND_PORT=0", "REDIS_HOST="+redis.Host(), "REDIS_PORT="+redis.Port(), "REDIS_PASSWORD=fixture")
	case "meta-worker":
		prefix = "ADDP_MYSQL_META_WORKER"
	default:
		t.Fatal("unexpected runtime binary name")
	}
	password := os.Getenv(prefix + "_PASSWORD")
	cmd.Env = append(cmd.Env, prefix+"_USER="+os.Getenv(prefix+"_USER"), prefix+"_PASSWORD="+password)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), want) {
		diagnostic := string(output)
		if password != "" {
			diagnostic = strings.ReplaceAll(diagnostic, password, "[redacted]")
		}
		t.Fatalf("expected exit 1 with %q, got %v: %s", want, err, diagnostic)
	}
	t.Logf("%s exited 1: %s", filepath.Base(binary), want)
}
