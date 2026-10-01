package buildinfo

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

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
