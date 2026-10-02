package buildinfo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerRegistrationUsesProcessIdentity(t *testing.T) {
	paths, err := filepath.Glob("../../*/backend/cmd/worker/main.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("worker entrypoints: %v", err)
	}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typeName, ok := literal.Type.(*ast.SelectorExpr)
			if !ok || typeName.Sel.Name != "ModuleRegistrationRequest" {
				return true
			}
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := field.Key.(*ast.Ident)
				if !ok || key.Name != "InstanceID" {
					continue
				}
				call, ok := field.Value.(*ast.CallExpr)
				if !ok {
					t.Errorf("%s: registry identity must consume the shared process identity, not an execution lease owner", path)
					continue
				}
				function, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || function.Sel.Name != "ProcessInstanceID" || len(call.Args) != 0 {
					t.Errorf("%s: unexpected registry identity source", path)
				}
			}
			return true
		})
	}
}

func TestHealthIncludesStableBuildAndProcessIdentity(t *testing.T) {
	originalBuildID, originalCommit := BuildID, GitCommit
	originalFingerprint, originalBuiltAt := SourceFingerprint, BuiltAt
	t.Cleanup(func() {
		BuildID, GitCommit = originalBuildID, originalCommit
		SourceFingerprint, BuiltAt = originalFingerprint, originalBuiltAt
	})

	BuildID = "build-1"
	GitCommit = "commit-1"
	SourceFingerprint = "sha256:fingerprint-1"
	BuiltAt = "2026-08-13T07:25:09Z"

	response := Health("model")
	if response.Status != "ok" || response.Module != "model" ||
		response.BuildID != BuildID || response.GitCommit != GitCommit ||
		response.SourceFingerprint != SourceFingerprint || response.BuiltAt != BuiltAt {
		t.Fatalf("unexpected health response: %#v", response)
	}
	if _, err := time.Parse(time.RFC3339Nano, response.StartedAt); err != nil {
		t.Fatalf("started_at = %q: %v", response.StartedAt, err)
	}
	if second := Health("model"); second.StartedAt != response.StartedAt {
		t.Fatalf("started_at changed: first=%q second=%q", response.StartedAt, second.StartedAt)
	}
}

func TestProcessIdentityUsesLaunchEnvironment(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"process-a", "process-b"} {
		cmd := exec.Command(self, "-test.run=^TestProcessIdentityHelper$")
		cmd.Env = append(os.Environ(), "ADDP_IDENTITY_HELPER=1", "ADDP_PROCESS_INSTANCE_ID="+id)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("identity helper: %v %s", err, output)
		}
	}
}
func TestProcessIdentityHelper(t *testing.T) {
	if os.Getenv("ADDP_IDENTITY_HELPER") != "1" {
		return
	}
	expected := os.Getenv("ADDP_PROCESS_INSTANCE_ID")
	first := Health("manager")
	if first.InstanceID != expected || ProcessInstanceID() != expected {
		t.Fatal("launch identity not consumed")
	}
	os.Setenv("ADDP_PROCESS_INSTANCE_ID", "later-environment")
	if Health("manager").InstanceID != expected {
		t.Fatal("same process changed registration identity")
	}
}
