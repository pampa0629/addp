package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
)

func TestProcessMetricsRegistrationIsImmutableAndPrivate(t *testing.T) {
	registry, _, db := newObservationRegistry(t)
	if err := db.AutoMigrate(&models.HostNode{}); err != nil {
		t.Fatal(err)
	}
	request := models.ModuleRegistrationRequest{ModuleName: "monitor", InstanceID: "native-process", Role: "worker", RoutePrefix: "/monitor", ProcessStartedAt: time.Now().UTC().Truncate(time.Microsecond),
		ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:18100/metrics"}}
	if err := registry.Register(&request); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(&request); err != nil {
		t.Fatalf("same declaration: %v", err)
	}
	module, err := registry.GetModule("monitor")
	if err != nil {
		t.Fatal(err)
	}
	if len(module.Instances) != 1 || !module.Instances[0].ProcessMetricsDeclared {
		t.Fatal("safe presence projection missing")
	}
	encoded, err := json.Marshal(module)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "18100") || strings.Contains(string(encoded), "endpoint") || strings.Contains(string(encoded), "schema_version") {
		t.Fatal("browser DTO exposed private declaration")
	}
	for _, mutate := range []func(*models.ModuleRegistrationRequest){
		func(r *models.ModuleRegistrationRequest) { r.ProcessMetrics = nil },
		func(r *models.ModuleRegistrationRequest) {
			r.ProcessMetrics = &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:18101/metrics"}
		},
		func(r *models.ModuleRegistrationRequest) { r.ProcessStartedAt = r.ProcessStartedAt.Add(time.Second) },
		func(r *models.ModuleRegistrationRequest) { r.Role = "scheduler" },
	} {
		copy := request
		mutate(&copy)
		if err := registry.Register(&copy); !errors.Is(err, ErrInvalidModuleRegistration) {
			t.Fatalf("mutable identity accepted: %v", err)
		}
	}
	// Failed observation configuration is represented by absence, not a invented
	// endpoint. It may not be attached later to the same process identity.
	notSelected := request
	notSelected.InstanceID = "unselected"
	notSelected.ProcessMetrics = nil
	if err := registry.Register(&notSelected); err != nil {
		t.Fatal(err)
	}
	notSelected.ProcessMetrics = request.ProcessMetrics
	if err := registry.Register(&notSelected); !errors.Is(err, ErrInvalidModuleRegistration) {
		t.Fatal("late declaration attached")
	}
}

func TestProcessMetricsProjectionDoesNotRequireHostBinding(t *testing.T) {
	registry, _, db := newObservationRegistry(t)
	if err := db.AutoMigrate(&models.HostNode{}); err != nil {
		t.Fatal(err)
	}
	node := models.HostNode{NodeID: uuid.NewString(), DisplayName: "VM", NodeKind: "virtual", Enabled: true, Version: 1, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{{ClientID: "addp-monitor", ModuleName: "monitor"}}}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	base := models.ModuleRegistrationRequest{ModuleName: "monitor", Role: "worker", RoutePrefix: "/monitor", RegistrationClientID: "addp-monitor", ProcessStartedAt: time.Now().UTC(),
		ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:18100/metrics"}}
	for _, subject := range []struct{ id, node string }{{"unbound", ""}, {"bound", node.NodeID}, {"unknown-host", uuid.NewString()}} {
		r := base
		r.InstanceID = subject.id
		r.NodeID = subject.node
		if err := registry.Register(&r); err != nil {
			t.Fatal(err)
		}
	}
	assert := func(bound bool) {
		t.Helper()
		snapshot, err := registry.ObservabilityIdentities(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.ModuleInstances) != 3 {
			t.Fatalf("independent process coverage: %+v", snapshot)
		}
		for _, i := range snapshot.ModuleInstances {
			if i.ProcessMetrics == nil || i.ProcessStartedAt == nil {
				t.Fatal("missing current private declaration")
			}
			if i.InstanceID == "bound" && bound {
				if i.NodeID != node.NodeID {
					t.Fatal("binding missing")
				}
			} else if i.NodeID != "" {
				t.Fatal("invalid host relationship leaked")
			}
		}
	}
	assert(true)
	if err := db.Model(&node).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	assert(false)
	if err := registry.Deregister("monitor", "bound"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.ObservabilityIdentities(context.Background())
	if err != nil || len(snapshot.ModuleInstances) != 2 {
		t.Fatalf("offline process retained: %+v %v", snapshot, err)
	}
}
