package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

func expectationTestDocument(at time.Time) commonmodels.ServiceExpectationDocument {
	return commonmodels.ServiceExpectationDocument{Schema: "addp.service-expectations/v1", DeploymentID: strings.Repeat("a", 64), Revision: 1,
		Groups: []commonmodels.ServiceGroupExpectation{
			{ModuleName: "meta", Role: "backend", ExpectedReplicas: 1, DesiredState: "running", ChangedAt: at},
			{ModuleName: "meta", Role: "worker", RuntimeName: "scan", ExpectedReplicas: 2, DesiredState: "running", ChangedAt: at},
			{ModuleName: "transfer", Role: "worker", RuntimeName: "continuous_sync", ExpectedReplicas: 1, DesiredState: "running", ChangedAt: at},
			{ModuleName: "transfer", Role: "worker", RuntimeName: "sync", ExpectedReplicas: 1, DesiredState: "running", ChangedAt: at},
		}}
}

func TestServiceExpectationParserRejectsIncompleteAliasedOrInventedIntent(t *testing.T) {
	at := time.Now().UTC()
	plan := expectationTestDocument(at.Add(-time.Hour))
	raw, _ := json.Marshal(plan)
	if parseServiceExpectations(raw, at) == nil {
		t.Fatal("valid persistent intent was rejected")
	}
	for _, raw := range []string{
		`{}`, `[]`, `null`, strings.Replace(string(raw), `"schema":`, `"Schema":`, 1),
		strings.Replace(string(raw), `"revision":1`, `"revision":true`, 1),
		strings.Replace(string(raw), `"revision":1`, `"revision":1,"revision":2`, 1),
		strings.Replace(string(raw), `"runtime_name":"scan"`, `"runtime_name":"scan","runtime_name":"sync"`, 1),
		strings.Replace(string(raw), `"runtime_name":"",`, ``, 1),
		strings.Replace(string(raw), `"runtime_name":""`, `"runtime_name":null`, 1),
		strings.Replace(string(raw), `"running"`, `"stopped"`, 1),
		strings.Replace(string(raw), `"worker"`, `"scheduler"`, 1),
		string(raw) + `{}`, string(raw[:len(raw)-1]) + `,"secret":"private"}`,
		strings.Repeat("x", deploymentDiscoveryLimit+1),
	} {
		if parseServiceExpectations([]byte(raw), at) != nil {
			t.Fatal("invalid intent was accepted")
		}
	}
	plan.Groups[0].ChangedAt = at.Add(time.Second)
	raw, _ = json.Marshal(plan)
	if parseServiceExpectations(raw, at) != nil {
		t.Fatal("future intent was accepted")
	}
}

func TestServiceGroupsUseFormalLeasesAndPurposeWithoutRequiringMetricsOrHostBinding(t *testing.T) {
	registry, _, db := newObservationRegistry(t)
	if err := db.AutoMigrate(&models.HostNode{}, &models.EngineDeployment{}); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	registry.deploymentDiscovery = &DeploymentDiscoveryService{Directory: directory}
	path := filepath.Join(directory, "service-expectations.json")
	plan := expectationTestDocument(time.Now().Add(-time.Hour).UTC())
	write := func() {
		t.Helper()
		raw, _ := json.Marshal(plan)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	register := func(module, id, role, purpose string) {
		t.Helper()
		req := &models.ModuleRegistrationRequest{ModuleName: module, InstanceID: id, Role: role, RoutePrefix: "/" + module,
			ProcessStartedAt: time.Now().Add(-time.Minute).UTC(), Metadata: map[string]any{"runtime_name": purpose}}
		if role == "backend" {
			req.ModuleURL = "http://example.test"
		}
		if err := registry.Register(req); err != nil {
			t.Fatal(err)
		}
	}
	register("meta", "backend", "backend", "")
	register("meta", "scan-one", "worker", "scan")
	register("meta", "scan-two", "worker", "scan")
	register("transfer", "continuous", "worker", "continuous_sync")
	register("transfer", "bounded", "worker", "sync")
	read := func() *commonmodels.ObservabilityIdentitySnapshot {
		t.Helper()
		snapshot, err := registry.ObservabilityIdentities(context.Background())
		if err != nil || snapshot.ServiceGroups == nil {
			t.Fatalf("snapshot=%+v err=%v", snapshot, err)
		}
		return snapshot
	}
	snapshot := read()
	if len(snapshot.ModuleInstances) != 0 || snapshot.ServiceGroups.State != "available" || len(snapshot.ServiceGroups.Groups) != 4 {
		t.Fatalf("independent group projection: %+v groups=%+v", snapshot, snapshot.ServiceGroups)
	}
	for index, want := range []int{1, 2, 1, 1} {
		group := snapshot.ServiceGroups.Groups[index]
		if group.OnlineReplicas != want || group.RegisteredCount != want || group.State != "observed" {
			t.Fatalf("group %d: %+v", index, group)
		}
	}
	if !snapshot.ServiceGroups.ObservedAt.Equal(snapshot.ObservedAt) {
		t.Fatal("group counts did not use the control-plane snapshot time")
	}
	if err := db.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", "scan-one").Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if group := read().ServiceGroups.Groups[1]; group.OnlineReplicas != 1 || group.RegisteredCount != 2 {
		t.Fatalf("one expired replica: %+v", group)
	}
	if err := db.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", "scan-two").Updates(map[string]any{"status": "down", "stop_reason": "graceful"}).Error; err != nil {
		t.Fatal(err)
	}
	if group := read().ServiceGroups.Groups[1]; group.OnlineReplicas != 0 || group.State != "observed" {
		t.Fatalf("zero is a lease fact, not an inferred alert: %+v", group)
	}
	register("meta", "unknown-purpose", "worker", "")
	if group := read().ServiceGroups.Groups[1]; group.State != "unknown" || group.Reason != "runtime_name_unavailable" {
		t.Fatalf("unknown purpose was guessed: %+v", group)
	}
	plan.Groups[1].DesiredState = "planned_stopped"
	plan.Revision++
	write()
	if group := read().ServiceGroups.Groups[1]; group.State != "paused" || group.Reason != "planned_stopped" {
		t.Fatalf("planned stop: %+v", group)
	}
	if err := os.WriteFile(path, []byte(`{"private":"do-not-copy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if groups := read().ServiceGroups; groups.State != "unavailable" || len(groups.Groups) != 0 || groups.Reason != "invalid_intent" {
		t.Fatalf("invalid file became zero/healthy: %+v", groups)
	}
	write()
	changed := false
	const callback = "test:service-intent-change"
	if err := db.Callback().Row().After("gorm:row").Register(callback, func(tx *gorm.DB) {
		if !changed && tx.Statement.Table == "host_nodes" {
			changed = true
			plan.Revision++
			write()
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Row().Remove(callback) })
	if groups := read().ServiceGroups; !changed || groups.State != "unavailable" || groups.Reason != "source_changed" || len(groups.Groups) != 0 {
		t.Fatalf("changed intent survived recheck: %+v", groups)
	}
}
