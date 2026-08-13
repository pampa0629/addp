package buildinfo

import (
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
