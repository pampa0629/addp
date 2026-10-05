package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/google/uuid"
)

func TestObservabilityIdentitiesUseCurrentIntentBindingAndLease(t *testing.T) {
	registry, _, db := newObservationRegistry(t)
	if err := db.AutoMigrate(&models.HostNode{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	empty, err := registry.ObservabilityIdentities(ctx)
	if err != nil || empty.Nodes == nil || empty.ModuleInstances == nil || len(empty.Nodes) != 0 || empty.ObservedAt.IsZero() {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	node := models.HostNode{NodeID: uuid.NewString(), DisplayName: "first", NodeKind: "virtual", Enabled: true, Version: 1, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{{ClientID: "addp-manager", ModuleName: "manager"}, {ClientID: "addp-gateway", ModuleName: "gateway"}}}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"backend", "worker", "scheduler"} {
		request := &models.ModuleRegistrationRequest{ModuleName: "manager", InstanceID: role, Role: role, RoutePrefix: "/manager", ProcessStartedAt: time.Now().UTC(), NodeID: node.NodeID, RegistrationClientID: "addp-manager"}
		if role == "backend" {
			request.ModuleURL = "http://example.test"
		}
		if err := registry.Register(request); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Register(&models.ModuleRegistrationRequest{ModuleName: "gateway", InstanceID: "ingress", Role: "ingress", ModuleURL: "http://gateway.test", ProcessStartedAt: time.Now().UTC(), NodeID: node.NodeID, RegistrationClientID: "addp-gateway"}); err != nil {
		t.Fatal(err)
	}
	registerExcluded := func(id, declared, client string) {
		t.Helper()
		if err := registry.Register(&models.ModuleRegistrationRequest{ModuleName: "manager", InstanceID: id, Role: "worker", RoutePrefix: "/manager", ProcessStartedAt: time.Now().UTC(), NodeID: declared, RegistrationClientID: client}); err != nil {
			t.Fatal(err)
		}
	}
	registerExcluded("unbound", "", "addp-manager")
	registerExcluded("unknown", uuid.NewString(), "addp-manager")
	registerExcluded("foreign", node.NodeID, "addp-meta")
	registerExcluded("expired", node.NodeID, "addp-manager")
	registerExcluded("down", node.NodeID, "addp-manager")
	if err := db.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", "expired").Update("lease_expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := registry.Deregister("manager", "down"); err != nil {
		t.Fatal(err)
	}
	check := func(nodes, instances int) {
		t.Helper()
		snapshot, err := registry.ObservabilityIdentities(ctx)
		if err != nil || len(snapshot.Nodes) != nodes || len(snapshot.ModuleInstances) != instances {
			t.Fatalf("snapshot=%+v err=%v", snapshot, err)
		}
		for _, instance := range snapshot.ModuleInstances {
			if instance.NodeID != node.NodeID || !instance.LeaseExpiresAt.After(snapshot.ObservedAt) || instance.InstanceID != instance.Role {
				t.Fatalf("invalid included instance %+v", instance)
			}
		}
	}
	check(1, 4)
	if err := db.Model(&models.ModuleDefinition{}).Where("module_name = ?", "manager").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	check(1, 1)
	if err := db.Model(&models.ModuleDefinition{}).Where("module_name = ?", "manager").Update("enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&node).Update("allowed_module_bindings", "[]").Error; err != nil {
		t.Fatal(err)
	}
	check(1, 0)
	if err := registry.SendHeartbeat("manager", "worker"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&node).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	check(0, 0)
	if err := registry.SendHeartbeat("manager", "scheduler"); err != nil {
		t.Fatal(err)
	}
	// Discovery never writes the lease/state; stopping observation does not stop business heartbeats.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if snapshot, err := registry.ObservabilityIdentities(cancelled); snapshot != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled=%+v %v", snapshot, err)
	}
}

func TestObservabilityIdentityNodeBudgetNeverReturnsPartialSnapshot(t *testing.T) {
	registry, _, db := newObservationRegistry(t)
	if err := db.AutoMigrate(&models.HostNode{}); err != nil {
		t.Fatal(err)
	}
	nodes := make([]models.HostNode, commonmodels.ObservabilityIdentityNodeLimit+1)
	for i := range nodes {
		nodes[i] = models.HostNode{NodeID: uuid.NewString(), DisplayName: "node", NodeKind: "virtual", Enabled: true, Version: 1, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{}}
	}
	if err := db.CreateInBatches(nodes, 100).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot, err := registry.ObservabilityIdentities(context.Background()); snapshot != nil || !errors.Is(err, repository.ErrObservabilityIdentityBudget) {
		t.Fatalf("oversized=%+v %v", snapshot, err)
	}
	if err := db.Delete(&nodes[len(nodes)-1]).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot, err := registry.ObservabilityIdentities(context.Background()); err != nil || len(snapshot.Nodes) != commonmodels.ObservabilityIdentityNodeLimit {
		t.Fatalf("boundary=%+v %v", snapshot, err)
	}
}

func TestObservabilityIdentityInstanceAndByteBudgetsNeverTruncate(t *testing.T) {
	registry, _, db := newObservationRegistry(t)
	if err := db.AutoMigrate(&models.HostNode{}); err != nil {
		t.Fatal(err)
	}
	node := models.HostNode{NodeID: uuid.NewString(), DisplayName: "node", NodeKind: "virtual", Enabled: true, Version: 1, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{{ClientID: "addp-manager", ModuleName: "manager"}}}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	definition := models.ModuleDefinition{ModuleName: "manager", RoutePrefix: "/manager", Enabled: true, Version: 1}
	if err := db.Create(&definition).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	instances := make([]models.ModuleRuntimeInstance, commonmodels.ObservabilityIdentityInstanceLimit+1)
	for i := range instances {
		instances[i] = models.ModuleRuntimeInstance{ModuleDefinitionID: definition.ID, InstanceID: "worker-" + strconv.Itoa(i), Role: "worker", DeclaredNodeID: node.NodeID, RegistrationClientID: "addp-manager", Status: "up", LastHeartbeat: now, LeaseExpiresAt: now.Add(time.Hour), RegisteredAt: now}
	}
	if err := db.CreateInBatches(instances, 100).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot, err := registry.ObservabilityIdentities(context.Background()); snapshot != nil || !errors.Is(err, repository.ErrObservabilityIdentityBudget) {
		t.Fatalf("candidate overflow=%+v %v", snapshot, err)
	}
	if err := db.Delete(&instances[len(instances)-1]).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot, err := registry.ObservabilityIdentities(context.Background()); err != nil || len(snapshot.ModuleInstances) != commonmodels.ObservabilityIdentityInstanceLimit {
		t.Fatalf("candidate boundary=%+v %v", snapshot, err)
	}
	// Each identity remains within the VARCHAR(100) character contract; UTF-8
	// bytes still exceed the independent response budget at this object count.
	if err := db.Exec("UPDATE module_runtime_instances SET instance_id = CAST(id AS TEXT) || substr(?,1,100-length(CAST(id AS TEXT)))", strings.Repeat("汉", 100)).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot, err := registry.ObservabilityIdentities(context.Background()); snapshot != nil || !errors.Is(err, repository.ErrObservabilityIdentityBudget) {
		t.Fatalf("byte overflow=%+v %v", snapshot, err)
	}
}
