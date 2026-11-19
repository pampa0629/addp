package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeFrontendRequiresListenerOwnedByLauncher(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".dev-pids"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dev-pids", "console-frontend.pid"), []byte("90\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := "90"
	run := func(_ context.Context, name string, args ...string) (string, error) {
		switch {
		case name == "lsof" && args[0] == "-t":
			return "91", nil
		case name == "lsof" && args[0] == "-a":
			return "p91\nfcwd\nn" + filepath.Join(root, "console", "frontend"), nil
		case name == "ps" && args[3] == "ppid=":
			return owner, nil
		case name == "ps":
			return "node 12.5 2048", nil
		default:
			t.Fatalf("unexpected %s %v", name, args)
			return "", nil
		}
	}
	got := nativeResource(context.Background(), run, root, frontend{module: "console", port: 5170})
	if got == nil || got.cores != .125 || got.memoryBytes != 2<<20 || got.restarts != nil {
		t.Fatal(got)
	}
	owner = "1"
	if got := nativeResource(context.Background(), run, root, frontend{module: "console", port: 5170}); got != nil {
		t.Fatal("foreign listener admitted", got)
	}
}

func TestComposeFrontendRejectsRecreatedOrForeignContainer(t *testing.T) {
	const root = "/owned/workspace"
	cid := strings.Repeat("a", 64)
	workingDir, changed := root, false
	count := 0
	run := func(_ context.Context, name string, args ...string) (string, error) {
		if name != "docker" || args[0] != "--host" || args[1] != "unix:///docker.sock" {
			t.Fatal(name, args)
		}
		switch args[2] {
		case "ps":
			return cid, nil
		case "stats":
			return `{"CPUPerc":"32.00%","MemUsage":"40MiB / 1GiB"}`, nil
		case "inspect":
			count++
			restarts := 2
			if changed && count%2 == 0 {
				restarts++
			}
			row := map[string]any{"ID": cid, "RestartCount": restarts, "State": map[string]any{"Running": true, "StartedAt": "2026-11-19T00:00:00Z"},
				"Config": map[string]any{"Labels": map[string]string{"com.docker.compose.project": "addp-platform", "com.docker.compose.project.working_dir": workingDir, "com.docker.compose.service": "monitor-frontend"}}}
			body, err := json.Marshal(row)
			return string(body), err
		default:
			t.Fatal(args)
			return "", nil
		}
	}
	cfg := configuration{Workspace: root, DockerSocket: "/docker.sock"}
	f := frontend{service: "monitor-frontend"}
	got := composeResource(context.Background(), run, cfg, f)
	if got == nil || got.cores != .32 || got.memoryBytes != 40<<20 || *got.restarts != 2 {
		t.Fatal(got)
	}
	changed = true
	if got := composeResource(context.Background(), run, cfg, f); got != nil {
		t.Fatal("changed identity was accepted", got)
	}
	workingDir = "/another/deployment"
	if got := composeResource(context.Background(), run, cfg, f); got != nil {
		t.Fatal("foreign identity was accepted", got)
	}
}
