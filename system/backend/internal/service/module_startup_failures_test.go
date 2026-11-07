package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
)

func TestModuleStartupResultsRequireCurrentDeploymentAndConfirmedReason(t *testing.T) {
	at := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	plan := expectationTestDocument(at.Add(-time.Hour))
	raw := []byte(`{"schema":"addp.service-startup-results/v1","deployment_id":"` + plan.DeploymentID + `","modules":{"meta":{"attempt":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","started_at":"2026-11-06T23:30:00Z","reason":"build_failed","failed_at":"2026-11-06T23:31:00Z"}}}`)
	if parseBackendStartupResults(raw, plan.DeploymentID, at) == nil {
		t.Fatal("valid failure rejected")
	}
	for _, test := range [][2]string{
		{"build_failed", "raw diagnostic"},
		{"2026-11-06T23:31:00Z", "2026-11-07T00:01:00Z"},
		{"2026-11-06T23:31:00Z", "2026-11-06T23:29:00Z"},
		{"\"attempt\":", "\"command\":\"secret\",\"attempt\":"},
		{"\"reason\":", "\"reason\":\"process_exited\",\"reason\":"},
	} {
		bad := strings.Replace(string(raw), test[0], test[1], 1)
		if parseBackendStartupResults([]byte(bad), plan.DeploymentID, at) != nil {
			t.Fatalf("invalid failure accepted: %v", test)
		}
	}
	if parseBackendStartupResults(raw, strings.Repeat("b", 64), at) != nil {
		t.Fatal("foreign deployment accepted")
	}
	// The same failure must disappear after a planned stop, even without a DB read.
	plan.Groups[0].DesiredState = "planned_stopped"
	dir := t.TempDir()
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"service-expectations.json": planJSON, startupResultsFile: raw} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := ModuleRegistryService{deploymentDiscovery: &DeploymentDiscoveryService{Directory: dir}}
	if rows, err := s.ListModuleStartupFailures(context.Background()); err != nil || len(rows) != 0 {
		t.Fatalf("planned stop exposed a failure: %+v %v", rows, err)
	}
}

func TestModuleStartupFailureIsSupersededOnlyByBackendFacts(t *testing.T) {
	now := time.Now().UTC()
	failedAt := now.Add(-time.Minute)
	worker := models.ModuleRuntimeInstance{Role: "worker", Status: "up", RegisteredAt: now, LeaseExpiresAt: now.Add(time.Minute)}
	expired := models.ModuleRuntimeInstance{Role: "backend", Status: "up", RegisteredAt: failedAt.Add(-time.Second), LeaseExpiresAt: now}
	if backendStartupFailureSuperseded([]models.ModuleRuntimeInstance{worker, expired}, failedAt, now) {
		t.Fatal("Worker or expired old lease hid the failure")
	}
	expired.LeaseExpiresAt = now.Add(time.Second)
	if !backendStartupFailureSuperseded([]models.ModuleRuntimeInstance{expired}, failedAt, now) {
		t.Fatal("currently online Backend should hide the failure")
	}
	expired.Status, expired.RegisteredAt = "down", failedAt.Add(time.Second)
	if !backendStartupFailureSuperseded([]models.ModuleRuntimeInstance{expired}, failedAt, now) {
		t.Fatal("later registration should supersede the old failure")
	}
}
